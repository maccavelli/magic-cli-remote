package codex

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/maccavelli/magic-cli-remote/internal/agenterr"
	"github.com/maccavelli/magic-cli-remote/internal/command"
	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/procutil"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/providerauth"
)

type engine struct {
	cmd             *exec.Cmd
	conn            *conn
	dead            chan struct{}
	generation      int
	experimental    bool
	capabilities    *capabilityState
	identity        BinaryIdentity
	initialize      initializeMetadata
	collab          collaborationProbe
	diffUnavailable bool
	cleanup         func()
	managedLease    *managedDaemonLease
	ready           bool
}

type initializeMetadata struct {
	CodexHome      string
	UserAgent      string
	PlatformFamily string
	PlatformOS     string
}

// Provider manages a Codex app-server engine process and its sessions.
type Provider struct {
	cfg       Config
	log       *slog.Logger
	configErr error

	mu       sync.Mutex
	eng      *engine
	starting bool
	closed   bool

	sessions   map[string]*session
	generation int

	// Sandbox health (MADR 0048): workspace-write viability on this host.
	healthMu sync.RWMutex
	health   sandboxHealth // zero = unknown until probe
	// probeFn is a test seam; nil uses probeSandboxHealth.
	probeFn func(ctx context.Context, bin string) sandboxHealth

	// version is the negotiated Codex CLI version used in capability reasons.
	// Tests set it; production fills it lazily from `codex --version`.
	version string

	// models is the last successfully decoded typed catalog. Picker rows and
	// Fast/personality gates are rebuilt from this source (MADR 0080 D17).
	models []modelRecord

	// runtime is provider-global, sanitized status assembled from notifications
	// and engine metadata. It intentionally contains no raw config or paths.
	runtimeMu sync.RWMutex
	runtime   runtimeState

	doctorMu  sync.Mutex
	doctor    *doctorFlight
	doctorRun doctorRunFunc

	// coord is the credential transaction coordinator (MADR 0074 D21). It is
	// nil for a provider built the way the daemon builds one today; only the
	// coordinated constructor supplies it, which is what keeps P18 dark until
	// P20 can activate transaction and process ownership together.
	coord *providerauth.Coordinator
	// busy reports live sessions that a credential swap would disrupt. Nil
	// means never busy.
	busy func() int

	// Test seams for the externally managed lifecycle and deterministic
	// replacement backoff. Production uses the exact Codex CLI commands and
	// context-aware sleeping.
	daemonRun daemonLifecycleRunner
	sleepFn   retrySleeper
}

// New creates a Provider from config.
func New(cfg Config) *Provider {
	return NewWithLogger(cfg, nil)
}

// NewCoordinated creates a Provider whose device login runs inside a
// credential transaction (MADR 0074 D21/D22).
//
// Production daemon construction still uses New/NewWithLogger. Wiring this
// variant in is P20's job, because an owned flow is only safe once the server
// also owns its lifecycle; activating one without the other would reintroduce
// the orphaned-callback defect this repair exists to remove.
func NewCoordinated(cfg Config, log *slog.Logger, coord *providerauth.Coordinator, busy func() int) *Provider {
	p := NewWithLogger(cfg, log)
	p.coord = coord
	p.busy = busy
	return p
}

// NewWithLogger creates a Provider from config with a custom logger.
func NewWithLogger(cfg Config, log *slog.Logger) *Provider {
	if cfg.Bin == "" {
		cfg.Bin = "codex"
	}
	l := slog.Default()
	if log != nil {
		l = log
	}
	validated, configErr := cfg.validated()
	if configErr == nil {
		cfg = validated
	}
	return &Provider{
		cfg:       cfg,
		log:       l.With(slog.String("component", "provider.codex")),
		configErr: configErr,
		sessions:  make(map[string]*session),
		health:    sandboxHealth{Reason: sandboxUnknown},
		daemonRun: runDaemonLifecycle,
		sleepFn:   sleepContext,
	}
}

// sandboxHealth returns the last probe result (MADR 0048).
func (p *Provider) sandboxHealth() sandboxHealth {
	p.healthMu.RLock()
	defer p.healthMu.RUnlock()
	return p.health
}

func (p *Provider) setSandboxHealth(h sandboxHealth) {
	p.healthMu.Lock()
	p.health = h
	p.healthMu.Unlock()
	if h.OK {
		p.log.Info("codex sandbox health ok", slog.String("reason", string(h.Reason)))
	} else if h.Reason != sandboxUnknown {
		p.log.Warn("codex sandbox health degraded",
			slog.String("reason", string(h.Reason)),
			slog.String("detail", h.Detail),
		)
	}
}

// noteSandboxFailure sticky-flips health to unhealthy from mid-turn evidence.
func (p *Provider) noteSandboxFailure(detail string) {
	p.healthMu.Lock()
	defer p.healthMu.Unlock()
	if p.health.OK || p.health.Reason == sandboxUnknown || p.health.Reason == sandboxOK {
		p.health = sandboxHealth{
			OK:       false,
			Reason:   classifySandboxError(detail),
			Detail:   truncateRunes(detail, 300),
			ProbedAt: time.Now().UTC(),
		}
		p.log.Warn("codex sandbox failure observed mid-turn",
			slog.String("reason", string(p.health.Reason)),
			slog.String("detail", p.health.Detail),
		)
	}
}

