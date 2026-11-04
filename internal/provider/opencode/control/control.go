// Package control runs OpenCode v2's control plane: one
// `opencode serve --stdio --port 0` child that serves the HTTP surfaces ACP
// lacks (0214 MADR D2). Sessions run on `opencode acp`; this child answers
// auth, catalog grouping, workspace, diagnostics, revert and shell (0214 PLAN
// P6).
//
// The child is started on first use and stopped after an idle period, so a
// host that never opens those surfaces runs one engine, not two. Its stdin is
// the lease: closing it stops the engine, and nothing outlives Magic holding
// it. Every request carries a password generated for that launch, which goes
// only into the child's OPENCODE_PASSWORD; it is never in argv, a log line, a
// capture or a file (0214 PLAN C5).
package control

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/maccavelli/magic-cli-remote/internal/procutil"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/launch"
	"github.com/maccavelli/magic-cli-remote/internal/wirecap"
)

// Defaults for the zero values in Config.
const (
	// defaultStartTimeout bounds the launch: the ready line and /api/info. A
	// cold opencode took 43.7 s to answer ACP's initialize (0214 MADR
	// amendment A, A6); serve shares that boot.
	defaultStartTimeout = 120 * time.Second
	// defaultIdleTimeout is how long the child runs with no request.
	defaultIdleTimeout = 10 * time.Minute
	// defaultLeaseWait is how long a closed lease has to end the child before
	// it is signalled. G3 measured 0.04–0.22 s.
	defaultLeaseWait = 5 * time.Second
	// defaultTerminateWait is the SIGTERM half of the stop, before the kill.
	defaultTerminateWait = 5 * time.Second
	// defaultRequestTimeout bounds one request.
	defaultRequestTimeout = 2 * time.Minute
)

// agentRetries bounds A3's retry: GET /api/agent answers 503 while plugins
// load, so it is tried at most this many times, waiting agentRetryFirst and
// doubling between tries (about 8 s in all).
const agentRetries = 6

// agentRetryFirst is the first wait; a variable so tests can shorten it.
var agentRetryFirst = 250 * time.Millisecond

// passwordEnv is the child's password variable (D2).
const passwordEnv = "OPENCODE_PASSWORD"

// basicUser is the user HTTP Basic pairs with the password.
const basicUser = "opencode"

// Config configures the control plane.
type Config struct {
	// Bin is the opencode binary; "opencode" when empty.
	Bin string
	// MinVersion is the oldest engine /api/info may name; the child is
	// refused below it, or when it names none (0214 MADR D3).
	MinVersion string
	// UpgradeURL is where that refusal sends the operator.
	UpgradeURL string
	// Env is added to the child's environment, after Magic's own.
	Env []string
	// StartTimeout, IdleTimeout, LeaseWait and TerminateWait override the
	// defaults above when positive.
	StartTimeout  time.Duration
	IdleTimeout   time.Duration
	LeaseWait     time.Duration
	TerminateWait time.Duration
	// Log receives the plane's lines; slog.Default when nil.
	Log *slog.Logger
}

// Plane is the lease-bound control plane. Its methods are safe for
// concurrent use.
type Plane struct {
	cfg  Config
	log  *slog.Logger
	http *http.Client
	wire *wirecap.Capture

	mu       sync.Mutex
	eng      *engine
	starting chan struct{} // closed when an in-flight start finishes
	closed   bool
	inFlight int
	lastUse  time.Time
	idle     *time.Timer
}

// engine is one running child.
type engine struct {
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	url      string
	password string
	version  string
	dead     chan struct{} // closed when the child has exited
	exitErr  error         // the child's Wait result, set before dead closes
	release  func()
	lease    procutil.EngineLease
}

// New returns a control plane. It starts nothing until a request needs it.
func New(cfg Config) *Plane {
	if cfg.Bin == "" {
		cfg.Bin = "opencode"
	}
	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}
	return &Plane{
		cfg:  cfg,
		log:  log.With(slog.String("component", "opencode-control")),
		http: &http.Client{Timeout: defaultRequestTimeout},
		wire: wirecap.For("opencode-control"),
	}
}

func orDefault(d, def time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return def
}

