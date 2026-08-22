package credstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	yaml "go.yaml.in/yaml/v3"
)

// MaxSecretBytes bounds an injected credential. Real keys are well under a
// kilobyte; the cap exists so a malformed or hostile client cannot make the
// daemon write an arbitrarily large file into the user's home directory.
const MaxSecretBytes = 8 << 10

// ErrEmptySecret is returned for a blank credential — always a client bug, and
// writing it would silently break the agent it was meant to configure.
var ErrEmptySecret = errors.New("credential is empty")

// ErrSecretTooLarge is returned for a credential above MaxSecretBytes.
var ErrSecretTooLarge = fmt.Errorf("credential exceeds %d bytes", MaxSecretBytes)

// ValidateSecret checks a credential before any write touches the filesystem.
func ValidateSecret(secret string) error {
	if strings.TrimSpace(secret) == "" {
		return ErrEmptySecret
	}
	if len(secret) > MaxSecretBytes {
		return ErrSecretTooLarge
	}
	return nil
}

// writeFileAtomic writes data to path via a temporary file in the same
// directory, fsynced and chmodded before the rename.
//
// Same-directory matters: rename is only atomic within a filesystem, and a
// temp file in /tmp can land on a different one. Mode is set before the rename
// so the file is never briefly world-readable — it holds a credential.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".mcremote-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer func() {
		// Best-effort cleanup; a successful rename makes this a no-op.
		_ = os.Remove(tmpName)
	}()
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod temp: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename into place: %w", err)
	}
	return nil
}

// MergeJSONAuth sets one provider entry in an OpenCode/Kilo-style auth.json,
// preserving every other entry and every field of the entry it replaces that
// it does not own.
//
// Read-modify-write is not atomic against another writer (the agent's own TUI,
// or a second daemon). MADR 0074 D10 accepts that: last writer wins, no
// locking. What this must never do is lose *other* providers' credentials,
// which is why it merges rather than overwrites the document.
func MergeJSONAuth(path, providerID, credType, key string) error {
	return MergeJSONAuthMetadata(path, providerID, credType, key, nil)
}

// MergeJSONAuthMetadata is MergeJSONAuth carrying the typed prompt answers a
// method declared (MADR 0083 D2) — the engine's ApiAuth `metadata` field.
// Empty metadata leaves any existing metadata untouched.
func MergeJSONAuthMetadata(path, providerID, credType, key string, metadata map[string]string) error {
	if err := ValidateSecret(key); err != nil {
		return err
	}
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		return errors.New("provider id is required")
	}
	doc := map[string]json.RawMessage{}
	b, err := os.ReadFile(path) //nolint:gosec // fixed store location
	switch {
	case err == nil:
		if len(b) > 0 {
			if err := json.Unmarshal(b, &doc); err != nil {
				// Refuse rather than clobber: an unparseable store is more
				// likely a format change than a corrupt file, and overwriting
				// it would destroy every other credential in it.
				return fmt.Errorf("refusing to rewrite unparseable %s: %w", filepath.Base(path), err)
			}
		}
	case errors.Is(err, fs.ErrNotExist):
		// First credential on this host.
	default:
		return fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}

	entry := map[string]any{}
	if existing, ok := doc[providerID]; ok {
		// Keep fields we do not manage (refresh tokens, expiry, …).
		_ = json.Unmarshal(existing, &entry)
	}
	entry["type"] = credType
	entry["key"] = key
	if len(metadata) > 0 {
		entry["metadata"] = metadata
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	doc[providerID] = raw

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, out, 0o600)
}

// DeleteJSONAuth removes one provider entry. A missing file or missing entry
// is success: the caller asked for the credential to be gone.
func DeleteJSONAuth(path, providerID string) error {
	b, err := os.ReadFile(path) //nolint:gosec // fixed store location
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	doc := map[string]json.RawMessage{}
	if len(b) > 0 {
		if err := json.Unmarshal(b, &doc); err != nil {
			return fmt.Errorf("refusing to rewrite unparseable %s: %w", filepath.Base(path), err)
		}
	}
	if _, ok := doc[providerID]; !ok {
		return nil
	}
	delete(doc, providerID)
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, out, 0o600)
}