func (p *Provider) runSandboxProbe(ctx context.Context) {
	fn := p.probeFn
	if fn == nil {
		fn = probeSandboxHealth
	}
	h := fn(ctx, p.cfg.Bin)
	p.setSandboxHealth(h)
}

// ID returns the provider identifier.
func (p *Provider) ID() provider.ID { return provider.IDCodex }

// Ready reports whether the engine binary is found on PATH.
func (p *Provider) Ready() bool {
	if p.configErr != nil {
		return false
	}
	_, err := exec.LookPath(p.cfg.Bin)
	return err == nil
}

// CommandTable returns the command table.
func (p *Provider) CommandTable() command.Table { return commandTable }

// maxModelListPages bounds model/list cursor following. Codex returns 7 models
// in one page today; the bound exists so a malformed or looping cursor cannot
// spin the WS handler.
const maxModelListPages = 10

// modelListEntry is one row of a codex model/list page.
//
// The field names are load-bearing and were both wrong before MADR 0043 D5:
// the response array is `data`, not `models`, and the request needs a `params`
// object — codex answers a missing one with `-32600 Invalid request: missing
// field 'params'`. Both failures used to fall into the same empty static
// catalog as "no engine", which is why an empty codex model picker looked like
// a codex limitation for a release.
type modelListEntry struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
	Hidden      bool   `json:"hidden"`
	IsDefault   bool   `json:"isDefault"`
	// DefaultReasoningEffort and InputModalities are display metadata; codex
	// exposes reasoning effort per model and the daemon shows it as a badge.
	DefaultReasoningEffort string   `json:"defaultReasoningEffort"`
	InputModalities        []string `json:"inputModalities"`
	// SupportedReasoningEfforts is the model's own ladder, with prose per rung.
	// Required in codex's schema and it genuinely varies: measured on 0.145.0,
	// gpt-5.6-sol/terra advertise six rungs (including `ultra`), luna five and
	// the 5.4/5.5 family four (MADR 0052 §1).
	SupportedReasoningEfforts []struct {
		ReasoningEffort string `json:"reasoningEffort"`
		Description     string `json:"description"`
	} `json:"supportedReasoningEfforts"`
	ServiceTiers        []serviceTier `json:"serviceTiers"`
	DefaultServiceTier  string        `json:"defaultServiceTier"`
	SupportsPersonality bool          `json:"supportsPersonality"`
}

type modelListPage struct {
	Data       []modelListEntry `json:"data"`
	NextCursor string           `json:"nextCursor"`
}

// ListModels returns the model catalog from the engine.
//
// It works with no thread open — model/list is an app-server request, not a
// thread one — so codex supports pre-session model selection like every other
// provider.
func (p *Provider) ListModels(ctx context.Context) (picker.Catalog, error) {
	fallback := picker.SingleCatalog(picker.SourceStatic, nil, p.cfg.Model, true)
	if _, err := p.ensureEngine(ctx); err != nil {
		p.log.Warn("list models: engine unavailable", slog.String("err", err.Error()))
		return fallback, nil
	}
	fr := p.framer()
	if fr == nil {
		p.log.Warn("list models: engine not running")
		return fallback, nil
	}
	cat, recs, err := collectModels(ctx, fr.sendReadOnlyRequest, p.cfg.Model, p.log)
	if err == nil && len(recs) > 0 {
		p.mu.Lock()
		p.models = recs
		p.mu.Unlock()
	}
	return cat, err
}

func (p *Provider) cachedModels() []modelRecord {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	recs := p.models
	p.mu.Unlock()
	if len(recs) > 0 {
		return recs
	}
	return nil
}

// rpcSender is the slice of the engine connection catalog code needs. Taking it
// as a function lets the paging, the `params` shape and the `data` field name
// be tested without an engine — all three were wrong at once before, and the
// only reason that survived a release is that nothing could exercise them.
type rpcSender func(ctx context.Context, method string, params any) (json.RawMessage, error)

func listModelsVia(ctx context.Context, send rpcSender, cfgModel string, log *slog.Logger) (picker.Catalog, error) {
	cat, _, err := collectModels(ctx, send, cfgModel, log)
	return cat, err
}

