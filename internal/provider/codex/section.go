package codex

import (
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/config"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/providerauth"
)

// Section is providers.codex (0208 MADR D6): the Codex app-server JSON-RPC
// provider (MADR 0028).
type Section struct {
	// The keys every agent has. Codex's defaults: permission_timeout_seconds
	// 900, long enough to unlock the phone and answer, balancing MADR 0028's
	// default and the app-server's long-running tool expectation; prewarm
	// boots the shared app-server engine at daemon start so the first session
	// skips the ~500ms cold start; turn_stall_notice_seconds 120, because codex
	// tools often run multi-minute and a zero value left the phone with no
	// stall UI during WS blips.
	config.AgentCommon `mapstructure:",squash"`
	// StreamCoalesceMs is how long assistant/thought text is held so it can be
	// emitted as one event instead of one per model token (MADR 0024). 0
	// disables coalescing. Default 80.
	StreamCoalesceMs int `mapstructure:"stream_coalesce_ms"`
	// ApprovalPolicy is an optional override for the Codex approval policy.
	// Valid values: "untrusted", "on-request", "never". With SandboxMode it is
	// the pair a session starts in when it matches a mode; both empty seed the
	// default mode, and "never" alone is repaired to auto (seedPolicy, MADR
	// 0047 D2).
	ApprovalPolicy string `mapstructure:"approval_policy"`
	// SandboxMode is an optional override for the Codex sandbox mode.
	// Valid values: "read-only", "workspace-write", "danger-full-access". See
	// ApprovalPolicy.
	SandboxMode string `mapstructure:"sandbox_mode"`
	// AllowFullAccess advertises the "full-access" session mode, which runs
	// with no approval prompts *and* no sandbox. Off by default: auto-approve
	// is one risk, auto-approve with nothing containing it is another, so the
	// unsandboxed variant is opt-in rather than one tap away (MADR 0044 D5).
	AllowFullAccess bool `mapstructure:"allow_full_access"`
	// SandboxBrokenPolicy controls create behaviour when the Linux sandbox
	// cannot create a user namespace (MADR 0048). Valid: empty/"warn" (default),
	// "require_full_access", "refuse".
	SandboxBrokenPolicy string `mapstructure:"sandbox_broken_policy"`
	// Transport is stdio (default), unix_ws, ws, or managed_daemon_proxy.
	Transport string `mapstructure:"transport"`
	// ListenAddress is optional and only applies to ws. It must be loopback.
	ListenAddress string `mapstructure:"listen_address"`
	// WSAuthMode is capability_token or signed_bearer. Secret material is
	// always generated into the daemon runtime directory, never configured.
	WSAuthMode string `mapstructure:"ws_auth_mode"`
	// ReconnectAttempts is capped at three. Default 3.
	ReconnectAttempts int `mapstructure:"reconnect_attempts"`
	// Environments is the administrator-owned remote execution catalog. Phones
	// see only ids, roots, and observational status/info projections.
	// Config-file only: a list of tables.
	Environments []EnvironmentSection `mapstructure:"environments" env:"-"`
	// StandaloneProcessesEnabled gates the unsandboxed process/* surface.
	StandaloneProcessesEnabled bool `mapstructure:"standalone_processes_enabled"`
	// StandaloneProcessEnvAllowlist contains host-approved non-secret names.
	StandaloneProcessEnvAllowlist []string `mapstructure:"standalone_process_env_allowlist"`
}

// EnvironmentSection is one host-admin registered exec server, an entry of
// providers.codex.environments.
type EnvironmentSection struct {
	ID                    string   `mapstructure:"id"`
	ExecServerURL         string   `mapstructure:"exec_server_url"`
	ConnectTimeoutMS      int      `mapstructure:"connect_timeout_ms"`
	RuntimeWorkspaceRoots []string `mapstructure:"runtime_workspace_roots"`
}

// defaultSection is providers.codex holding its defaults. Codex is enabled
// by default and selectable from the phone's new-session provider menu;
// registration is harmless when the binary is absent.
func defaultSection() *Section {
	return &Section{
		AgentCommon: config.AgentCommon{
			Enabled:                  true,
			Bin:                      "codex",
			AlwaysApprove:            false,
			PermissionTimeoutSeconds: 900,
			Prewarm:                  false,
			// Match grok/opencode: long tool runs (e.g. full
			// `flutter test`) must surface a stall notice on the phone
			// rather than looking like a frozen agent. 0 still disables.
			TurnStallNoticeSeconds: 120,
		},
		StreamCoalesceMs:           80,
		ApprovalPolicy:             "",
		SandboxMode:                "",
		AllowFullAccess:            false,
		Transport:                  "stdio",
		ReconnectAttempts:          3,
		StandaloneProcessesEnabled: false,
	}
}