// SetGooseActiveProvider rewrites only the `active_provider` scalar, leaving
// every other line of config.yaml byte-identical.
//
// A YAML round-trip would reformat the file, drop comments, and reorder keys —
// unacceptable for a file the user also edits by hand, and goose has no
// non-interactive command that would do it for us (`goose configure` takes no
// flags at all). Line surgery is the conservative option.
func SetGooseActiveProvider(path, providerID string) error {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		return errors.New("provider id is required")
	}
	b, err := os.ReadFile(path) //nolint:gosec // fixed store location
	if errors.Is(err, fs.ErrNotExist) {
		return writeFileAtomic(path, []byte("active_provider: "+providerID+"\n"), 0o600)
	}
	if err != nil {
		return fmt.Errorf("read goose config: %w", err)
	}
	lines := strings.Split(string(b), "\n")
	replaced := false
	for i, line := range lines {
		if line != strings.TrimLeft(line, " \t") {
			continue // indented: not the top-level key
		}
		k, _, ok := splitYAMLScalar(strings.TrimSpace(line))
		if !ok || k != "active_provider" {
			continue
		}
		lines[i] = "active_provider: " + providerID
		replaced = true
		break
	}
	if !replaced {
		// Prepend so it cannot land inside another block's indented body.
		lines = append([]string{"active_provider: " + providerID}, lines...)
	}
	return writeFileAtomic(path, []byte(strings.Join(lines, "\n")), 0o600)
}

// ErrGooseKeyringManaged is returned when goose is configured to keep its
// secrets in the OS keyring (MADR 0074 D18).
//
// mcremote does not write it, and the reason is a security one rather than an
// engineering shortfall: the portable ways to drive a keychain from a daemon
// either put the secret in argv, where every process on the host can read it
// out of `ps`, or need an interactive unlock that no headless flow can answer.
// D2 forbids the first and headless-first forbids the second, so the daemon
// says so plainly instead of writing a file goose will not read.
var ErrGooseKeyringManaged = errors.New(
	"goose keeps secrets in the OS keyring; set GOOSE_DISABLE_KEYRING on the host or run `goose configure` there")

// ReadGooseSecretNames lists the secret names in goose's file store. Names
// only: the values never leave this function (D2).
func ReadGooseSecretNames(path string) ([]string, error) {
	values, err := readGooseSecrets(path)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(values))
	for k := range values {
		out = append(out, k)
	}
	sort.Strings(out)
	return out, nil
}

// readGooseSecrets parses secrets.yaml into its flat key→value map. A missing
// file is an empty map: that is a cold host, not an error.
func readGooseSecrets(path string) (map[string]any, error) {
	b, err := os.ReadFile(path) //nolint:gosec // fixed store location
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read goose secrets: %w", err)
	}
	values := map[string]any{}
	if len(b) > 0 {
		if err := yaml.Unmarshal(b, &values); err != nil {
			// Same rule as auth.json: refuse rather than clobber a file whose
			// shape we do not recognise, because it holds every other secret.
			return nil, fmt.Errorf("refusing to rewrite unparseable %s: %w", filepath.Base(path), err)
		}
	}
	return values, nil
}

// SetGooseSecret merges one secret into goose's file store (MADR 0074 D18).
//
// The file is goose's own format — a flat YAML map written by serde_yaml — and
// goose reads it whenever its keyring is disabled or unavailable. Callers must
// check GooseKeyringDisabled first: writing this file on a keyring-backed host
// would look like success and change nothing.
func SetGooseSecret(path, key, value string) error {
	if err := ValidateSecret(value); err != nil {
		return err
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("goose secret name is required")
	}
	values, err := readGooseSecrets(path)
	if err != nil {
		return err
	}
	values[key] = value
	return writeGooseSecrets(path, values)
}

// DeleteGooseSecret removes one secret. A missing file or key is success.
func DeleteGooseSecret(path, key string) error {
	values, err := readGooseSecrets(path)
	if err != nil {
		return err
	}
	if _, ok := values[key]; !ok {
		return nil
	}
	delete(values, key)
	return writeGooseSecrets(path, values)
}