// Request is one call to the control plane.
type Request struct {
	Method string
	// Path is the route, from "/api/…".
	Path  string
	Query url.Values
	// Directory scopes the call to a project. When set, it travels as the
	// location[directory] query parameter and the x-opencode-directory header
	// (0214 MADR D2, amended).
	Directory string
	// Body is sent as JSON when non-nil. A create body carries the directory
	// as location.directory: see WithLocation.
	Body any
	// Raw takes the answer as it comes, in any content type, for a route that
	// answers a file's bytes (fs.read). At most MaxBytes are read, and a longer
	// answer sets Response.Truncated. The body never enters the wire capture,
	// which records only its size (0214 MADR, added at P6d).
	Raw      bool
	MaxBytes int
}

// WithLocation returns body with location.directory set to dir, as the
// session create routes take it (D2).
func WithLocation(dir string, body map[string]any) map[string]any {
	out := make(map[string]any, len(body)+1)
	for k, v := range body {
		out[k] = v
	}
	out["location"] = map[string]any{"directory": dir}
	return out
}

// Response is the control plane's answer.
type Response struct {
	Status int
	// Body is the answer as the engine sent it.
	Body []byte
	// ContentType is the answer's, as sent.
	ContentType string
	// Truncated reports a raw answer longer than its MaxBytes; Body holds the
	// first MaxBytes.
	Truncated bool
}

// ErrCredentialRead refuses GET /api/credential: the control plane never
// reads stored credentials (D2).
var ErrCredentialRead = errors.New("the control plane never reads /api/credential")

// ErrProviderRoute refuses /api/provider and /api/model, and anything under
// them: their answers carry a key set as a provider option in opencode.json,
// in plain text (0214 MADR, added at P6c).
var ErrProviderRoute = errors.New("the control plane never calls /api/provider or /api/model, which return configured keys")

// underRoute reports whether path is route or under it.
func underRoute(path, route string) bool {
	return path == route || strings.HasPrefix(path, route+"/")
}

// Do sends req, starting the child if none runs. A JSON answer must say so in
// its content type (0214 MADR D12). GET /api/agent is retried while it answers
// 503 (A3).
func (p *Plane) Do(ctx context.Context, req Request) (Response, error) {
	if strings.EqualFold(req.Method, http.MethodGet) && strings.HasPrefix(req.Path, "/api/credential") {
		return Response{}, ErrCredentialRead
	}
	if underRoute(req.Path, "/api/provider") || underRoute(req.Path, "/api/model") {
		return Response{}, ErrProviderRoute
	}
	if req.Raw && req.MaxBytes <= 0 {
		return Response{}, fmt.Errorf("control plane %s %s: a raw request needs a positive MaxBytes", req.Method, req.Path)
	}
	eng, err := p.acquire(ctx)
	if err != nil {
		return Response{}, err
	}
	defer p.releaseUse()

	wait := agentRetryFirst
	for attempt := 1; ; attempt++ {
		res, err := p.send(ctx, eng, req)
		if err != nil || res.Status != http.StatusServiceUnavailable ||
			!(req.Method == http.MethodGet && req.Path == "/api/agent") || attempt == agentRetries {
			return res, err
		}
		p.log.Debug("control plane: agents not ready; retrying",
			slog.Int("attempt", attempt), slog.Duration("wait", wait))
		select {
		case <-ctx.Done():
			return res, ctx.Err()
		case <-time.After(wait):
		}
		wait *= 2
	}
}