func collectModels(ctx context.Context, send rpcSender, cfgModel string, log *slog.Logger) (picker.Catalog, []modelRecord, error) {
	fallback := picker.SingleCatalog(picker.SourceStatic, nil, cfgModel, true)
	opts := make([]picker.Option, 0, 8)
	recs := make([]modelRecord, 0, 8)
	cursor := ""
	for page := 0; page < maxModelListPages; page++ {
		// An empty object, never nil: codex rejects a request with no params.
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		raw, err := send(ctx, "model/list", params)
		if err != nil {
			// Loud, not silent: this is exactly the failure that hid two bugs.
			log.Warn("list models: model/list failed",
				slog.Int("page", page), slog.String("err", err.Error()))
			return fallback, nil, nil
		}
		var resp modelListPage
		if err := json.Unmarshal(raw, &resp); err != nil {
			log.Warn("list models: model/list decode failed", slog.String("err", err.Error()))
			return fallback, nil, nil
		}
		for _, m := range resp.Data {
			if m.ID == "" || m.Hidden {
				continue
			}
			rec, err := decodeModelListEntry(m)
			if err != nil {
				log.Warn("list models: skip malformed model",
					slog.String("id", m.ID), slog.String("err", err.Error()))
				continue
			}
			recs = append(recs, rec)
			opts = append(opts, rec.pickerOption())
		}
		if resp.NextCursor == "" || resp.NextCursor == cursor {
			break
		}
		cursor = resp.NextCursor
	}
	if len(opts) == 0 {
		// An engine that answered with nothing is still a live answer, but an
		// empty picker is indistinguishable from a broken one — say so.
		log.Warn("list models: engine returned no models")
		return fallback, nil, nil
	}
	defaultID := catalogDefaultID(recs, cfgModel)
	// Codex reports no release dates, so ordering only pins the default first
	// and leaves the engine's own order intact (MADR 0043 D3).
	return picker.SingleCatalog(picker.SourceLive,
		picker.OrderModels(opts, defaultID), defaultID, true), recs, nil
}

// EnsureServer starts the engine asynchronously if not already running.
func (p *Provider) EnsureServer() {
	if !p.Ready() {
		return
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				p.log.Error("engine pre-start panic", slog.Any("recover", r), slog.String("stack", string(debug.Stack())))
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), engineStartTimeout)
		defer cancel()
		if _, err := p.ensureEngine(ctx); err != nil {
			p.log.Warn("engine pre-start failed",
				slog.String("bin", p.cfg.Bin), slog.String("err", err.Error()))
		}
	}()
}

// enginePollInterval paces callers waiting on another goroutine's in-flight
// engine start. It must never be zero: a spin here burns a whole core per
// waiter and, because ws async handlers run on a per-connection slot budget,
// wedges every other operation on that connection behind it.
const enginePollInterval = 200 * time.Millisecond

func (p *Provider) ensureEngine(ctx context.Context) (*conn, error) {
	if p.configErr != nil {
		return nil, p.configErr
	}
	for {
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			return nil, fmt.Errorf("provider shut down")
		}
		if p.eng != nil && (p.eng.ready || p.eng.cmd == nil) {
			cn := p.eng.conn
			p.mu.Unlock()
			return cn, nil
		}
		if !p.starting {
			p.starting = true
			p.mu.Unlock()
			break
		}
		p.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(enginePollInterval):
		}
	}
	// Bound the handshake on our own clock as well as the caller's: session
	// creates arrive with a deliberately cancel-free context (a phone that
	// drops mid-create must not abort the launch), so a wedged engine would
	// otherwise hold the starting gate — and every waiter — forever.
	startCtx, cancel := context.WithTimeout(ctx, engineStartTimeout)
	defer cancel()
	fr, err := p.startEngine(startCtx)
	p.mu.Lock()
	p.starting = false
	p.mu.Unlock()
	return fr, err
}

type engineAttempt struct {
	cmd          *exec.Cmd
	conn         *conn
	waitCh       chan error
	dead         chan struct{}
	stderr       *lineRing
	cleanup      func()
	managedLease *managedDaemonLease
}