func writeGooseSecrets(path string, values map[string]any) error {
	out, err := yaml.Marshal(values)
	if err != nil {
		return fmt.Errorf("encode goose secrets: %w", err)
	}
	return writeFileAtomic(path, out, 0o600)
}

// grokModelTableHeader is the quoted TOML table grok 1.0.3 actually
// reads. Unquoted [model.grok-4.5] is parsed as model.grok-4.5
// (MADR 0085 D4 / G6).
func grokModelTableHeader(modelID string) string {
	return `[model."` + escapeTOML(modelID) + `"]`
}

func isGrokAPIKeyLine(trimmed string) bool {
	if !strings.HasPrefix(trimmed, "api_key") {
		return false
	}
	if len(trimmed) == len("api_key") {
		return true
	}
	switch trimmed[len("api_key")] {
	case ' ', '\t', '=':
		return true
	default:
		return false
	}
}

func isQuotedGrokModelTable(trimmed string) bool {
	return strings.HasPrefix(trimmed, `[model."`) && strings.HasSuffix(trimmed, `"]`)
}

// SetGrokModelAPIKey writes api_key under exactly one quoted table
// [model."<modelID>"] (MADR 0085 D4). It also deletes a leftover
// [auth] api_key written by the previous implementation.
func SetGrokModelAPIKey(path, modelID, key string) error {
	if err := ValidateSecret(key); err != nil {
		return err
	}
	if strings.TrimSpace(modelID) == "" {
		return fmt.Errorf("grok model id is empty")
	}
	var lines []string
	b, err := os.ReadFile(path) //nolint:gosec // fixed store location
	switch {
	case err == nil:
		lines = strings.Split(string(b), "\n")
	case errors.Is(err, fs.ErrNotExist):
	default:
		return fmt.Errorf("read grok config: %w", err)
	}

	header := grokModelTableHeader(modelID)
	quoted := `api_key = "` + escapeTOML(key) + `"`
	inTarget := false
	inAuth := false
	replaced := false
	out := make([]string, 0, len(lines)+4)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			inTarget = trimmed == header
			inAuth = trimmed == "[auth]"
			out = append(out, line)
			continue
		}
		if inAuth && isGrokAPIKeyLine(trimmed) {
			continue
		}
		if inTarget && isGrokAPIKeyLine(trimmed) {
			if !replaced {
				out = append(out, quoted)
				replaced = true
			}
			continue
		}
		out = append(out, line)
	}
	if !replaced {
		if len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
			out = out[:len(out)-1]
		}
		out = append(out, "", header, quoted, "")
	}
	return writeFileAtomic(path, []byte(strings.Join(out, "\n")), 0o600)
}

// ClearGrokModelAPIKey removes api_key from [model."<modelID>"] (when
// modelID is non-empty) and from leftover [auth]. Other model tables
// are left alone (MADR 0085 D4).
func ClearGrokModelAPIKey(path, modelID string) error {
	b, err := os.ReadFile(path) //nolint:gosec // fixed store location
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read grok config: %w", err)
	}
	header := ""
	if strings.TrimSpace(modelID) != "" {
		header = grokModelTableHeader(modelID)
	}
	lines := strings.Split(string(b), "\n")
	out := make([]string, 0, len(lines))
	inTarget := false
	inAuth := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			inTarget = header != "" && trimmed == header
			inAuth = trimmed == "[auth]"
			out = append(out, line)
			continue
		}
		if (inTarget || inAuth) && isGrokAPIKeyLine(trimmed) {
			continue
		}
		out = append(out, line)
	}
	return writeFileAtomic(path, []byte(strings.Join(out, "\n")), 0o600)
}

// HasGrokConfigAPIKey reports whether any quoted [model."…"] table or
// leftover [auth] table contains an api_key line. It does not return
// or log the value (MADR 0074 D2 / 0085 D5). Unquoted [model.grok-4.5]
// is not presence — grok 1.0.3 does not honour it.
func HasGrokConfigAPIKey(path string) bool {
	b, err := os.ReadFile(path) //nolint:gosec // fixed store location
	if err != nil {
		return false
	}
	inQuotedModel := false
	inAuth := false
	for _, line := range strings.Split(string(b), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			inQuotedModel = isQuotedGrokModelTable(trimmed)
			inAuth = trimmed == "[auth]"
			continue
		}
		if (inQuotedModel || inAuth) && isGrokAPIKeyLine(trimmed) {
			return true
		}
	}
	return false
}

