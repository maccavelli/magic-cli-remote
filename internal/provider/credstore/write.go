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

// SetGrokModelAPIKey writes a per-model api_key into ~/.grok/config.toml.
//
// Grok's documented precedence is per-model api_key, then the OAuth session,
// then XAI_API_KEY. The env route would need a daemon restart the daemon
// cannot perform on itself, so this is the write path mcremote uses.
func SetGrokModelAPIKey(path, key string) error {
	if err := ValidateSecret(key); err != nil {
		return err
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

	quoted := `api_key = "` + escapeTOML(key) + `"`
	inAuth := false
	replaced := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			inAuth = trimmed == "[auth]"
			continue
		}
		if inAuth && strings.HasPrefix(trimmed, "api_key") {
			lines[i] = quoted
			replaced = true
			break
		}
	}
	if !replaced {
		if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
			lines = lines[:len(lines)-1]
		}
		lines = append(lines, "", "[auth]", quoted, "")
	}
	return writeFileAtomic(path, []byte(strings.Join(lines, "\n")), 0o600)
}

// ClearGrokModelAPIKey removes the api_key line from the [auth] table.
func ClearGrokModelAPIKey(path string) error {
	b, err := os.ReadFile(path) //nolint:gosec // fixed store location
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read grok config: %w", err)
	}
	lines := strings.Split(string(b), "\n")
	out := make([]string, 0, len(lines))
	inAuth := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			inAuth = trimmed == "[auth]"
			out = append(out, line)
			continue
		}
		if inAuth && strings.HasPrefix(trimmed, "api_key") {
			continue
		}
		out = append(out, line)
	}
	return writeFileAtomic(path, []byte(strings.Join(out, "\n")), 0o600)
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