func (p *Provider) launchEngineProcess(ctx context.Context, identity BinaryIdentity) (*engineAttempt, error) {
	cfg := p.cfg
	var endpoint string
	var auth *webSocketAuth
	var lease *managedDaemonLease
	var socketPath string

	switch cfg.Transport {
	case TransportStdio:
	case TransportUnixWS:
		dir := filepath.Join(cfg.RuntimeDir, "codex")
		if cfg.RuntimeDir == "" {
			return nil, fmt.Errorf("unix_ws requires the daemon runtime directory")
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("create Codex runtime directory: %w", err)
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			return nil, fmt.Errorf("secure Codex runtime directory: %w", err)
		}
		if cfg.ListenAddress != "" {
			socketPath = cfg.ListenAddress
		} else {
			socketPath = filepath.Join(dir, "app-server-"+uuid.NewString()+".sock")
		}
		endpoint = socketPath
	case TransportWS:
		listen := cfg.ListenAddress
		if listen == "" {
			listen = "127.0.0.1:0"
		}
		reserved, err := net.Listen("tcp", listen)
		if err != nil {
			return nil, fmt.Errorf("reserve Codex WebSocket port: %w", err)
		}
		endpoint = reserved.Addr().String()
		if err := reserved.Close(); err != nil {
			return nil, fmt.Errorf("release reserved Codex WebSocket port: %w", err)
		}
		mode := cfg.WSAuthMode
		if mode == "" {
			mode = WSAuthCapabilityToken
		}
		auth, err = createWebSocketAuth(cfg.RuntimeDir, mode)
		if err != nil {
			return nil, err
		}
		cfg.WSAuthMode = mode
	case TransportManagedDaemonProxy:
		out, err := p.daemonRun(ctx, cfg.Bin, "start")
		if err != nil {
			return nil, err
		}
		lease, err = validateManagedLease(out, identity)
		if err != nil {
			return nil, err
		}
		endpoint = lease.SocketPath
	default:
		return nil, fmt.Errorf("unsupported Codex transport %q", cfg.Transport)
	}

	secretFile := ""
	if auth != nil {
		secretFile = auth.secretFile
	}
	args, err := launchArguments(cfg, endpoint, secretFile)
	if err != nil {
		if auth != nil {
			auth.remove()
		}
		if lease != nil {
			p.stopManagedLease(context.Background(), lease)
		}
		return nil, err
	}
	cmd := exec.Command(p.cfg.Bin, args...)
	procutil.SetProcessGroup(cmd)
	procutil.SetDeathSignal(cmd)
	// Stamp ownership into the environment (Linux reaping) and registry (cross-platform).
	engineID := uuid.NewString()
	cmd.Env = append(os.Environ(),
		procutil.EnvEngineID+"="+engineID,
		procutil.EnvEngineOwner+"="+procutil.OwnerToken(),
	)

	var stdin io.WriteCloser
	var stdout io.ReadCloser
	cleanupPrepared := func() {
		if auth != nil {
			auth.remove()
		}
		if lease != nil {
			p.stopManagedLease(context.Background(), lease)
		}
	}
	if cfg.Transport == TransportStdio || cfg.Transport == TransportManagedDaemonProxy {
		stdin, err = cmd.StdinPipe()
		if err != nil {
			cleanupPrepared()
			return nil, fmt.Errorf("stdin pipe: %w", err)
		}
		stdout, err = cmd.StdoutPipe()
		if err != nil {
			_ = stdin.Close()
			cleanupPrepared()
			return nil, fmt.Errorf("stdout pipe: %w", err)
		}
	} else {
		cmd.Stdin = nil
		cmd.Stdout = io.Discard
	}
	stderr := &lineRing{
		log:    p.log,
		prefix: "codex-stderr",
		max:    20,
		// Surface silent 429/quota/backoff from engine stderr the same way
		// goose/acphttp does (MADR 0073 F1).
		onLine: p.onEngineLogLine,
	}
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		if auth != nil {
			auth.remove()
		}
		if lease != nil {
			p.stopManagedLease(context.Background(), lease)
		}
		return nil, fmt.Errorf("start %s: %w", p.cfg.Bin, err)
	}
	registryLease, regErr := procutil.RegisterEngine("", procutil.EngineRecord{
		ID:       engineID,
		Provider: "codex",
		PID:      cmd.Process.Pid,
		PGID:     cmd.Process.Pid,
		Owner:    procutil.OwnerToken(),
	})
	if regErr != nil {
		_ = procutil.KillProcessGroup(cmd.Process)
		_, _ = cmd.Process.Wait()
		if auth != nil {
			auth.remove()
		}
		if lease != nil {
			p.stopManagedLease(context.Background(), lease)
		}
		return nil, fmt.Errorf("register engine: %w", regErr)
	}

	waitCh := make(chan error, 1)
	dead := make(chan struct{})
	go func() {
		defer func() {
			if r := recover(); r != nil {
				p.log.Error("cmd wait panic", slog.Any("recover", r), slog.String("stack", string(debug.Stack())))
			}
		}()
		waitCh <- cmd.Wait()
		_ = procutil.RemoveEngine(registryLease)
		close(dead)
	}()

	cleanup := func() {
		if auth != nil {
			auth.remove()
		}
		if socketPath != "" {
			_ = os.Remove(socketPath)
		}
	}
	var tr transport
	switch cfg.Transport {
	case TransportStdio:
		tr = newJSONLTransport(stdin, stdout)
	case TransportUnixWS:
		tr, err = dialTransportWithBackoff(ctx, func(ctx context.Context) (transport, error) {
			return dialUnixWebSocketTransport(ctx, endpoint, nil)
		})
		if err == nil {
			err = os.Chmod(endpoint, 0o600)
		}
	case TransportWS:
		baseURL := "http://" + endpoint
		if err = waitWebSocketHealth(ctx, baseURL, http.DefaultClient); err == nil {
			tr, err = dialWebSocketTransportWithHeaders(ctx, "ws://"+endpoint, nil, auth.header)
		}
	case TransportManagedDaemonProxy:
		tr, err = dialPipeWebSocketTransport(ctx, stdout, stdin)
	}
	if err != nil {
		_ = procutil.KillProcessGroup(cmd.Process)
		<-waitCh
		cleanup()
		if lease != nil {
			p.stopManagedLease(context.Background(), lease)
		}
		return nil, fmt.Errorf("connect Codex %s transport: %w", cfg.Transport, err)
	}
	cn := newTransportConn(tr, p.log)
	// Start consuming JSON-RPC frames before the initialize request. The
	// handshake itself is a request, so waiting for its response before the
	// read pump runs leaves the response in stdout unread until the caller's
	// context expires.
	go cn.readPump(p.routeNotification, p.routeServerRequest)
	return &engineAttempt{cmd: cmd, conn: cn, waitCh: waitCh, dead: dead, stderr: stderr, cleanup: cleanup, managedLease: lease}, nil
}