// send makes one HTTP request to eng.
func (p *Plane) send(ctx context.Context, eng *engine, req Request) (Response, error) {
	q := url.Values{}
	for k, v := range req.Query {
		q[k] = append([]string(nil), v...)
	}
	if req.Directory != "" {
		q.Set("location[directory]", req.Directory)
	}
	target := eng.url + req.Path
	if len(q) > 0 {
		target += "?" + q.Encode()
	}
	var body io.Reader
	if req.Body != nil {
		b, err := json.Marshal(req.Body)
		if err != nil {
			return Response{}, fmt.Errorf("control plane %s %s: %w", req.Method, req.Path, err)
		}
		body = bytes.NewReader(b)
	}
	hr, err := http.NewRequestWithContext(ctx, req.Method, target, body)
	if err != nil {
		return Response{}, fmt.Errorf("control plane %s %s: %w", req.Method, req.Path, err)
	}
	hr.Header.Set("Authorization", basicAuth(eng.password))
	if req.Raw {
		hr.Header.Set("Accept", "*/*")
	} else {
		hr.Header.Set("Accept", "application/json")
	}
	if req.Directory != "" {
		hr.Header.Set("x-opencode-directory", req.Directory)
	}
	if req.Body != nil {
		hr.Header.Set("Content-Type", "application/json")
	}
	res, err := p.http.Do(hr)
	if err != nil {
		return Response{}, fmt.Errorf("control plane %s %s: %w", req.Method, req.Path, err)
	}
	defer res.Body.Close()
	contentType := res.Header.Get("Content-Type")
	if req.Raw {
		return p.readRaw(req, res, contentType)
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	if err != nil {
		return Response{}, fmt.Errorf("control plane %s %s: %w", req.Method, req.Path, err)
	}
	p.wire.Frame(raw)
	p.log.Debug("control plane request", slog.String("method", req.Method),
		slog.String("path", req.Path), slog.Int("status", res.StatusCode))
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := requireJSON(contentType, raw); err != nil {
			return Response{Status: res.StatusCode, Body: raw, ContentType: contentType},
				fmt.Errorf("control plane %s %s: %w", req.Method, req.Path, err)
		}
	}
	return Response{Status: res.StatusCode, Body: raw, ContentType: contentType}, nil
}

// readRaw reads a raw answer: at most MaxBytes, with one byte more read to
// tell a longer answer from one that ends exactly there. The capture gets a
// line naming the route and the size, never the bytes: they are a file of the
// user's, which may hold anything.
func (p *Plane) readRaw(req Request, res *http.Response, contentType string) (Response, error) {
	raw, err := io.ReadAll(io.LimitReader(res.Body, int64(req.MaxBytes)+1))
	if err != nil {
		return Response{}, fmt.Errorf("control plane %s %s: %w", req.Method, req.Path, err)
	}
	out := Response{Status: res.StatusCode, Body: raw, ContentType: contentType}
	if len(raw) > req.MaxBytes {
		out.Body, out.Truncated = raw[:req.MaxBytes], true
	}
	note, _ := json.Marshal(map[string]any{"raw": map[string]any{
		"method": req.Method, "path": req.Path, "status": res.StatusCode, "bytes": len(out.Body), "truncated": out.Truncated,
	}})
	p.wire.Frame(note)
	p.log.Debug("control plane raw request", slog.String("method", req.Method),
		slog.String("path", req.Path), slog.Int("status", res.StatusCode), slog.Int("bytes", len(out.Body)))
	return out, nil
}

func basicAuth(password string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(basicUser+":"+password))
}

// requireJSON refuses an answer that is not JSON, naming its content type and
// first bytes (0214 MADR D12).
func requireJSON(contentType string, body []byte) error {
	mt, _, _ := mime.ParseMediaType(contentType)
	if mt == "application/json" {
		return nil
	}
	head := strings.TrimSpace(string(body))
	if len(head) > 80 {
		head = head[:80]
	}
	return fmt.Errorf("answered %q, not application/json: %q", contentType, head)
}

// acquire returns a running engine, starting one if needed, and counts the
// caller in flight so the idle stop waits for it.
func (p *Plane) acquire(ctx context.Context) (*engine, error) {
	for {
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			return nil, errors.New("control plane closed")
		}
		if eng := p.eng; eng != nil {
			select {
			case <-eng.dead:
				p.eng = nil // it died; start another
			default:
				p.inFlight++
				p.lastUse = time.Now()
				p.mu.Unlock()
				return eng, nil
			}
		}
		if p.starting != nil {
			wait := p.starting
			p.mu.Unlock()
			select {
			case <-wait:
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		done := make(chan struct{})
		p.starting = done
		p.mu.Unlock()

		eng, err := p.start(ctx)

		p.mu.Lock()
		p.starting = nil
		close(done)
		if err != nil {
			p.mu.Unlock()
			return nil, err
		}
		if p.closed {
			p.mu.Unlock()
			p.stop(eng)
			return nil, errors.New("control plane closed")
		}
		p.eng = eng
		p.mu.Unlock()
	}
}

// releaseUse counts a request out, and arms the idle stop when none is left.
func (p *Plane) releaseUse() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.inFlight--
	p.lastUse = time.Now()
	if p.inFlight > 0 || p.eng == nil {
		return
	}
	idle := orDefault(p.cfg.IdleTimeout, defaultIdleTimeout)
	if p.idle != nil {
		p.idle.Stop()
	}
	p.idle = time.AfterFunc(idle, p.stopIfIdle)
}