// Validate checks providers.codex.
func (s *Section) Validate() error {
	if err := s.ValidateAgent("codex"); err != nil {
		return err
	}
	if v := s.StreamCoalesceMs; v < 0 || v > config.MaxStreamCoalesceMs {
		return fmt.Errorf("providers.codex.stream_coalesce_ms must be 0..%d, got %d",
			config.MaxStreamCoalesceMs, v)
	}
	if !validApprovalPolicy(s.ApprovalPolicy) {
		return fmt.Errorf("providers.codex.approval_policy must be empty, untrusted, on-request, or never, got %q",
			s.ApprovalPolicy)
	}
	if !validSandboxMode(s.SandboxMode) {
		return fmt.Errorf("providers.codex.sandbox_mode must be empty, read-only, workspace-write, or danger-full-access, got %q",
			s.SandboxMode)
	}
	switch strings.TrimSpace(s.SandboxBrokenPolicy) {
	case "", "warn", "require_full_access", "refuse":
	default:
		return fmt.Errorf("providers.codex.sandbox_broken_policy must be empty, warn, require_full_access, or refuse, got %q",
			s.SandboxBrokenPolicy)
	}
	switch strings.TrimSpace(s.Transport) {
	case "stdio":
		if s.ListenAddress != "" || s.WSAuthMode != "" {
			return fmt.Errorf("providers.codex stdio transport rejects listen_address and ws_auth_mode")
		}
	case "unix_ws", "managed_daemon_proxy":
		if runtime.GOOS == "windows" {
			return fmt.Errorf("providers.codex transport %q is Unix-only", s.Transport)
		}
		if s.ListenAddress != "" || s.WSAuthMode != "" {
			return fmt.Errorf("providers.codex transport %q owns its endpoint and rejects WS fields", s.Transport)
		}
	case "ws":
		if address := s.ListenAddress; address != "" {
			host, _, err := net.SplitHostPort(address)
			if err != nil || !loopbackHost(host) {
				return fmt.Errorf("providers.codex.listen_address must be loopback host:port")
			}
		}
		switch s.WSAuthMode {
		case "", "capability_token", "signed_bearer":
		default:
			return fmt.Errorf("providers.codex.ws_auth_mode must be empty, capability_token, or signed_bearer")
		}
	default:
		return fmt.Errorf("providers.codex.transport must be stdio, unix_ws, ws, or managed_daemon_proxy")
	}
	if attempts := s.ReconnectAttempts; attempts < 0 || attempts > 3 {
		return fmt.Errorf("providers.codex.reconnect_attempts must be 0..3, got %d", attempts)
	}
	return s.validateExecution()
}

var codexEnvironmentName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// validateExecution checks the remote execution catalog and the standalone
// process allowlist.
func (s *Section) validateExecution() error {
	seenIDs := make(map[string]struct{}, len(s.Environments))
	for index, environment := range s.Environments {
		id := strings.TrimSpace(environment.ID)
		if id == "" {
			return fmt.Errorf("providers.codex.environments[%d] environment id must not be empty", index)
		}
		if _, exists := seenIDs[id]; exists {
			return fmt.Errorf("providers.codex.environments has duplicate environment id %q", id)
		}
		seenIDs[id] = struct{}{}
		parsed, err := url.Parse(environment.ExecServerURL)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "ws" && parsed.Scheme != "wss") || parsed.User != nil {
			return fmt.Errorf("providers.codex.environments[%d].exec_server_url must be an unauthenticated ws:// or wss:// URL", index)
		}
		if parsed.Scheme == "ws" && !loopbackHost(parsed.Hostname()) {
			return fmt.Errorf("providers.codex.environments[%d].exec_server_url ws:// is restricted to loopback", index)
		}
		if environment.ConnectTimeoutMS <= 0 || environment.ConnectTimeoutMS > 60_000 {
			return fmt.Errorf("providers.codex.environments[%d].connect_timeout_ms must be 1..60000", index)
		}
		logicalRoots := make(map[string]struct{}, len(environment.RuntimeWorkspaceRoots))
		canonicalRoots := make(map[string]struct{}, len(environment.RuntimeWorkspaceRoots))
		for _, root := range environment.RuntimeWorkspaceRoots {
			if !filepath.IsAbs(root) {
				return fmt.Errorf("providers.codex.environments[%d].runtime_workspace_roots must be absolute", index)
			}
			logical := filepath.Clean(root)
			if _, exists := logicalRoots[logical]; exists {
				return fmt.Errorf("providers.codex.environments[%d] has duplicate runtime_workspace_roots", index)
			}
			logicalRoots[logical] = struct{}{}
			canonical := logical
			if resolved, resolveErr := filepath.EvalSymlinks(logical); resolveErr == nil {
				canonical = filepath.Clean(resolved)
			}
			if _, exists := canonicalRoots[canonical]; exists {
				return fmt.Errorf("providers.codex.environments[%d] has duplicate canonical runtime_workspace_roots", index)
			}
			canonicalRoots[canonical] = struct{}{}
		}
	}
	seenNames := make(map[string]struct{}, len(s.StandaloneProcessEnvAllowlist))
	for _, rawName := range s.StandaloneProcessEnvAllowlist {
		name := strings.TrimSpace(rawName)
		if name == "" {
			return fmt.Errorf("providers.codex.standalone_process_env_allowlist contains an empty name")
		}
		if !codexEnvironmentName.MatchString(name) {
			return fmt.Errorf("providers.codex.standalone_process_env_allowlist has invalid environment name %q", name)
		}
		if codexSecretEnvironmentName(name) {
			return fmt.Errorf("providers.codex.standalone_process_env_allowlist rejects secret-like name %q", name)
		}
		if _, exists := seenNames[name]; exists {
			return fmt.Errorf("providers.codex.standalone_process_env_allowlist has duplicate name %q", name)
		}
		seenNames[name] = struct{}{}
	}
	return nil
}