func dialTransportWithBackoff(ctx context.Context, dial func(context.Context) (transport, error)) (transport, error) {
	delay := 25 * time.Millisecond
	var lastErr error
	for {
		tr, err := dial(ctx)
		if err == nil {
			return tr, nil
		}
		lastErr = err
		if err := sleepContext(ctx, delay); err != nil {
			return nil, fmt.Errorf("transport readiness: %w", lastErr)
		}
		if delay < 400*time.Millisecond {
			delay *= 2
		}
	}
}

func (p *Provider) stopManagedLease(ctx context.Context, lease *managedDaemonLease) {
	if lease == nil {
		return
	}
	verifyCtx, cancel := context.WithTimeout(ctx, engineStopTimeout)
	defer cancel()
	current, err := p.daemonRun(verifyCtx, p.cfg.Bin, "version")
	if err != nil || !lifecycleMatchesLease(current, lease) {
		p.log.Warn("refusing to stop unverified Codex daemon lease", slog.Any("err", err))
		return
	}
	stopped, err := p.daemonRun(verifyCtx, p.cfg.Bin, "stop")
	if err != nil || stopped.Status != daemonStopped {
		p.log.Warn("owned Codex daemon stop failed", slog.Any("err", err))
	}
}

func (p *Provider) reapAttempt(att *engineAttempt) {
	if att == nil || att.cmd == nil || att.cmd.Process == nil {
		return
	}
	_ = procutil.KillProcessGroup(att.cmd.Process)
	<-att.waitCh
	_ = att.conn.transport.Close()
	if att.cleanup != nil {
		att.cleanup()
	}
	p.stopManagedLease(context.Background(), att.managedLease)
}

func initializeParams(experimental bool) map[string]any {
	return initializeParamsWithProfile(experimental, MCPClientExtensionProfile{})
}

// MCPClientExtensionProfile is immutable for every thread on an initialized
// connection. P1 defaults to the empty profile; P2/P13 enable members only
// after their complete typed renderer is available.
type MCPClientExtensionProfile struct {
	OpenAIForm        bool
	StandardFormInput bool
	MCPAppUI          bool
}

func initializeParamsWithProfile(experimental bool, profile MCPClientExtensionProfile) map[string]any {
	extensions := make(map[string]any, 3)
	if profile.OpenAIForm {
		extensions["openai/form"] = map[string]any{}
	}
	if profile.StandardFormInput {
		extensions["openai/standard-form-input"] = map[string]any{}
	}
	if profile.MCPAppUI {
		extensions["io.modelcontextprotocol/ui"] = map[string]any{
			"mimeTypes": []string{"text/html;profile=mcp-app"},
		}
	}
	return map[string]any{
		"clientInfo": map[string]string{
			"name":    "mcremote",
			"title":   "magic-cli-remote",
			"version": "dev",
		},
		"capabilities": map[string]any{
			"experimentalApi":           experimental,
			"requestAttestation":        false,
			"optOutNotificationMethods": []string{},
			"extensions":                extensions,
		},
	}
}

func initializeParamsWithLegacyForm(experimental bool) map[string]any {
	return map[string]any{
		"clientInfo": map[string]string{
			"name":    "mcremote",
			"title":   "magic-cli-remote",
			"version": "dev",
		},
		"capabilities": map[string]any{
			"experimentalApi":                experimental,
			"requestAttestation":             false,
			"optOutNotificationMethods":      []string{},
			"mcpServerOpenaiFormElicitation": true,
		},
	}
}

func (p *Provider) initializeConn(ctx context.Context, cn *conn, experimental bool) (json.RawMessage, error) {
	return cn.sendRequest(ctx, "initialize", initializeParams(experimental))
}