// escapeTOML escapes a basic-string value. Keys are opaque vendor strings, so
// a stray quote or backslash must not produce a config the agent cannot parse.
func escapeTOML(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\t", `\t`, "\r", `\r`)
	return r.Replace(s)
}

// ZeroString overwrites the backing array of a secret held in a []byte. Go
// strings are immutable, so callers that want this must hold the secret as a
// byte slice; this exists so the intent is expressed in one place rather than
// reinvented per call site.
func ZeroString(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// GooseKeyringMarker tags the GOOSE_DISABLE_KEYRING line as mcremote's, so
// mcremote can remove its own line without ever deleting one the operator set
// by hand (MADR 0110 D10).
//
// It is invisible to both readers: YAML treats it as a comment, and
// splitYAMLScalar already strips a trailing " #" from a scalar value.
const GooseKeyringMarker = "# managed by mcremote (providers.goose.keyring_disabled)"

// ErrGooseKeyringOperatorOwned means the config carries a GOOSE_DISABLE_KEYRING
// line mcremote did not write. Removing it would delete a setting someone chose
// deliberately, so the file is left exactly as it was.
var ErrGooseKeyringOperatorOwned = errors.New(
	"goose config has an operator-set GOOSE_DISABLE_KEYRING; leaving it alone")

const gooseKeyringKey = "GOOSE_DISABLE_KEYRING"

// SetGooseKeyringDisabled reconciles the GOOSE_DISABLE_KEYRING key in goose's
// config.yaml (MADR 0110 D1/D9/D10).
//
// disabled==true writes `GOOSE_DISABLE_KEYRING: true` with the ownership
// marker. disabled==false removes that line entirely rather than writing
// `false`: both read identically to goose, and removal leaves the operator's
// file as it was.
//
// It reports whether it changed anything, so a daemon restart against an
// already-correct file touches nothing. Like SetGooseActiveProvider above,
// this is line surgery rather than a YAML round-trip, because the operator
// edits this file by hand and a round-trip would reformat it and drop their
// comments.
func SetGooseKeyringDisabled(path string, disabled bool) (wroteChange bool, err error) {
	b, readErr := os.ReadFile(path) //nolint:gosec // fixed store location
	if errors.Is(readErr, fs.ErrNotExist) {
		if !disabled {
			return false, nil // nothing to remove
		}
		return true, writeFileAtomic(path, []byte(gooseKeyringLine()+"\n"), 0o600)
	}
	if readErr != nil {
		return false, fmt.Errorf("read goose config: %w", readErr)
	}

	lines := strings.Split(string(b), "\n")
	idx := -1
	for i, line := range lines {
		if line != strings.TrimLeft(line, " \t") {
			continue // indented: not the top-level key
		}
		k, _, ok := splitYAMLScalar(strings.TrimSpace(line))
		if ok && k == gooseKeyringKey {
			idx = i
			break
		}
	}

	if !disabled {
		if idx < 0 {
			return false, nil
		}
		if !strings.Contains(lines[idx], GooseKeyringMarker) {
			return false, ErrGooseKeyringOperatorOwned
		}
		lines = append(lines[:idx], lines[idx+1:]...)
		return true, writeFileAtomic(path, []byte(strings.Join(lines, "\n")), 0o600)
	}

	want := gooseKeyringLine()
	if idx >= 0 {
		if lines[idx] == want {
			return false, nil
		}
		lines[idx] = want
	} else {
		// Prepend so it cannot land inside another block's indented body.
		lines = append([]string{want}, lines...)
	}
	return true, writeFileAtomic(path, []byte(strings.Join(lines, "\n")), 0o600)
}

func gooseKeyringLine() string {
	return gooseKeyringKey + ": true  " + GooseKeyringMarker
}
