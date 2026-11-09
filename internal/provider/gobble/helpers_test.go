package gobble

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maccavelli/gobble-cli/acpext"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent"
	"github.com/maccavelli/magic-cli-remote/internal/testexec"
)

// stub is a fake `gobble` binary for the auth CLI: it records each run's argv
// and stdin under dir, and answers `auth status --json` from dir/status.json
// with dir/status.code as its exit. Tests never run the host's gobble or touch
// its store.
type stub struct {
	bin, dir string
}

// stubStatus builds a status report in which ready's providers are usable,
// each from source (auth.json with authType api_key, or env).
func stubStatus(source string, ready ...string) string {
	var rows []string
	for _, p := range acpext.AuthProviders() {
		row := `{"provider":` + strconv.Quote(p.ID) + `,"source":"none","usable":false}`
		for _, r := range ready {
			if r == p.ID {
				switch source {
				case sourceAuthJSON:
					row = `{"provider":` + strconv.Quote(p.ID) + `,"source":"auth.json","authType":"api_key","usable":true}`
				default:
					row = `{"provider":` + strconv.Quote(p.ID) + `,"source":"env","variable":"X_API_KEY","usable":true}`
				}
			}
		}
		rows = append(rows, row)
	}
	return `{"providers":[` + strings.Join(rows, ",") + `]}`
}

func newStub(t *testing.T, status string) *stub {
	t.Helper()
	testexec.SkipIfNoPOSIXShell(t)
	dir := t.TempDir()
	s := &stub{dir: dir}
	s.setStatus(t, status, 0)
	q := strconv.Quote
	body := `d=` + q(dir) + `
printf '%s\n' "$*" >> "$d/argv"
case "$*" in
  "auth status --json")
    cat "$d/status.json"; exit "$(cat "$d/status.code")" ;;
  "auth login "*" --api-key - --json"*)
    cat > "$d/stdin"
    if [ -f "$d/login.error" ]; then cat "$d/login.error"; exit 1; fi
    printf '{"event":"saved","provider":"x","type":"api_key"}\n'; exit 0 ;;
  "auth logout "*)
    exit 0 ;;
  "auth login "*" --method device --json")
    printf '{"event":"device_code","provider":"x","url":"https://example.invalid/device","code":"ABCD-1234"}\n'
    printf '{"event":"saved","provider":"x","type":"oauth"}\n'; exit 0 ;;
esac
exit 64
`
	s.bin = testexec.WriteShellStub(t, filepath.Join(dir, "gobble"), body)
	return s
}

func (s *stub) setStatus(t *testing.T, status string, code int) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(s.dir, "status.json"), []byte(status), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.dir, "status.code"), []byte(strconv.Itoa(code)), 0o600); err != nil {
		t.Fatal(err)
	}
}

// runs are the argv lines the stub recorded.
func (s *stub) runs(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(s.dir, "argv"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

func (s *stub) stdin(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(s.dir, "stdin"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// newProvider is the gobble provider on the stub.
func newProvider(s *stub) *acpagent.Provider {
	return New(acpagent.Config{Bin: s.bin}, nil)
}

// authAgent is an agent that answers initialize, advertising gobble's nine
// methods, and authenticate with authErr's JSON-RPC error when set; it records
// each method it is sent. It is for starts that fail, which acptest.Start
// cannot take.
type authAgent struct {
	authErr string
	// newReply is session/new's result; {"sessionId":"agent-1"} when empty.
	newReply string

	mu      sync.Mutex
	methods []string
}

func (a *authAgent) got() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.methods...)
}

func (a *authAgent) serve(r io.Reader, w io.Writer) {
	var ids []string
	for _, p := range acpext.AuthProviders() {
		ids = append(ids, `{"id":`+strconv.Quote(p.ID)+`,"name":`+strconv.Quote(p.Name)+`}`)
	}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for sc.Scan() {
		var m struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if json.Unmarshal(sc.Bytes(), &m) != nil || m.Method == "" {
			continue
		}
		a.mu.Lock()
		a.methods = append(a.methods, m.Method)
		a.mu.Unlock()
		var reply string
		switch m.Method {
		case "initialize":
			reply = `"result":{"protocolVersion":1,"agentCapabilities":{},"authMethods":[` + strings.Join(ids, ",") + `]}`
		case "authenticate":
			reply = `"result":{}`
			if a.authErr != "" {
				reply = `"error":` + a.authErr
			}
		case "session/new":
			reply = `"result":{"sessionId":"agent-1"}`
			if a.newReply != "" {
				var compact bytes.Buffer
				_ = json.Compact(&compact, []byte(a.newReply))
				reply = `"result":` + compact.String()
			}
		default:
			if m.ID == nil {
				continue
			}
			reply = `"error":{"code":-32601,"message":"Method not found"}`
		}
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":`+string(m.ID)+`,`+reply+"}\n")
	}
}

// startOnAgent starts p against a, returning StartOn's error.
func startOnAgent(t *testing.T, p *acpagent.Provider, a *authAgent) (provider.Session, error) {
	t.Helper()
	clientReads, agentWrites := io.Pipe()
	agentReads, clientWrites := io.Pipe()
	go a.serve(agentReads, agentWrites)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s, err := p.StartOn(ctx, provider.StartOptions{CWD: t.TempDir()}, clientReads, clientWrites)
	t.Cleanup(func() {
		if s != nil {
			_ = s.Close(context.Background())
		}
		_ = agentWrites.Close()
		_ = clientWrites.Close()
		_ = agentReads.Close()
	})
	return s, err
}