func (p *Provider) startEngine(ctx context.Context) (*conn, error) {
	experimental := true
	identity, identityErr := resolveBinaryIdentity(ctx, p.cfg.Bin, p.version)
	if identityErr != nil {
		return nil, identityErr
	}
	if identity.Version != "" && identity.Version != "unknown" && identity.Version != "test-helper" {
		p.version = identity.Version
	}
	att, err := p.launchEngineProcess(ctx, identity)
	if err != nil {
		return nil, err
	}
	raw, err := p.initializeConn(ctx, att.conn, true)
	if isExperimentalInitRejection(err) {
		p.log.Info("codex initialize rejected experimental API; retrying once",
			slog.String("method", "initialize"),
			slog.String("codex_version", p.versionLabel()),
			slog.String("reason", reasonExperimentalUnavailable(p.versionLabel())),
		)
		p.reapAttempt(att)
		experimental = false
		att, err = p.launchEngineProcess(ctx, identity)
		if err != nil {
			return nil, err
		}
		raw, err = p.initializeConn(ctx, att.conn, false)
	}
	if err != nil {
		tail := ""
		if att.stderr != nil {
			tail = att.stderr.tail()
		}
		p.reapAttempt(att)
		if tail != "" {
			provider.LogStderrTail(p.log, p.cfg.Bin, tail)
			return nil, fmt.Errorf("initialize: %w; stderr:\n%s", err, tail)
		}
		return nil, fmt.Errorf("initialize: %w", err)
	}

	_ = att.conn.sendNotification(ctx, "initialized", nil)

	var initResp struct {
		CodexHome      string `json:"codexHome"`
		UserAgent      string `json:"userAgent"`
		PlatformFamily string `json:"platformFamily"`
		PlatformOS     string `json:"platformOs"`
	}
	if err := json.Unmarshal(raw, &initResp); err != nil {
		p.log.Debug("codex: initialize response parse", slog.String("err", err.Error()))
	}
	if initResp.CodexHome != "" {
		p.log.Debug("codex: engine ready", slog.String("codex_home", initResp.CodexHome))
	}

	manifest, err := loadEmbeddedContractManifest()
	if err != nil {
		p.reapAttempt(att)
		return nil, err
	}

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		p.reapAttempt(att)
		return nil, fmt.Errorf("provider shut down")
	}
	gen := p.generation + 1
	snapshot, err := buildCapabilitySnapshot(manifest, identity, gen, experimental, time.Now().UTC())
	if err != nil {
		p.mu.Unlock()
		p.reapAttempt(att)
		return nil, err
	}
	p.generation = gen
	eng := &engine{
		cmd:          att.cmd,
		conn:         att.conn,
		dead:         att.dead,
		generation:   gen,
		experimental: experimental,
		capabilities: newCapabilityState(snapshot),
		identity:     identity,
		initialize: initializeMetadata{
			CodexHome: initResp.CodexHome, UserAgent: initResp.UserAgent,
			PlatformFamily: initResp.PlatformFamily, PlatformOS: initResp.PlatformOS,
		},
		cleanup:      att.cleanup,
		managedLease: att.managedLease,
	}
	p.eng = eng
	p.models = nil
	p.mu.Unlock()

	p.probeCollaboration(ctx, eng)
	p.reconcileRetainedSessions(ctx, eng)
	p.mu.Lock()
	if p.eng == eng {
		eng.ready = true
	}
	p.mu.Unlock()
	p.log.Info("engine ready", slog.String("bin", p.cfg.Bin), slog.Bool("experimental", experimental))

	// MADR 0048 / 0071 F1: probe workspace-write sandbox after initialize.
	// Non-Linux short-circuits immediately (Seatbelt/0069). On Linux run in
	// the background so engine start is not blocked up to 8s; sessions that
	// race the probe see health "unknown" (no notice) until it lands.
	go func() {
		defer func() {
			if r := recover(); r != nil {
				p.log.Error("sandbox probe panic", slog.Any("recover", r), slog.String("stack", string(debug.Stack())))
			}
		}()
		probeCtx, probeCancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer probeCancel()
		p.runSandboxProbe(probeCtx)
	}()

	go func() {
		defer func() {
			if r := recover(); r != nil {
				p.log.Error("engine death monitor panic", slog.Any("recover", r), slog.String("stack", string(debug.Stack())))
			}
		}()
		err := <-att.waitCh
		p.handleUnexpectedEngineExit(gen, att, err)
	}()

	return att.conn, nil
}

var engineReconnectBackoffs = []time.Duration{250 * time.Millisecond, time.Second, 4 * time.Second}

func (p *Provider) handleUnexpectedEngineExit(gen int, att *engineAttempt, exitErr error) {
	p.mu.Lock()
	if p.generation != gen || p.closed || p.eng == nil || p.eng.generation != gen {
		p.mu.Unlock()
		return
	}
	p.eng = nil
	sessions := make([]*session, 0, len(p.sessions))
	for _, s := range p.sessions {
		sessions = append(sessions, s)
	}
	if att.managedLease != nil {
		// A proxy loss invalidates the managed lease. The safe recovery path is
		// the configured stdio fallback; never attach to a possibly foreign
		// daemon on the next attempt.
		p.cfg.Transport = TransportStdio
		p.cfg.ListenAddress = ""
		p.cfg.WSAuthMode = ""
	}
	p.mu.Unlock()

	if att.cleanup != nil {
		att.cleanup()
	}
	p.stopManagedLease(context.Background(), att.managedLease)
	p.log.Warn("engine exited", slog.String("bin", p.cfg.Bin), slog.Any("err", exitErr))
	for _, s := range sessions {
		s.engineLost()
	}

	attempts := p.cfg.ReconnectAttempts
	if attempts > len(engineReconnectBackoffs) {
		attempts = len(engineReconnectBackoffs)
	}
	for attempt := 0; attempt < attempts; attempt++ {
		p.mu.Lock()
		closed := p.closed
		p.mu.Unlock()
		if closed {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), engineStartTimeout)
		err := p.sleepFn(ctx, engineReconnectBackoffs[attempt])
		if err == nil {
			_, err = p.ensureEngine(ctx)
		}
		cancel()
		if err == nil {
			p.log.Info("Codex engine replacement ready", slog.Int("previous_generation", gen), slog.Int("attempt", attempt+1))
			return
		}
		p.log.Warn("Codex engine replacement failed", slog.Int("attempt", attempt+1), slog.Any("err", err))
	}
}