// stopIfIdle stops the engine when nothing used it for the idle period.
func (p *Plane) stopIfIdle() {
	idle := orDefault(p.cfg.IdleTimeout, defaultIdleTimeout)
	p.mu.Lock()
	eng := p.eng
	if eng == nil || p.inFlight > 0 || time.Since(p.lastUse) < idle {
		p.mu.Unlock()
		return
	}
	p.eng = nil
	p.mu.Unlock()
	p.log.Info("control plane idle; stopping", slog.Duration("idle", idle))
	p.stop(eng)
}

// Close stops the engine, if one runs, and refuses further requests.
func (p *Plane) Close() {
	p.mu.Lock()
	p.closed = true
	eng := p.eng
	p.eng = nil
	if p.idle != nil {
		p.idle.Stop()
	}
	starting := p.starting
	p.mu.Unlock()
	if eng == nil && starting != nil {
		// A start in flight sees closed when it finishes and stops its child.
		<-starting
	}
	if eng != nil {
		p.stop(eng)
	}
	p.wire.Close()
}

// Version is the engine version /api/info named at the last start, or "".
func (p *Plane) Version() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.eng == nil {
		return ""
	}
	return p.eng.version
}

// newPassword is a per-launch secret: 24 random bytes, hex.
func newPassword() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// start launches the child and waits for it to be ready: one JSON ready line
// naming its url, then /api/info answering JSON with a version at or above the
// floor.
func (p *Plane) start(ctx context.Context) (*engine, error) {
	password, err := newPassword()
	if err != nil {
		return nil, fmt.Errorf("control plane password: %w", err)
	}
	resolved, err := launch.Resolve(p.cfg.Bin)
	if err != nil {
		return nil, err
	}
	cmd, err := launch.Command(context.Background(), resolved, "serve", "--stdio", "--port", "0")
	if err != nil {
		return nil, err
	}
	procutil.SetDeathSignal(cmd)
	engineID := uuid.NewString()
	env := append(os.Environ(), p.cfg.Env...)
	cmd.Env = append(env,
		procutil.EnvEngineID+"="+engineID,
		procutil.EnvEngineOwner+"="+procutil.OwnerToken(),
		passwordEnv+"="+password,
	)
	if home, _ := os.UserHomeDir(); home != "" {
		cmd.Dir = home
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = &stderrLog{log: p.log}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s serve: %w", p.cfg.Bin, err)
	}
	release, err := procutil.SuperviseStarted(cmd.Process)
	if err != nil {
		_ = procutil.KillProcessGroup(cmd.Process)
		return nil, fmt.Errorf("supervise %s serve: %w", p.cfg.Bin, err)
	}
	eng := &engine{cmd: cmd, stdin: stdin, password: password, dead: make(chan struct{}), release: release}
	lease, regErr := procutil.RegisterEngine("", procutil.EngineRecord{
		ID:       engineID,
		Provider: "opencode-control",
		PID:      cmd.Process.Pid,
		PGID:     cmd.Process.Pid,
		Owner:    procutil.OwnerToken(),
	})
	if regErr != nil {
		release()
		_ = procutil.KillProcessGroup(cmd.Process)
		_ = cmd.Wait()
		return nil, fmt.Errorf("register control plane: %w", regErr)
	}
	eng.lease = lease
	// The single reaper. dead closes once Wait has returned.
	go func() {
		defer func() {
			if r := recover(); r != nil {
				p.log.Error("control plane wait panic", slog.Any("recover", r), slog.String("stack", string(debug.Stack())))
			}
		}()
		eng.exitErr = cmd.Wait()
		_ = procutil.RemoveEngine(lease)
		close(eng.dead)
		p.noteExit(eng)
	}()

	bound := orDefault(p.cfg.StartTimeout, defaultStartTimeout)
	readyCtx, cancel := context.WithTimeout(ctx, bound)
	defer cancel()
	if err := p.awaitReady(readyCtx, eng, stdout); err != nil {
		p.stop(eng)
		return nil, err
	}
	p.log.Info("control plane ready", slog.String("version", eng.version))
	return eng, nil
}

// readyLine is serve --stdio's one line on stdout.
type readyLine struct {
	URL string `json:"url"`
}