func codexSecretEnvironmentName(name string) bool {
	upper := strings.ToUpper(name)
	for _, marker := range []string{"TOKEN", "SECRET", "PASSWORD", "PASSWD", "API_KEY", "PRIVATE_KEY", "CREDENTIAL"} {
		if strings.Contains(upper, marker) {
			return true
		}
	}
	return false
}

// validApprovalPolicy returns true for recognized Codex approval policy values.
func validApprovalPolicy(s string) bool {
	switch s {
	case "", "untrusted", "on-request", "never":
		return true
	}
	return false
}

// validSandboxMode returns true for recognized Codex sandbox mode values.
func validSandboxMode(s string) bool {
	switch s {
	case "", "read-only", "workspace-write", "danger-full-access":
		return true
	}
	return false
}

// loopbackHost reports whether host names this machine.
func loopbackHost(host string) bool {
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// providerConfig maps the section onto the provider's config. runtimeDir is
// the daemon's runtime directory, where generated secret material lives.
func (s *Section) providerConfig(runtimeDir string) Config {
	streamCoalesce := time.Duration(s.StreamCoalesceMs) * time.Millisecond
	executionEnvironments := make([]provider.ExecutionEnvironment, 0, len(s.Environments))
	for _, environment := range s.Environments {
		executionEnvironments = append(executionEnvironments, provider.ExecutionEnvironment{
			ID: environment.ID, ExecServerURL: environment.ExecServerURL,
			ConnectTimeout:        time.Duration(environment.ConnectTimeoutMS) * time.Millisecond,
			RuntimeWorkspaceRoots: append([]string(nil), environment.RuntimeWorkspaceRoots...),
		})
	}
	return Config{
		Bin:                           s.Bin,
		AlwaysApprove:                 s.AlwaysApprove,
		DefaultCWD:                    s.DefaultCWD,
		Model:                         s.Model,
		PermissionTimeout:             time.Duration(s.PermissionTimeoutSeconds) * time.Second,
		Prewarm:                       s.Prewarm,
		TurnStallNotice:               time.Duration(s.TurnStallNoticeSeconds) * time.Second,
		StreamCoalesce:                &streamCoalesce,
		ApprovalPolicy:                s.ApprovalPolicy,
		SandboxMode:                   s.SandboxMode,
		AllowFullAccess:               s.AllowFullAccess,
		SandboxBrokenPolicy:           s.SandboxBrokenPolicy,
		Transport:                     TransportMode(s.Transport),
		ListenAddress:                 s.ListenAddress,
		WSAuthMode:                    WSAuthMode(s.WSAuthMode),
		ReconnectAttempts:             s.ReconnectAttempts,
		ReconnectAttemptsConfigured:   true,
		RuntimeDir:                    runtimeDir,
		Environments:                  executionEnvironments,
		StandaloneProcessesEnabled:    s.StandaloneProcessesEnabled,
		StandaloneProcessEnvAllowlist: append([]string(nil), s.StandaloneProcessEnvAllowlist...),
	}
}

// Descriptor registers Codex (0208 MADR D6). Its credentials are transactional
// (MADR 0074 D21): with a coordinator, the provider is the coordinated one. The
// credential adapter is built with the configured binary. It is REQUIRED, not
// optional: the reality probe that distinguishes "no credential anywhere" from
// "a credential this coordinator cannot see" asks the CLI, and an adapter
// built without a binary answers RealityUnknown for both (MADR 0134).
func Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:      provider.IDCodex,
		Name:    "codex",
		Section: func() config.ProviderSection { return defaultSection() },
		Credentials: func(sec config.ProviderSection) providerauth.Adapter {
			return NewCredentialAdapter("codex", sec.(*Section).Bin)
		},
		New: func(b provider.Build) provider.Provider {
			conf := b.Section.(*Section).providerConfig(b.RuntimeDir)
			conf.SavedCatalogs = b.SavedCatalogs
			// The coordinated constructor differs only by carrying a credential
			// coordinator; every other behaviour is identical, which keeps the
			// activation a wiring change rather than a second code path.
			if b.Credentials != nil {
				return NewCoordinated(conf, b.Log, b.Credentials, b.Busy)
			}
			return NewWithLogger(conf, b.Log)
		},
	}
}