func (p *Provider) reconcileRetainedSessions(ctx context.Context, eng *engine) {
	p.mu.Lock()
	sessions := make([]*session, 0, len(p.sessions))
	for _, s := range p.sessions {
		s.mu.Lock()
		stale := !s.closed && s.engineGeneration < eng.generation
		s.mu.Unlock()
		if stale {
			sessions = append(sessions, s)
		}
	}
	p.mu.Unlock()
	if len(sessions) == 0 {
		return
	}

	loaded := make(map[string]struct{})
	raw, err := sendWithOverloadRetry(ctx, "thread/loaded/list", map[string]any{}, true, eng.conn.sendRequest, p.sleepFn)
	if err == nil {
		var response struct {
			Data []string `json:"data"`
		}
		if json.Unmarshal(raw, &response) == nil {
			for _, id := range response.Data {
				loaded[id] = struct{}{}
			}
		}
	} else {
		p.log.Warn("Codex loaded-thread reconciliation failed", slog.Any("err", err))
	}
	for _, s := range sessions {
		if _, ok := loaded[s.AgentSessionID()]; ok {
			s.reconnected(eng.generation)
			continue
		}
		if err := s.resumeAfterReplacement(ctx, eng.conn, eng.generation); err != nil {
			s.reconnectFailed(err)
		}
	}
}

func resolveBinaryIdentity(ctx context.Context, bin, versionHint string) (BinaryIdentity, error) {
	path, err := exec.LookPath(bin)
	if err != nil {
		return BinaryIdentity{}, fmt.Errorf("resolve codex binary: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		path = resolved
	}
	file, err := os.Open(path)
	if err != nil {
		return BinaryIdentity{}, fmt.Errorf("open codex binary: %w", err)
	}
	hash := sha256.New()
	_, copyErr := io.Copy(hash, file)
	closeErr := file.Close()
	if copyErr != nil {
		return BinaryIdentity{}, fmt.Errorf("hash codex binary: %w", copyErr)
	}
	if closeErr != nil {
		return BinaryIdentity{}, fmt.Errorf("close codex binary: %w", closeErr)
	}
	version := strings.TrimSpace(versionHint)
	if version == "" {
		executable, _ := os.Executable()
		if os.Getenv("GO_WANT_CODEX_APP_SERVER_HELPER") == "1" && sameResolvedPath(path, executable) {
			version = "test-helper"
		} else {
			versionCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			out, versionErr := exec.CommandContext(versionCtx, path, "--version").CombinedOutput()
			if versionErr == nil {
				version = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(out)), "codex-cli "))
			}
		}
	}
	if version == "" {
		version = "unknown"
	}
	return BinaryIdentity{Path: path, Version: version, SHA256: fmt.Sprintf("%x", hash.Sum(nil))}, nil
}

func sameResolvedPath(left, right string) bool {
	leftResolved, leftErr := filepath.EvalSymlinks(left)
	rightResolved, rightErr := filepath.EvalSymlinks(right)
	return leftErr == nil && rightErr == nil && leftResolved == rightResolved
}

func (p *Provider) supportsCapability(id CapabilityID) bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.eng == nil {
		return false
	}
	if p.eng.capabilities != nil {
		return p.eng.capabilities.Supports(id)
	}
	switch id {
	case CapabilityThreadSearch, CapabilityThreadSettings, CapabilityCollaborationModes, CapabilityThreadForkDeferGoal:
		return p.eng.experimental
	case CapabilityThreadList, CapabilityThreadSource:
		return true
	default:
		return false
	}
}

func (p *Provider) disableCapability(id CapabilityID, reason CapabilityDenial) {
	p.mu.Lock()
	if p.eng != nil && p.eng.capabilities != nil {
		p.eng.capabilities.Disable(id, reason)
	}
	p.mu.Unlock()
}

// Shutdown stops the engine process.
func (p *Provider) Shutdown() {
	p.mu.Lock()
	p.closed = true
	eng := p.eng
	p.eng = nil
	p.mu.Unlock()

	if eng != nil && eng.cmd != nil && eng.cmd.Process != nil {
		_ = eng.conn.transport.Close()
		procutil.TerminateProcessGroup(eng.cmd.Process, eng.dead, engineStopTimeout)
		if eng.cleanup != nil {
			eng.cleanup()
		}
		p.stopManagedLease(context.Background(), eng.managedLease)
	}
}