// awaitReady reads the ready line, keeps draining stdout after it, as
// opencode's standalone.ts does, and checks /api/info.
func (p *Plane) awaitReady(ctx context.Context, eng *engine, stdout io.Reader) error {
	lines := make(chan []byte, 1)
	r := bufio.NewReader(stdout)
	go func() {
		line, err := r.ReadBytes('\n')
		if err != nil && len(line) == 0 {
			close(lines)
			return
		}
		lines <- line
		_, _ = io.Copy(io.Discard, r)
	}()
	var line []byte
	select {
	case l, ok := <-lines:
		if !ok {
			return fmt.Errorf("%s serve ended before its ready line", p.cfg.Bin)
		}
		line = l
	case <-eng.dead:
		return fmt.Errorf("%s serve exited before its ready line", p.cfg.Bin)
	case <-ctx.Done():
		return fmt.Errorf("%s serve wrote no ready line: %w", p.cfg.Bin, ctx.Err())
	}
	var ready readyLine
	if err := json.Unmarshal(bytes.TrimSpace(line), &ready); err != nil || !strings.HasPrefix(ready.URL, "http://") {
		head := strings.TrimSpace(string(line))
		if len(head) > 80 {
			head = head[:80]
		}
		return fmt.Errorf("%s serve's ready line is not {\"url\"}: %q", p.cfg.Bin, head)
	}
	eng.url = strings.TrimRight(ready.URL, "/")

	res, err := p.send(ctx, eng, Request{Method: http.MethodGet, Path: "/api/info"})
	if err != nil {
		return err
	}
	if res.Status != http.StatusOK {
		return fmt.Errorf("control plane /api/info answered %d", res.Status)
	}
	version := infoVersion(res.Body)
	if p.cfg.MinVersion != "" {
		if version == "" {
			return fmt.Errorf("opencode control plane named no version in /api/info; it needs %s or later (%s)",
				p.cfg.MinVersion, p.cfg.UpgradeURL)
		}
		if provider.CompareVersions(version, p.cfg.MinVersion) < 0 {
			return fmt.Errorf("opencode control plane version %s is below the minimum %s; upgrade: %s",
				version, p.cfg.MinVersion, p.cfg.UpgradeURL)
		}
	}
	eng.version = version
	return nil
}

// infoVersion reads /api/info's version, from its data envelope or bare.
func infoVersion(body []byte) string {
	var v struct {
		Version string `json:"version"`
		Data    struct {
			Version string `json:"version"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &v) != nil {
		return ""
	}
	if v.Data.Version != "" {
		return v.Data.Version
	}
	return v.Version
}

// stop ends eng: the lease first, then SIGTERM, then the kill (D2).
func (p *Plane) stop(eng *engine) {
	_ = eng.stdin.Close()
	select {
	case <-eng.dead:
	case <-time.After(orDefault(p.cfg.LeaseWait, defaultLeaseWait)):
		procutil.TerminateProcessGroup(eng.cmd.Process, eng.dead, orDefault(p.cfg.TerminateWait, defaultTerminateWait))
		<-eng.dead
	}
	eng.release()
}

// noteExit logs how the child ended. Exit 0 after the lease closed, and 130
// after SIGTERM on POSIX, are orderly; a Windows forced stop exits 1 and is not
// (0214 MADR amendment B, D2).
func (p *Plane) noteExit(eng *engine) {
	code := exitCode(eng.exitErr)
	if Orderly(code) {
		p.log.Info("control plane stopped", slog.Int("exit_code", code))
		return
	}
	p.log.Warn("control plane exited", slog.Int("exit_code", code), slog.Any("err", eng.exitErr))
}

// Orderly reports whether exit code code is an orderly stop on this host.
func Orderly(code int) bool {
	return code == 0 || code == 130 && runtime.GOOS != "windows"
}

// exitCode is err's exit status: 0 for nil, the code for an *exec.ExitError,
// and -1 otherwise, which includes a child killed by a signal.
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

// stderrLog writes the child's stderr as debug lines.
type stderrLog struct {
	log *slog.Logger
	mu  sync.Mutex
	buf []byte
}

func (w *stderrLog) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, b...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		if line := strings.TrimSpace(string(w.buf[:i])); line != "" {
			w.log.Debug("control plane stderr", slog.String("line", line))
		}
		w.buf = w.buf[i+1:]
	}
	return len(b), nil
}
