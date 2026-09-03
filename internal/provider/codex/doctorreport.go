package codex

import (
	"encoding/json"
	"errors"
	"strings"
)

// doctorSchemaVersion is the only `codex doctor --json` schema this code reads.
//
// A different version is not interpreted. The report is another tool's
// contract, and a field that moved or changed meaning is exactly the input that
// would make a confident wrong answer (MADR 0136).
const doctorSchemaVersion = 1

// authCredentialsCheckID is the check this code consumes. Nothing else in the
// report is read, and nothing else may become a dependency.
const authCredentialsCheckID = "auth.credentials"

// errDoctorUnusable means the report could not be understood: unreadable,
// malformed, an unrecognised schema, or missing the check or the fields this
// code needs. It is one sentinel on purpose — every one of those conditions
// leads to the same conservative answer, and distinguishing them would invite
// a caller to treat some as informative.
var errDoctorUnusable = errors.New("codex: doctor report cannot be interpreted")

// doctorReport is the subset of `codex doctor --json` this code reads.
type doctorReport struct {
	SchemaVersion int                    `json:"schemaVersion"`
	CodexVersion  string                 `json:"codexVersion"`
	Checks        map[string]doctorCheck `json:"checks"`
}

// doctorCheck is one check entry. `details` values are JSON strings even when
// they carry booleans ("true"/"false"), and `stored auth issue` is an array, so
// the map is decoded as raw messages and read through typed accessors rather
// than assumed to be map[string]string.
type doctorCheck struct {
	ID      string                     `json:"id"`
	Status  string                     `json:"status"`
	Summary string                     `json:"summary"`
	Details map[string]json.RawMessage `json:"details"`
}

// authCredentials is the interpreted view the classifier uses. It holds facts,
// not conclusions: the mapping from these to a StoreReality lives in
// ObserveCredentialStore, where the decision is reviewable in one place.
type authCredentials struct {
	// StorageMode is the RESOLVED backend Codex reports — "file", "keyring",
	// lowercased. This is why the report is worth reading: it reflects a key
	// set under a profile or via -c, and what "auto" actually resolved to,
	// none of which DetectCredentialStore can see (MADR 0136).
	StorageMode string
	// Usable reports whether Codex considers the stored credential good.
	// It is exactly status == "ok": on 0.152.1 a healthy host reports
	// `ok` / "auth is configured", and every unusable shape observed —
	// missing, incomplete, or environment-provided over an incomplete file —
	// reports `fail` or `warning`. Anything unrecognised is not usable.
	Usable bool
	// EnvVarsPresent is recorded for diagnostics and never classified.
	// Environment auth is per-process: the operator's shell may hold
	// OPENAI_API_KEY while the daemon's LaunchAgent does not, so it cannot
	// justify a durable, host-wide state (MADR 0136).
	EnvVarsPresent string
	// Status and Summary are carried for logging, never for branching beyond
	// Usable above.
	Status  string
	Summary string
}

// parseDoctorAuth reads the auth.credentials check out of a doctor report.
func parseDoctorAuth(raw []byte) (authCredentials, error) {
	var rep doctorReport
	if err := json.Unmarshal(raw, &rep); err != nil {
		return authCredentials{}, errDoctorUnusable
	}
	if rep.SchemaVersion != doctorSchemaVersion {
		return authCredentials{}, errDoctorUnusable
	}
	check, ok := rep.Checks[authCredentialsCheckID]
	if !ok {
		return authCredentials{}, errDoctorUnusable
	}
	mode := strings.ToLower(strings.TrimSpace(detailString(check.Details, "auth storage mode")))
	if mode == "" {
		// Without the backend there is nothing to classify from.
		return authCredentials{}, errDoctorUnusable
	}
	return authCredentials{
		StorageMode:    mode,
		Usable:         strings.EqualFold(strings.TrimSpace(check.Status), "ok"),
		EnvVarsPresent: detailString(check.Details, "auth env vars present"),
		Status:         check.Status,
		Summary:        check.Summary,
	}, nil
}

// detailString reads one details value as a string, tolerating the array form
// Codex uses for `stored auth issue`. A value of any other shape reads as
// empty rather than as an error: a detail this code does not need must not be
// able to fail the whole classification.
func detailString(details map[string]json.RawMessage, key string) string {
	raw, ok := details[key]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err == nil {
		return strings.Join(list, "; ")
	}
	return ""
}