// Start creates a new session with the engine.
func (p *Provider) Start(ctx context.Context, opts provider.StartOptions) (provider.Session, error) {
	_, err := p.ensureEngine(ctx)
	if err != nil {
		return nil, err
	}
	// Refuse create under refuse / require_full_access without gate (0048).
	h := p.sandboxHealth()
	approval, sandbox, modeID := seedPolicy(p.cfg)
	if _, _, _, err := applySandboxBrokenPolicy(p.cfg, h, approval, sandbox, modeID); err != nil {
		return nil, err
	}
	return p.startSession(ctx, opts)
}

func (p *Provider) startSession(ctx context.Context, opts provider.StartOptions) (*session, error) {
	fr := p.framer()
	if fr == nil {
		return nil, fmt.Errorf("engine not running")
	}

	// Shared resolution + errno-preserving validation (0069 P1): codex
	// previously fell back to os.Getwd() with no validation at all.
	cwd, err := provider.ResolveSessionCWD(opts.CWD, p.cfg.DefaultCWD, nil)
	if err != nil {
		return nil, err
	}
	opts.CWD = cwd
	s := newSession(p, p.cfg, opts, p.log)
	if err := s.create(ctx, fr); err != nil {
		return nil, fmt.Errorf("session create: %w", err)
	}

	p.mu.Lock()
	if p.eng != nil {
		s.engineGeneration = p.eng.generation
	}
	p.sessions[s.agentID] = s
	p.mu.Unlock()
	go s.hydrateGoalAsync()

	return s, nil
}

func (p *Provider) framer() *conn {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.eng == nil {
		return nil
	}
	return p.eng.conn
}

func (p *Provider) routeNotification(method string, params json.RawMessage) {
	switch notificationRouteFor(method) {
	case notificationRouteProvider:
		p.handleProviderNotification(method, params)
		return
	case notificationRouteUnknown:
		p.log.Debug("codex: unknown notification", slog.String("method", method))
		return
	}
	var info struct {
		ThreadID string `json:"threadId"`
	}
	_ = json.Unmarshal(params, &info)
	if info.ThreadID != "" {
		p.mu.Lock()
		s := p.sessions[info.ThreadID]
		p.mu.Unlock()
		if s != nil {
			s.handleNotification(method, params)
			return
		}
	}
	p.log.Debug("codex: session notification has no live thread", slog.String("method", method))
}

func (p *Provider) routeServerRequest(method string, id json.RawMessage, params json.RawMessage) {
	var info struct {
		ThreadID       string `json:"threadId"`
		ConversationID string `json:"conversationId"`
	}
	_ = json.Unmarshal(params, &info)
	threadID := firstNonEmpty(info.ThreadID, info.ConversationID)
	if threadID != "" {
		p.mu.Lock()
		s := p.sessions[threadID]
		p.mu.Unlock()
		if s != nil {
			s.handleServerRequest(method, id, params)
			return
		}
	}
	p.log.Debug("codex: unhandled server request", slog.String("method", method))
	fr := p.framer()
	if fr != nil {
		_ = fr.sendResponse(context.Background(), id, nil, &rpcErrorBody{
			Code: -32000, Message: "unknown thread",
		})
	}
}

type lineRing struct {
	log    *slog.Logger
	prefix string
	max    int
	// onLine is invoked once per complete stderr line (outside the write
	// lock). Optional; used to surface silent provider limits mid-turn.
	onLine func(string)

	mu   sync.Mutex
	ring []string
	buf  []byte
}

func (w *lineRing) Write(p []byte) (int, error) {
	var lines []string
	w.mu.Lock()
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			if len(w.buf) > 4096 {
				line := string(w.buf[:4096]) + "…"
				w.pushLocked(line)
				lines = append(lines, line)
				w.buf = w.buf[:0]
			}
			break
		}
		line := strings.TrimSpace(string(w.buf[:i]))
		n := copy(w.buf, w.buf[i+1:])
		w.buf = w.buf[:n]
		if line != "" {
			w.pushLocked(line)
			lines = append(lines, line)
		}
	}
	w.mu.Unlock()
	if w.onLine != nil {
		for _, line := range lines {
			w.onLine(line)
		}
	}
	return len(p), nil
}

func (w *lineRing) pushLocked(line string) {
	if w.log != nil {
		w.log.Debug(w.prefix, slog.String("line", line))
	}
	if w.max <= 0 {
		return
	}
	w.ring = append(w.ring, line)
	if len(w.ring) > w.max {
		w.ring = append([]string(nil), w.ring[len(w.ring)-w.max:]...)
	}
}

// onEngineLogLine fans provider limit signals to every busy session so a
// silent codex retry does not leave the phone on status=running forever.
func (p *Provider) onEngineLogLine(line string) {
	if !agenterr.IsLimit(line, time.Now()) {
		return
	}
	p.mu.Lock()
	sessions := make([]*session, 0, len(p.sessions))
	for _, s := range p.sessions {
		sessions = append(sessions, s)
	}
	p.mu.Unlock()
	for _, s := range sessions {
		s.noteProviderLimit(line)
	}
}

func (w *lineRing) tail() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.ring) == 0 {
		return ""
	}
	out := ""
	for i, line := range w.ring {
		if i > 0 {
			out += "\n"
		}
		out += line
	}
	return out
}
