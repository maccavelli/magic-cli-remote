package codex

import (
	"os"
	"path/filepath"
	"testing"
)

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "doctor", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestParseDoctorAuthFixtures pins the parse of every recorded shape.
//
// The fixtures are real `codex doctor --json` output from codex-cli 0.152.1,
// schemaVersion 1, reduced to the one check this code reads (MADR 0136).
func TestParseDoctorAuthFixtures(t *testing.T) {
	cases := []struct {
		fixture string
		mode    string
		usable  bool
		envVars string
	}{
		{"file-protected.json", "file", true, ""},
		{"no-credentials.json", "file", false, ""},
		{"incomplete-file.json", "file", false, ""},
		{"env-provided.json", "file", false, "OPENAI_API_KEY"},
		{"keyring-backend.json", "keyring", false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			got, err := parseDoctorAuth(loadFixture(t, tc.fixture))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if got.StorageMode != tc.mode {
				t.Errorf("storage mode = %q, want %q", got.StorageMode, tc.mode)
			}
			if got.Usable != tc.usable {
				t.Errorf("usable = %v, want %v (status %q)", got.Usable, tc.usable, got.Status)
			}
			if got.EnvVarsPresent != tc.envVars {
				t.Errorf("env vars = %q, want %q", got.EnvVarsPresent, tc.envVars)
			}
		})
	}
}

// TestParseDoctorAuthFailsClosed covers every way the report can be
// uninterpretable. All of them must produce the one sentinel, because all of
// them lead to the same conservative classification.
func TestParseDoctorAuthFailsClosed(t *testing.T) {
	good := string(loadFixture(t, "file-protected.json"))

	cases := map[string]string{
		"malformed json":       `{"schemaVersion":1,`,
		"empty":                ``,
		"future schema":        `{"schemaVersion":2,"checks":{"auth.credentials":{"status":"ok","details":{"auth storage mode":"File"}}}}`,
		"zero schema":          `{"checks":{"auth.credentials":{"status":"ok","details":{"auth storage mode":"File"}}}}`,
		"missing check":        `{"schemaVersion":1,"checks":{"network.websocket":{"status":"ok"}}}`,
		"no checks at all":     `{"schemaVersion":1}`,
		"missing storage mode": `{"schemaVersion":1,"checks":{"auth.credentials":{"status":"ok","details":{"stored API key":"true"}}}}`,
		"empty storage mode":   `{"schemaVersion":1,"checks":{"auth.credentials":{"status":"ok","details":{"auth storage mode":"  "}}}}`,
		"details wrong shape":  `{"schemaVersion":1,"checks":{"auth.credentials":{"status":"ok","details":[]}}}`,
		"not an object at all": `[]`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseDoctorAuth([]byte(body)); err == nil {
				t.Fatal("want errDoctorUnusable, got a usable parse")
			}
		})
	}

	// Control: the same parser accepts the real thing, so the cases above are
	// failing for their own reason and not because the parser rejects
	// everything.
	if _, err := parseDoctorAuth([]byte(good)); err != nil {
		t.Fatalf("control fixture must parse: %v", err)
	}
}

// TestStoredAuthIssueArrayDoesNotBreakParsing pins the one details value that
// is an array rather than a string. A detail this code does not read must not
// be able to fail the classification.
func TestStoredAuthIssueArrayDoesNotBreakParsing(t *testing.T) {
	got, err := parseDoctorAuth(loadFixture(t, "incomplete-file.json"))
	if err != nil {
		t.Fatalf("a report carrying the array-valued `stored auth issue` must parse: %v", err)
	}
	if got.StorageMode != "file" {
		t.Fatalf("storage mode = %q", got.StorageMode)
	}
}
