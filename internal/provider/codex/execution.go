package codex

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

const (
	maxTerminalReplayBytes  = 1 << 20
	maxTerminalPage         = 100
	maxExecutionOutput      = 256 << 10
	maxExecutionInput       = 256 << 10
	defaultExecutionTimeout = 5 * time.Minute
)

type terminalKey struct {
	Generation int
	ThreadID   string
	ID         string
}

type terminalRecord struct {
	info   provider.TerminalInfo
	chunks []provider.TerminalOutput
	bytes  int
	next   uint64
}

type terminalRegistry struct {
	mu       sync.Mutex
	maxBytes int
	records  map[terminalKey]*terminalRecord
}

func newTerminalRegistry(maxBytes int) *terminalRegistry {
	if maxBytes <= 0 {
		maxBytes = maxTerminalReplayBytes
	}
	return &terminalRegistry{maxBytes: maxBytes, records: make(map[terminalKey]*terminalRecord)}
}

func (r *terminalRegistry) Register(key terminalKey, info provider.TerminalInfo) error {
	if key.Generation <= 0 || key.ID == "" {
		return errors.New("terminal generation and id are required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.records[key]; exists {
		return errors.New("duplicate terminal")
	}
	info.ID = key.ID
	info.ThreadID = key.ThreadID
	info.Generation = key.Generation
	info.Running = true
	r.records[key] = &terminalRecord{info: info}
	return nil
}

func (r *terminalRegistry) Append(key terminalKey, stream string, data []byte, capReached bool) (provider.TerminalOutput, error) {
	if stream != "stdout" && stream != "stderr" {
		return provider.TerminalOutput{}, errors.New("terminal stream must be stdout or stderr")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	record := r.records[key]
	if record == nil {
		return provider.TerminalOutput{}, provider.ErrTerminalNotFound
	}
	record.next++
	chunk := provider.TerminalOutput{TerminalID: key.ID, Sequence: record.next, Stream: stream, Data: append([]byte(nil), data...), CapReached: capReached}
	record.chunks = append(record.chunks, chunk)
	record.bytes += len(data)
	for record.bytes > r.maxBytes && len(record.chunks) > 0 {
		record.bytes -= len(record.chunks[0].Data)
		record.chunks = record.chunks[1:]
	}
	return chunk, nil
}

func (r *terminalRegistry) Replay(key terminalKey, after uint64) ([]provider.TerminalOutput, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record := r.records[key]
	if record == nil {
		return nil, false, provider.ErrTerminalNotFound
	}
	gap := len(record.chunks) > 0 && after+1 < record.chunks[0].Sequence
	out := make([]provider.TerminalOutput, 0, len(record.chunks))
	for _, chunk := range record.chunks {
		if chunk.Sequence > after {
			copyChunk := chunk
			copyChunk.Data = append([]byte(nil), chunk.Data...)
			out = append(out, copyChunk)
		}
	}
	return out, gap, nil
}

func (r *terminalRegistry) List(threadID string) []provider.TerminalInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]provider.TerminalInfo, 0, len(r.records))
	for key, record := range r.records {
		if threadID == "" || key.ThreadID == threadID {
			out = append(out, record.info)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (r *terminalRegistry) Exit(key terminalKey, exitCode int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	record := r.records[key]
	if record == nil {
		return provider.ErrTerminalNotFound
	}
	record.info.Running = false
	record.info.ExitCode = &exitCode
	return nil
}

func (r *terminalRegistry) Delete(key terminalKey) {
	r.mu.Lock()
	delete(r.records, key)
	r.mu.Unlock()
}

func (r *terminalRegistry) KeyByID(generation int, id string) (terminalKey, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for key := range r.records {
		if key.Generation == generation && key.ID == id {
			return key, true
		}
	}
	return terminalKey{}, false
}

func (r *terminalRegistry) CleanupGeneration(generation int) {
	r.mu.Lock()
	for key := range r.records {
		if key.Generation == generation {
			delete(r.records, key)
		}
	}
	r.mu.Unlock()
}

// Device and managed-session detach intentionally preserve terminals. Their
// lifetime is provider generation/process lifetime, not phone attachment.
func (r *terminalRegistry) DetachDevice(string)  {}
func (r *terminalRegistry) DetachSession(string) {}

type ownedProcess struct {
	key terminalKey
	tty bool
}

type executionAPI struct {
	send              nativeRPCSender
	supports          nativeCapability
	registry          *terminalRegistry
	generation        int
	environments      map[string]provider.ExecutionEnvironment
	standaloneEnabled bool
	envAllowlist      map[string]struct{}
	processMu         sync.Mutex
	processes         map[string]ownedProcess
	// push forwards each appended chunk to the daemon's live terminal
	// channel. It is nil in unit tests and whenever no sink is installed.
	push func(threadID string, chunk provider.TerminalOutput)
}

func newExecutionAPI(send nativeRPCSender, supports nativeCapability, registry *terminalRegistry, generation int, environments []provider.ExecutionEnvironment) *executionAPI {
	if registry == nil {
		registry = newTerminalRegistry(maxTerminalReplayBytes)
	}
	byID := make(map[string]provider.ExecutionEnvironment, len(environments))
	for _, environment := range environments {
		byID[environment.ID] = environment
	}
	return &executionAPI{send: send, supports: supports, registry: registry, generation: generation, environments: byID, envAllowlist: make(map[string]struct{}), processes: make(map[string]ownedProcess)}
}

// appendAndPush buffers one chunk and forwards it live. Buffering is
// authoritative: a failed push never loses the bytes a client can replay.
func (a *executionAPI) appendAndPush(key terminalKey, stream string, data []byte, capReached bool) (provider.TerminalOutput, error) {
	chunk, err := a.registry.Append(key, stream, data, capReached)
	if err != nil {
		return provider.TerminalOutput{}, err
	}
	if a.push != nil {
		a.push(key.ThreadID, chunk)
	}
	return chunk, nil
}

func (a *executionAPI) require(id CapabilityID) error {
	if a.supports == nil || !a.supports(id) {
		return fmt.Errorf("Codex capability %s is unavailable", id)
	}
	return nil
}

func hasControl(value string) bool {
	return strings.IndexFunc(value, unicode.IsControl) >= 0
}

func validateArgv(argv []string) error {
	if len(argv) == 0 || len(argv) > 256 {
		return errors.New("nonempty bounded argv is required")
	}
	for _, argument := range argv {
		if argument == "" || len(argument) > 16<<10 || hasControl(argument) {
			return errors.New("argv contains an empty, oversized, or control-character argument")
		}
	}
	return nil
}

func validateSize(tty bool, rows, cols int) error {
	if !tty && (rows != 0 || cols != 0) {
		return errors.New("terminal size requires tty")
	}
	if tty && ((rows != 0 || cols != 0) && (rows < 1 || rows > 65535 || cols < 1 || cols > 65535)) {
		return errors.New("terminal size must be 1..65535")
	}
	return nil
}

func executionBounds(outputCap int, timeout time.Duration) (int, time.Duration, error) {
	if outputCap < 0 || outputCap > maxTerminalReplayBytes {
		return 0, 0, errors.New("output cap must be 0..1MiB")
	}
	if timeout < 0 || timeout > time.Hour {
		return 0, 0, errors.New("timeout must be 0..1h")
	}
	if outputCap == 0 {
		outputCap = maxExecutionOutput
	}
	if timeout == 0 {
		timeout = defaultExecutionTimeout
	}
	return outputCap, timeout, nil
}

// ExecSandboxed uses argv-based command/exec and never invokes a shell.
func (a *executionAPI) ExecSandboxed(ctx context.Context, request provider.ExecRequest) (provider.ExecResult, error) {
	if err := a.require(CapabilityCommandExec); err != nil {
		return provider.ExecResult{}, err
	}
	if err := validateArgv(request.Argv); err != nil {
		return provider.ExecResult{}, err
	}
	if request.CWD != "" && !filepath.IsAbs(request.CWD) {
		return provider.ExecResult{}, errors.New("cwd must be absolute")
	}
	if err := validateSize(request.TTY, request.Rows, request.Cols); err != nil {
		return provider.ExecResult{}, err
	}
	if (request.TTY || request.Stream) && request.ProcessID == "" {
		return provider.ExecResult{}, errors.New("streaming and tty require a process id")
	}
	capBytes, timeout, err := executionBounds(request.OutputBytesCap, request.Timeout)
	if err != nil {
		return provider.ExecResult{}, err
	}
	params := map[string]any{
		"command": request.Argv, "outputBytesCap": uint64(capBytes), "timeoutMs": timeout.Milliseconds(),
		"tty": request.TTY, "streamStdin": request.Stream || request.TTY, "streamStdoutStderr": request.Stream || request.TTY,
	}
	if request.CWD != "" {
		params["cwd"] = request.CWD
	}
	if request.PermissionProfileID != "" {
		params["permissionProfile"] = request.PermissionProfileID
	}
	if request.ProcessID != "" {
		params["processId"] = request.ProcessID
	}
	if request.Env != nil {
		params["env"] = request.Env
	}
	if request.TTY && request.Rows > 0 {
		params["size"] = map[string]any{"rows": uint16(request.Rows), "cols": uint16(request.Cols)}
	}
	if request.ProcessID != "" {
		key := terminalKey{Generation: a.generation, ThreadID: request.ThreadID, ID: request.ProcessID}
		if err := a.registry.Register(key, provider.TerminalInfo{Kind: provider.TerminalKindExec, Label: provider.ExecutionLabelSandboxed, Command: strings.Join(request.Argv, " "), CWD: request.CWD, TTY: request.TTY, AuditClass: provider.ExecutionAuditCommandExec}); err != nil {
			return provider.ExecResult{}, err
		}
	}
	raw, err := a.send(ctx, "command/exec", params)
	if err != nil {
		return provider.ExecResult{}, err
	}
	var response struct {
		ExitCode int    `json:"exitCode"`
		Stdout   string `json:"stdout"`
		Stderr   string `json:"stderr"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return provider.ExecResult{}, err
	}
	if len(response.Stdout) > capBytes {
		response.Stdout = response.Stdout[:capBytes]
	}
	if len(response.Stderr) > capBytes {
		response.Stderr = response.Stderr[:capBytes]
	}
	return provider.ExecResult{ExitCode: response.ExitCode, Stdout: response.Stdout, Stderr: response.Stderr, Label: provider.ExecutionLabelSandboxed, AuditClass: provider.ExecutionAuditCommandExec}, nil
}

func (a *executionAPI) WriteExec(ctx context.Context, processID string, data []byte, closeStdin bool) error {
	if err := a.require(CapabilityCommandExecWrite); err != nil {
		return err
	}
	if processID == "" || len(data) > maxExecutionInput {
		return errors.New("bounded process id and input required")
	}
	params := map[string]any{"processId": processID, "closeStdin": closeStdin}
	if len(data) > 0 {
		params["deltaBase64"] = base64.StdEncoding.EncodeToString(data)
	}
	_, err := a.send(ctx, "command/exec/write", params)
	return err
}

func (a *executionAPI) ResizeExec(ctx context.Context, processID string, rows, cols int) error {
	if err := a.require(CapabilityCommandExecResize); err != nil {
		return err
	}
	if processID == "" || validateSize(true, rows, cols) != nil || rows == 0 || cols == 0 {
		return errors.New("bounded process id and terminal size required")
	}
	_, err := a.send(ctx, "command/exec/resize", map[string]any{"processId": processID, "size": map[string]any{"rows": uint16(rows), "cols": uint16(cols)}})
	return err
}

func (a *executionAPI) TerminateExec(ctx context.Context, processID string) error {
	if err := a.require(CapabilityCommandExecTerminate); err != nil {
		return err
	}
	if processID == "" {
		return errors.New("process id required")
	}
	_, err := a.send(ctx, "command/exec/terminate", map[string]any{"processId": processID})
	return err
}

func (a *executionAPI) RunThreadShell(ctx context.Context, threadID, command string) (provider.ExecutionResult, error) {
	if err := a.require(CapabilityThreadShellCommand); err != nil {
		return provider.ExecutionResult{}, err
	}
	command = strings.TrimSpace(command)
	if threadID == "" || command == "" || len(command) > 64<<10 || hasControl(command) {
		return provider.ExecutionResult{}, errors.New("bounded thread id and shell command required")
	}
	_, err := a.send(ctx, "thread/shellCommand", map[string]any{"threadId": threadID, "command": command})
	if err != nil {
		return provider.ExecutionResult{}, fmt.Errorf("%w: %v", provider.ErrExecutionOutcomeUnknown, err)
	}
	return provider.ExecutionResult{Started: true, Label: provider.ExecutionLabelUnsandboxed, AuditClass: provider.ExecutionAuditThreadShell}, nil
}

func boundedTerminalLimit(limit int) int {
	if limit <= 0 {
		return 50
	}
	if limit > maxTerminalPage {
		return maxTerminalPage
	}
	return limit
}

func (a *executionAPI) ListBackgroundTerminals(ctx context.Context, threadID, cursor string, limit int) (provider.TerminalPage, error) {
	if err := a.require(CapabilityBackgroundTerminalList); err != nil {
		return provider.TerminalPage{}, provider.ErrNativeUnavailable
	}
	limit = boundedTerminalLimit(limit)
	params := map[string]any{"threadId": threadID, "limit": uint32(limit)}
	if cursor != "" {
		params["cursor"] = cursor
	}
	raw, err := a.send(ctx, "thread/backgroundTerminals/list", params)
	if err != nil {
		return provider.TerminalPage{}, err
	}
	var response struct {
		Data []struct {
			ProcessID string `json:"processId"`
			Command   string `json:"command"`
			CWD       string `json:"cwd"`
		} `json:"data"`
		NextCursor *string `json:"nextCursor"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return provider.TerminalPage{}, err
	}
	if len(response.Data) > maxTerminalPage {
		return provider.TerminalPage{}, errors.New("terminal page exceeds bound")
	}
	page := provider.TerminalPage{Limit: limit}
	if response.NextCursor != nil {
		page.NextCursor = boundedPermissionText(*response.NextCursor, 1024)
	}
	for _, row := range response.Data {
		page.Terminals = append(page.Terminals, provider.TerminalInfo{ID: row.ProcessID, ThreadID: threadID, Kind: provider.TerminalKindBackground, Label: provider.ExecutionLabelSandboxed, Command: boundedPermissionText(row.Command, 4096), CWD: row.CWD, Running: true, Native: true})
	}
	return page, nil
}

func (a *executionAPI) TerminateBackgroundTerminal(ctx context.Context, threadID, processID string) (bool, error) {
	if err := a.require(CapabilityBackgroundTerminalTerminate); err != nil {
		return false, provider.ErrNativeUnavailable
	}
	raw, err := a.send(ctx, "thread/backgroundTerminals/terminate", map[string]any{"threadId": threadID, "processId": processID})
	if err != nil {
		return false, err
	}
	var response struct {
		Terminated bool `json:"terminated"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return false, err
	}
	return response.Terminated, nil
}

func (a *executionAPI) CleanBackgroundTerminals(ctx context.Context, threadID string) error {
	if err := a.require(CapabilityBackgroundTerminalClean); err != nil {
		return provider.ErrNativeUnavailable
	}
	_, err := a.send(ctx, "thread/backgroundTerminals/clean", map[string]any{"threadId": threadID})
	return err
}

var executionEnvName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func secretEnvironmentName(name string) bool {
	upper := strings.ToUpper(name)
	for _, marker := range []string{"TOKEN", "SECRET", "PASSWORD", "PASSWD", "API_KEY", "PRIVATE_KEY", "CREDENTIAL"} {
		if strings.Contains(upper, marker) {
			return true
		}
	}
	return false
}

func validateExecutionConfig(environments []provider.ExecutionEnvironment, allowlist []string) error {
	seen := make(map[string]struct{}, len(environments))
	for _, environment := range environments {
		if strings.TrimSpace(environment.ID) == "" {
			return errors.New("environment id required")
		}
		if _, exists := seen[environment.ID]; exists {
			return errors.New("duplicate environment id")
		}
		seen[environment.ID] = struct{}{}
		parsed, err := url.Parse(environment.ExecServerURL)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "ws" && parsed.Scheme != "wss") || parsed.User != nil {
			return errors.New("environment exec server must be an unauthenticated ws(s) URL")
		}
		if parsed.Scheme == "ws" && !isLoopbackHost(parsed.Hostname()) {
			return errors.New("plaintext environment WebSocket must be loopback")
		}
		if environment.ConnectTimeout <= 0 || environment.ConnectTimeout > time.Minute {
			return errors.New("environment connect timeout must be 1ms..60s")
		}
		roots := make(map[string]struct{}, len(environment.RuntimeWorkspaceRoots))
		for _, root := range environment.RuntimeWorkspaceRoots {
			if !filepath.IsAbs(root) {
				return errors.New("environment roots must be absolute")
			}
			clean := filepath.Clean(root)
			if _, exists := roots[clean]; exists {
				return errors.New("duplicate environment root")
			}
			roots[clean] = struct{}{}
		}
	}
	seen = make(map[string]struct{}, len(allowlist))
	for _, name := range allowlist {
		if !executionEnvName.MatchString(name) || secretEnvironmentName(name) {
			return errors.New("invalid or secret-like standalone environment name")
		}
		if _, exists := seen[name]; exists {
			return errors.New("duplicate standalone environment name")
		}
		seen[name] = struct{}{}
	}
	return nil
}

func (a *executionAPI) RegisterEnvironments(ctx context.Context) error {
	if len(a.environments) == 0 {
		return nil
	}
	if err := a.require(CapabilityEnvironmentAdd); err != nil {
		return err
	}
	ids := make([]string, 0, len(a.environments))
	for id := range a.environments {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		environment := a.environments[id]
		_, err := a.send(ctx, "environment/add", map[string]any{"environmentId": environment.ID, "execServerUrl": environment.ExecServerURL, "connectTimeoutMs": uint64(environment.ConnectTimeout.Milliseconds())})
		if err != nil {
			return err
		}
	}
	return nil
}

func (a *executionAPI) EnvironmentStatus(ctx context.Context, id string) (provider.EnvironmentStatus, error) {
	if _, exists := a.environments[id]; !exists {
		return provider.EnvironmentStatus{}, errors.New("unknown configured environment")
	}
	if err := a.require(CapabilityEnvironmentStatus); err != nil {
		return provider.EnvironmentStatus{}, err
	}
	raw, err := a.send(ctx, "environment/status", map[string]any{"environmentId": id})
	if err != nil {
		return provider.EnvironmentStatus{}, err
	}
	var response struct{ Status, Error string }
	if err := json.Unmarshal(raw, &response); err != nil {
		return provider.EnvironmentStatus{}, err
	}
	switch response.Status {
	case "ready", "pending", "disconnected", "unknown":
	default:
		return provider.EnvironmentStatus{}, errors.New("invalid environment status")
	}
	return provider.EnvironmentStatus{ID: id, Status: response.Status, Error: boundedPermissionText(response.Error, 1024)}, nil
}

func (a *executionAPI) EnvironmentInfo(ctx context.Context, id string) (provider.EnvironmentInfo, error) {
	if _, exists := a.environments[id]; !exists {
		return provider.EnvironmentInfo{}, errors.New("unknown configured environment")
	}
	if err := a.require(CapabilityEnvironmentInfo); err != nil {
		return provider.EnvironmentInfo{}, err
	}
	raw, err := a.send(ctx, "environment/info", map[string]any{"environmentId": id})
	if err != nil {
		return provider.EnvironmentInfo{}, err
	}
	var response struct {
		Shell struct{ Name, Path string }
		CWD   *string `json:"cwd"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return provider.EnvironmentInfo{}, err
	}
	info := provider.EnvironmentInfo{ID: id, ShellName: boundedPermissionText(response.Shell.Name, 64), ShellPath: boundedPermissionText(response.Shell.Path, 1024)}
	if response.CWD != nil {
		parsed, parseErr := url.Parse(*response.CWD)
		if parseErr != nil || parsed.Scheme != "file" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || !filepath.IsAbs(parsed.Path) {
			return provider.EnvironmentInfo{}, errors.New("environment cwd is not a canonical file URI")
		}
		info.CWD = parsed.String()
	}
	return info, nil
}

func pathWithinAny(path string, roots []string) bool {
	if !filepath.IsAbs(path) {
		return false
	}
	clean := filepath.Clean(path)
	for _, root := range roots {
		rel, err := filepath.Rel(filepath.Clean(root), clean)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func (a *executionAPI) ValidateEnvironmentSelection(id, cwd string, runtimeRoots []string) (provider.EnvironmentSelection, error) {
	environment, exists := a.environments[id]
	if !exists {
		return provider.EnvironmentSelection{}, errors.New("unknown configured environment")
	}
	if !pathWithinAny(cwd, environment.RuntimeWorkspaceRoots) {
		return provider.EnvironmentSelection{}, errors.New("environment cwd is outside allowed roots")
	}
	selection := provider.EnvironmentSelection{EnvironmentID: id, CWD: filepath.Clean(cwd)}
	seen := make(map[string]struct{}, len(runtimeRoots))
	for _, root := range runtimeRoots {
		if !pathWithinAny(root, environment.RuntimeWorkspaceRoots) {
			return provider.EnvironmentSelection{}, errors.New("runtime root is outside allowed roots")
		}
		clean := filepath.Clean(root)
		if _, exists := seen[clean]; exists {
			return provider.EnvironmentSelection{}, errors.New("duplicate runtime root")
		}
		seen[clean] = struct{}{}
		selection.RuntimeWorkspaceRoots = append(selection.RuntimeWorkspaceRoots, clean)
	}
	return selection, nil
}

func (a *executionAPI) SpawnProcess(ctx context.Context, request provider.ProcessSpawnRequest) (provider.ProcessInfo, error) {
	if !a.standaloneEnabled {
		return provider.ProcessInfo{}, errors.New("standalone processes are disabled")
	}
	if err := a.require(CapabilityProcessSpawn); err != nil {
		return provider.ProcessInfo{}, err
	}
	if err := validateArgv(request.Argv); err != nil {
		return provider.ProcessInfo{}, err
	}
	if !filepath.IsAbs(request.CWD) {
		return provider.ProcessInfo{}, errors.New("standalone cwd must be absolute")
	}
	if err := validateSize(request.TTY, request.Rows, request.Cols); err != nil {
		return provider.ProcessInfo{}, err
	}
	capBytes, timeout, err := executionBounds(request.OutputBytesCap, request.Timeout)
	if err != nil {
		return provider.ProcessInfo{}, err
	}
	for name := range request.Env {
		if !executionEnvName.MatchString(name) || secretEnvironmentName(name) {
			return provider.ProcessInfo{}, errors.New("invalid or secret-like environment name")
		}
		if _, allowed := a.envAllowlist[name]; !allowed {
			return provider.ProcessInfo{}, errors.New("environment name is not allowlisted")
		}
	}
	handle := uuid.NewString()
	params := map[string]any{
		"processHandle": handle, "command": request.Argv, "cwd": filepath.Clean(request.CWD), "env": request.Env,
		"tty": request.TTY, "streamStdin": request.Stream || request.TTY, "streamStdoutStderr": request.Stream || request.TTY,
		"outputBytesCap": uint64(capBytes), "timeoutMs": timeout.Milliseconds(),
	}
	if request.TTY && request.Rows > 0 {
		params["size"] = map[string]any{"rows": uint16(request.Rows), "cols": uint16(request.Cols)}
	}
	key := terminalKey{Generation: a.generation, ThreadID: request.ThreadID, ID: handle}
	if err := a.registry.Register(key, provider.TerminalInfo{Kind: provider.TerminalKindProcess, Label: provider.ExecutionLabelStandalone, Command: strings.Join(request.Argv, " "), CWD: request.CWD, TTY: request.TTY, AuditClass: provider.ExecutionAuditProcess}); err != nil {
		return provider.ProcessInfo{}, err
	}
	a.processMu.Lock()
	a.processes[handle] = ownedProcess{key: key, tty: request.TTY}
	a.processMu.Unlock()
	if _, err := a.send(ctx, "process/spawn", params); err != nil {
		return provider.ProcessInfo{}, fmt.Errorf("%w: %v", provider.ErrExecutionOutcomeUnknown, err)
	}
	return provider.ProcessInfo{ID: handle, Generation: a.generation, Label: provider.ExecutionLabelStandalone, AuditClass: provider.ExecutionAuditProcess}, nil
}

func (a *executionAPI) ownedProcess(handle string) (ownedProcess, error) {
	a.processMu.Lock()
	defer a.processMu.Unlock()
	process, exists := a.processes[handle]
	if !exists || process.key.Generation != a.generation {
		return ownedProcess{}, provider.ErrTerminalNotFound
	}
	return process, nil
}

func (a *executionAPI) WriteProcess(ctx context.Context, handle string, data []byte, closeStdin bool) error {
	if err := a.require(CapabilityProcessWrite); err != nil {
		return err
	}
	if _, err := a.ownedProcess(handle); err != nil {
		return err
	}
	if len(data) > maxExecutionInput {
		return errors.New("process input exceeds bound")
	}
	params := map[string]any{"processHandle": handle, "closeStdin": closeStdin}
	if len(data) > 0 {
		params["deltaBase64"] = base64.StdEncoding.EncodeToString(data)
	}
	_, err := a.send(ctx, "process/writeStdin", params)
	return err
}

func (a *executionAPI) ResizeProcess(ctx context.Context, handle string, rows, cols int) error {
	if err := a.require(CapabilityProcessResize); err != nil {
		return err
	}
	process, err := a.ownedProcess(handle)
	if err != nil {
		return err
	}
	if !process.tty || validateSize(true, rows, cols) != nil || rows == 0 || cols == 0 {
		return errors.New("process resize requires an owned tty and valid size")
	}
	_, err = a.send(ctx, "process/resizePty", map[string]any{"processHandle": handle, "size": map[string]any{"rows": uint16(rows), "cols": uint16(cols)}})
	return err
}

func (a *executionAPI) KillProcess(ctx context.Context, handle string) error {
	if err := a.require(CapabilityProcessKill); err != nil {
		return err
	}
	process, err := a.ownedProcess(handle)
	if err != nil {
		return err
	}
	if _, err := a.send(ctx, "process/kill", map[string]any{"processHandle": handle}); err != nil {
		return err
	}
	a.processMu.Lock()
	delete(a.processes, handle)
	a.processMu.Unlock()
	return a.registry.Exit(process.key, -1)
}

func (a *executionAPI) HandleProcessOutput(handle, stream, deltaBase64 string, capReached bool) (provider.TerminalOutput, error) {
	process, err := a.ownedProcess(handle)
	if err != nil {
		return provider.TerminalOutput{}, err
	}
	data, err := base64.StdEncoding.DecodeString(deltaBase64)
	if err != nil || len(data) > maxExecutionInput {
		return provider.TerminalOutput{}, errors.New("invalid or oversized base64 process output")
	}
	return a.appendAndPush(process.key, stream, data, capReached)
}

func (a *executionAPI) HandleProcessExit(handle string, exitCode int, stdout, stderr string, stdoutCap, stderrCap bool) error {
	process, err := a.ownedProcess(handle)
	if err != nil {
		return err
	}
	if len(stdout) > maxExecutionOutput || len(stderr) > maxExecutionOutput {
		return errors.New("buffered process output exceeds bound")
	}
	if stdout != "" {
		if _, err := a.appendAndPush(process.key, "stdout", []byte(stdout), stdoutCap); err != nil {
			return err
		}
	}
	if stderr != "" {
		if _, err := a.appendAndPush(process.key, "stderr", []byte(stderr), stderrCap); err != nil {
			return err
		}
	}
	a.processMu.Lock()
	delete(a.processes, handle)
	a.processMu.Unlock()
	return a.registry.Exit(process.key, exitCode)
}

func (a *executionAPI) CleanupProcesses(ctx context.Context) {
	a.processMu.Lock()
	processes := make([]ownedProcess, 0, len(a.processes))
	for _, process := range a.processes {
		processes = append(processes, process)
	}
	a.processes = make(map[string]ownedProcess)
	a.processMu.Unlock()
	for _, process := range processes {
		_, _ = a.send(ctx, "process/kill", map[string]any{"processHandle": process.key.ID})
	}
	a.registry.CleanupGeneration(a.generation)
}

// TerminalOutputSink receives one sequence-numbered chunk for the daemon
// session that owns the terminal. It is a live push channel: chunks are
// retained only in the bounded replay buffer and never enter session history.
type TerminalOutputSink func(sessionID string, out provider.TerminalOutput)

// SetTerminalOutputSink installs the daemon's live terminal push. Passing nil
// disables pushing; bounded replay through ReplayTerminal is unaffected.
func (p *Provider) SetTerminalOutputSink(sink TerminalOutputSink) {
	p.mu.Lock()
	p.terminalSink = sink
	p.mu.Unlock()
}

// pushTerminalOutput resolves the managed session that owns threadID and
// forwards one chunk. A terminal on a thread with no attached managed session
// (or a standalone process with no thread) is buffered only.
func (p *Provider) pushTerminalOutput(threadID string, chunk provider.TerminalOutput) {
	if threadID == "" {
		return
	}
	p.mu.Lock()
	sink := p.terminalSink
	localID := ""
	for _, s := range p.sessions {
		if s.agentID == threadID {
			localID = s.localID
			break
		}
	}
	p.mu.Unlock()
	if sink == nil || localID == "" {
		return
	}
	sink(localID, chunk)
}

func (p *Provider) executionFor(ctx context.Context) (*executionAPI, error) {
	if _, err := p.ensureEngine(ctx); err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.execution == nil || p.eng == nil || p.execution.generation != p.eng.generation {
		return nil, errors.New("Codex execution adapter is unavailable")
	}
	return p.execution, nil
}

// RunSandboxedExec executes one structured argv under Codex permissions.
func (p *Provider) RunSandboxedExec(ctx context.Context, request provider.ExecRequest) (provider.ExecResult, error) {
	api, err := p.executionFor(ctx)
	if err != nil {
		return provider.ExecResult{}, err
	}
	return api.ExecSandboxed(ctx, request)
}

// RunUnsandboxedThreadShell runs explicit shell text with full host access.
func (p *Provider) RunUnsandboxedThreadShell(ctx context.Context, threadID, command string) (provider.ExecutionResult, error) {
	api, err := p.executionFor(ctx)
	if err != nil {
		return provider.ExecutionResult{}, err
	}
	return api.RunThreadShell(ctx, threadID, command)
}

// ListExecutionEnvironments projects administrator-owned ids and roots only.
func (p *Provider) ListExecutionEnvironments(ctx context.Context) ([]provider.ExecutionEnvironment, error) {
	api, err := p.executionFor(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]provider.ExecutionEnvironment, 0, len(api.environments))
	for _, environment := range api.environments {
		environment.ExecServerURL = ""
		environment.ConnectTimeout = 0
		environment.RuntimeWorkspaceRoots = append([]string(nil), environment.RuntimeWorkspaceRoots...)
		out = append(out, environment)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// ReadExecutionEnvironmentStatus observes status without starting recovery.
func (p *Provider) ReadExecutionEnvironmentStatus(ctx context.Context, id string) (provider.EnvironmentStatus, error) {
	api, err := p.executionFor(ctx)
	if err != nil {
		return provider.EnvironmentStatus{}, err
	}
	return api.EnvironmentStatus(ctx, id)
}

// ReadExecutionEnvironmentInfo returns sanitized shell and cwd metadata.
func (p *Provider) ReadExecutionEnvironmentInfo(ctx context.Context, id string) (provider.EnvironmentInfo, error) {
	api, err := p.executionFor(ctx)
	if err != nil {
		return provider.EnvironmentInfo{}, err
	}
	return api.EnvironmentInfo(ctx, id)
}

// SpawnStandaloneProcess starts one confirmed default-off process.
func (p *Provider) SpawnStandaloneProcess(ctx context.Context, request provider.ProcessSpawnRequest) (provider.ProcessInfo, error) {
	api, err := p.executionFor(ctx)
	if err != nil {
		return provider.ProcessInfo{}, err
	}
	return api.SpawnProcess(ctx, request)
}

// WriteTerminal sends bounded stdin to an owned exec or process handle.
func (p *Provider) WriteTerminal(ctx context.Context, id string, data []byte, closeStdin bool) error {
	api, err := p.executionFor(ctx)
	if err != nil {
		return err
	}
	if _, processErr := api.ownedProcess(id); processErr == nil {
		return api.WriteProcess(ctx, id, data, closeStdin)
	}
	return api.WriteExec(ctx, id, data, closeStdin)
}

// ResizeTerminal resizes an owned PTY.
func (p *Provider) ResizeTerminal(ctx context.Context, id string, rows, cols int) error {
	api, err := p.executionFor(ctx)
	if err != nil {
		return err
	}
	if _, processErr := api.ownedProcess(id); processErr == nil {
		return api.ResizeProcess(ctx, id, rows, cols)
	}
	return api.ResizeExec(ctx, id, rows, cols)
}

// ReplayTerminal returns buffered chunks and whether the requested sequence
// fell behind the retained 1 MiB window.
func (p *Provider) ReplayTerminal(ctx context.Context, threadID, id string, after uint64) ([]provider.TerminalOutput, bool, error) {
	api, err := p.executionFor(ctx)
	if err != nil {
		return nil, false, err
	}
	return api.registry.Replay(terminalKey{Generation: api.generation, ThreadID: threadID, ID: id}, after)
}

// ListTerminals returns daemon-owned terminals and negotiated native entries.
func (p *Provider) ListTerminals(ctx context.Context, threadID string) ([]provider.TerminalInfo, error) {
	api, err := p.executionFor(ctx)
	if err != nil {
		return nil, err
	}
	out := api.registry.List(threadID)
	page, nativeErr := api.ListBackgroundTerminals(ctx, threadID, "", maxTerminalPage)
	if nativeErr == nil {
		out = append(out, page.Terminals...)
	} else if !errors.Is(nativeErr, provider.ErrNativeUnavailable) {
		return nil, nativeErr
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// StopTerminal terminates exactly one owned or negotiated native terminal.
func (p *Provider) StopTerminal(ctx context.Context, threadID, id string) error {
	api, err := p.executionFor(ctx)
	if err != nil {
		return err
	}
	for _, terminal := range api.registry.List(threadID) {
		if terminal.ID != id || !terminal.Running {
			continue
		}
		if terminal.Kind == provider.TerminalKindProcess {
			return api.KillProcess(ctx, id)
		}
		if terminal.Kind == provider.TerminalKindExec {
			return api.TerminateExec(ctx, id)
		}
	}
	terminated, err := api.TerminateBackgroundTerminal(ctx, threadID, id)
	if err != nil {
		if errors.Is(err, provider.ErrNativeUnavailable) {
			return provider.ErrTerminalNotFound
		}
		return err
	}
	if !terminated {
		return provider.ErrTerminalNotFound
	}
	return nil
}

// StopAllTerminals explicitly terminates all known terminals for one thread.
func (p *Provider) StopAllTerminals(ctx context.Context, threadID string) (int, error) {
	terminals, err := p.ListTerminals(ctx, threadID)
	if err != nil {
		return 0, err
	}
	stopped := 0
	for _, terminal := range terminals {
		if !terminal.Running {
			continue
		}
		if err := p.StopTerminal(ctx, threadID, terminal.ID); err != nil {
			return stopped, err
		}
		stopped++
	}
	return stopped, nil
}

var _ provider.ExecutionSession = (*session)(nil)
var _ provider.EnvironmentSession = (*session)(nil)

// RunSandboxedExec runs one structured argv scoped to this native thread.
func (s *session) RunSandboxedExec(ctx context.Context, request provider.ExecRequest) (provider.ExecResult, error) {
	if s.p == nil {
		return provider.ExecResult{}, errors.New("Codex provider unavailable")
	}
	request.ThreadID = s.agentID
	return s.p.RunSandboxedExec(ctx, request)
}

// RunUnsandboxedShell implements provider.ExecutionSession for the active
// native thread. Confirmation is enforced by the daemon/phone boundary.
func (s *session) RunUnsandboxedShell(ctx context.Context, command string) (provider.ExecutionResult, error) {
	if s.p == nil {
		return provider.ExecutionResult{}, errors.New("Codex provider unavailable")
	}
	return s.p.RunUnsandboxedThreadShell(ctx, s.agentID, command)
}

// SpawnStandaloneProcess starts a default-off unsandboxed process bound to
// this thread's terminal registry. Confirmation is enforced by the caller.
func (s *session) SpawnStandaloneProcess(ctx context.Context, request provider.ProcessSpawnRequest) (provider.ProcessInfo, error) {
	if s.p == nil {
		return provider.ProcessInfo{}, errors.New("Codex provider unavailable")
	}
	return s.p.SpawnStandaloneProcess(ctx, request)
}

// WriteTerminal sends bounded stdin to a terminal this thread owns.
func (s *session) WriteTerminal(ctx context.Context, id string, data []byte, closeStdin bool) error {
	if s.p == nil {
		return errors.New("Codex provider unavailable")
	}
	if err := s.requireOwnedTerminal(id); err != nil {
		return err
	}
	return s.p.WriteTerminal(ctx, id, data, closeStdin)
}

// ResizeTerminal resizes a PTY this thread owns.
func (s *session) ResizeTerminal(ctx context.Context, id string, rows, cols int) error {
	if s.p == nil {
		return errors.New("Codex provider unavailable")
	}
	if err := s.requireOwnedTerminal(id); err != nil {
		return err
	}
	return s.p.ResizeTerminal(ctx, id, rows, cols)
}

// ReplayTerminal returns retained chunks after the client's last sequence and
// reports whether the 1 MiB window already dropped the requested position.
func (s *session) ReplayTerminal(ctx context.Context, id string, after uint64) ([]provider.TerminalOutput, bool, error) {
	if s.p == nil {
		return nil, false, errors.New("Codex provider unavailable")
	}
	return s.p.ReplayTerminal(ctx, s.agentID, id, after)
}

// requireOwnedTerminal fails closed on a terminal registered to a different
// thread so one session can never steer another session's stdin.
func (s *session) requireOwnedTerminal(id string) error {
	for _, terminal := range s.p.terminals.List(s.agentID) {
		if terminal.ID == id {
			return nil
		}
	}
	return provider.ErrTerminalNotFound
}

// ListTerminals returns daemon-owned and negotiated native terminals without
// tying their lifetime to this managed session attachment.
func (s *session) ListTerminals(ctx context.Context) ([]provider.TerminalInfo, error) {
	if s.p == nil {
		return nil, errors.New("Codex provider unavailable")
	}
	return s.p.ListTerminals(ctx, s.agentID)
}

// StopTerminal terminates one exact terminal id.
func (s *session) StopTerminal(ctx context.Context, id string) error {
	if s.p == nil {
		return errors.New("Codex provider unavailable")
	}
	return s.p.StopTerminal(ctx, s.agentID, id)
}

// StopAllTerminals terminates every known terminal for this native thread.
func (s *session) StopAllTerminals(ctx context.Context) (int, error) {
	if s.p == nil {
		return 0, errors.New("Codex provider unavailable")
	}
	return s.p.StopAllTerminals(ctx, s.agentID)
}

// SetExecutionEnvironment validates readiness and allowed roots immediately;
// failure leaves the prior selection untouched.
func (s *session) SetExecutionEnvironment(ctx context.Context, selection *provider.EnvironmentSelection) error {
	if s.p == nil {
		return errors.New("Codex provider unavailable")
	}
	if selection == nil {
		s.mu.Lock()
		s.environmentSelection = nil
		s.environmentSelectionSet = true
		s.mu.Unlock()
		return nil
	}
	api, err := s.p.executionFor(ctx)
	if err != nil {
		return err
	}
	validated, err := api.ValidateEnvironmentSelection(selection.EnvironmentID, selection.CWD, selection.RuntimeWorkspaceRoots)
	if err != nil {
		return err
	}
	status, err := api.EnvironmentStatus(ctx, selection.EnvironmentID)
	if err != nil {
		return err
	}
	if status.Status != "ready" {
		return fmt.Errorf("environment %s is %s", selection.EnvironmentID, status.Status)
	}
	s.mu.Lock()
	copySelection := validated
	s.environmentSelection = &copySelection
	s.environmentSelectionSet = true
	s.mu.Unlock()
	return nil
}

func (p *Provider) handleExecutionNotification(method string, params json.RawMessage) {
	p.mu.Lock()
	api := p.execution
	sessions := make([]*session, 0, len(p.sessions))
	for _, session := range p.sessions {
		sessions = append(sessions, session)
	}
	p.mu.Unlock()
	if api == nil {
		return
	}
	switch method {
	case "command/exec/outputDelta":
		var body struct {
			ProcessID   string `json:"processId"`
			Stream      string `json:"stream"`
			DeltaBase64 string `json:"deltaBase64"`
			CapReached  bool   `json:"capReached"`
		}
		if json.Unmarshal(params, &body) != nil {
			return
		}
		data, err := base64.StdEncoding.DecodeString(body.DeltaBase64)
		if err != nil || len(data) > maxExecutionInput {
			return
		}
		key, ok := api.registry.KeyByID(api.generation, body.ProcessID)
		if !ok {
			return
		}
		_, _ = api.appendAndPush(key, body.Stream, data, body.CapReached)
	case "process/outputDelta":
		var body struct {
			ProcessHandle string `json:"processHandle"`
			Stream        string `json:"stream"`
			DeltaBase64   string `json:"deltaBase64"`
			CapReached    bool   `json:"capReached"`
		}
		if json.Unmarshal(params, &body) == nil {
			_, _ = api.HandleProcessOutput(body.ProcessHandle, body.Stream, body.DeltaBase64, body.CapReached)
		}
	case "process/exited":
		var body struct {
			ProcessHandle    string `json:"processHandle"`
			ExitCode         int    `json:"exitCode"`
			Stdout           string `json:"stdout"`
			Stderr           string `json:"stderr"`
			StdoutCapReached bool   `json:"stdoutCapReached"`
			StderrCapReached bool   `json:"stderrCapReached"`
		}
		if json.Unmarshal(params, &body) == nil {
			_ = api.HandleProcessExit(body.ProcessHandle, body.ExitCode, body.Stdout, body.Stderr, body.StdoutCapReached, body.StderrCapReached)
		}
	case "thread/environment/connected", "thread/environment/disconnected":
		var body struct {
			EnvironmentID string `json:"environmentId"`
		}
		if json.Unmarshal(params, &body) != nil || body.EnvironmentID == "" {
			return
		}
		state := "connected"
		if method == "thread/environment/disconnected" {
			state = "disconnected"
		}
		for _, session := range sessions {
			session.emit(event.Event{Type: event.TypeNotice, SessionID: session.localID, AgentSessionID: session.agentID, Timestamp: time.Now().UTC(), Text: fmt.Sprintf("Execution environment %s %s", boundedPermissionText(body.EnvironmentID, 128), state)})
		}
	}
}
