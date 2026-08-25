package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// MaxDoctorOutputBytes is the maximum combined output accepted from codex doctor.
const MaxDoctorOutputBytes = 262144

const (
	maxDoctorChecks  = 128
	maxDoctorDetails = 32
	maxDoctorList    = 32
	maxDoctorString  = 2048
)

type doctorRunFunc func(context.Context, string, ...string) ([]byte, error)

type doctorFlight struct {
	done   chan struct{}
	report DoctorReport
	err    error
}

// DoctorReport is the sanitized schema-version-1 diagnostic projection.
type DoctorReport struct {
	SchemaVersion int           `json:"schemaVersion"`
	GeneratedAt   string        `json:"generatedAt,omitempty"`
	OverallStatus string        `json:"overallStatus"`
	CodexVersion  string        `json:"codexVersion,omitempty"`
	Checks        []DoctorCheck `json:"checks"`
}

// JSON returns the report's deterministic JSON representation.
func (r DoctorReport) JSON() []byte { b, _ := json.Marshal(r); return b }

// DoctorCheck is one bounded, sanitized diagnostic check.
type DoctorCheck struct {
	ID         string                  `json:"id"`
	Category   string                  `json:"category,omitempty"`
	Status     string                  `json:"status"`
	Summary    string                  `json:"summary"`
	Details    map[string]DoctorDetail `json:"details,omitempty"`
	Issues     []DoctorIssue           `json:"issues,omitempty"`
	Notes      []string                `json:"notes,omitempty"`
	DurationMS int64                   `json:"durationMs,omitempty"`
}

// DoctorDetail is a scalar or bounded list value from a diagnostic check.
type DoctorDetail struct {
	Text   string   `json:"text,omitempty"`
	Values []string `json:"values,omitempty"`
}

// UnmarshalJSON accepts the two detail shapes defined by doctor schema 1.
func (d *DoctorDetail) UnmarshalJSON(raw []byte) error {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		d.Text = text
		return nil
	}
	var values []string
	if json.Unmarshal(raw, &values) == nil {
		d.Values = values
		return nil
	}
	return errors.New("doctor detail must be string or string array")
}

// DoctorIssue is a sanitized diagnostic issue with remediation removed.
type DoctorIssue struct {
	Severity string   `json:"severity"`
	Cause    string   `json:"cause"`
	Measured *string  `json:"measured,omitempty"`
	Expected *string  `json:"expected,omitempty"`
	Remedy   *string  `json:"remedy,omitempty"`
	Fields   []string `json:"fields,omitempty"`
}

type rawDoctorReport struct {
	SchemaVersion int                       `json:"schemaVersion"`
	GeneratedAt   string                    `json:"generatedAt"`
	OverallStatus string                    `json:"overallStatus"`
	CodexVersion  string                    `json:"codexVersion"`
	Checks        map[string]rawDoctorCheck `json:"checks"`
}

type rawDoctorCheck struct {
	ID          string                  `json:"id"`
	Category    string                  `json:"category"`
	Status      string                  `json:"status"`
	Summary     string                  `json:"summary"`
	Details     map[string]DoctorDetail `json:"details"`
	Issues      []DoctorIssue           `json:"issues"`
	Notes       []string                `json:"notes"`
	Remediation json.RawMessage         `json:"remediation"`
	DurationMS  int64                   `json:"durationMs"`
}

var doctorSensitive = regexp.MustCompile(`(?i)(bearer|token|secret|password|authorization|api[_-]?key|(?:^|\s|[=:])(?:/|~[/\\]|[a-z]:[/\\])|https?://|wss?://|sk-[a-z0-9])`)

var knownDoctorChecks = map[string]bool{
	"app_server.status": true, "auth.credentials": true, "config.load": true,
	"git.environment": true, "installation": true, "mcp.config": true,
	"network.env": true, "network.provider_reachability": true,
	"network.websocket_reachability": true, "runtime.provenance": true,
	"runtime.search": true, "sandbox.helpers": true, "security.endpoint": true,
	"state.paths": true, "state.rollout_db_parity": true, "system.disk": true,
	"system.environment": true, "terminal.env": true, "terminal.title": true,
	"updates.status": true,
}

// ProjectDoctorReport validates and sanitizes a schema-version-1 doctor report.
func ProjectDoctorReport(raw []byte) (DoctorReport, error) {
	if len(raw) == 0 || len(raw) > MaxDoctorOutputBytes {
		return DoctorReport{}, errors.New("doctor output exceeds bound")
	}
	var in rawDoctorReport
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return DoctorReport{}, fmt.Errorf("decode doctor report: %w", err)
	}
	if in.SchemaVersion != 1 {
		return DoctorReport{}, fmt.Errorf("unsupported doctor schema %d", in.SchemaVersion)
	}
	if len(in.Checks) > maxDoctorChecks {
		return DoctorReport{}, errors.New("too many doctor checks")
	}
	out := DoctorReport{SchemaVersion: 1, GeneratedAt: sanitizeDoctor(in.GeneratedAt), OverallStatus: sanitizeDoctor(in.OverallStatus), CodexVersion: sanitizeDoctor(in.CodexVersion)}
	keys := make([]string, 0, len(in.Checks))
	for key := range in.Checks {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		check := in.Checks[key]
		if key != check.ID {
			return DoctorReport{}, fmt.Errorf("doctor check key/id mismatch")
		}
		projected := DoctorCheck{ID: sanitizeDoctor(check.ID), Status: sanitizeDoctor(check.Status), Summary: sanitizeDoctor(check.Summary)}
		if knownDoctorChecks[key] {
			projected.Category = sanitizeDoctor(check.Category)
			projected.DurationMS = check.DurationMS
			projected.Details = sanitizeDetails(check.Details)
			projected.Issues = sanitizeIssues(check.Issues)
			projected.Notes = sanitizeStrings(check.Notes)
		}
		out.Checks = append(out.Checks, projected)
	}
	if len(out.JSON()) > MaxDoctorOutputBytes {
		return DoctorReport{}, errors.New("doctor projection exceeds bound")
	}
	return out, nil
}

func sanitizeDetails(in map[string]DoctorDetail) map[string]DoctorDetail {
	if len(in) == 0 {
		return nil
	}
	keys := make([]string, 0, len(in))
	for key := range in {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) > maxDoctorDetails {
		keys = keys[:maxDoctorDetails]
	}
	out := make(map[string]DoctorDetail, len(keys))
	for _, key := range keys {
		value := in[key]
		out[sanitizeDoctor(key)] = DoctorDetail{Text: sanitizeDoctor(value.Text), Values: sanitizeStrings(value.Values)}
	}
	return out
}

func sanitizeIssues(in []DoctorIssue) []DoctorIssue {
	if len(in) > maxDoctorList {
		in = in[:maxDoctorList]
	}
	out := make([]DoctorIssue, 0, len(in))
	for _, issue := range in {
		issue.Severity = sanitizeDoctor(issue.Severity)
		issue.Cause = sanitizeDoctor(issue.Cause)
		issue.Measured = sanitizeDoctorPtr(issue.Measured)
		issue.Expected = sanitizeDoctorPtr(issue.Expected)
		issue.Remedy = sanitizeDoctorPtr(issue.Remedy)
		issue.Fields = sanitizeStrings(issue.Fields)
		out = append(out, issue)
	}
	return out
}

func sanitizeDoctorPtr(in *string) *string {
	if in == nil {
		return nil
	}
	out := sanitizeDoctor(*in)
	return &out
}
func sanitizeStrings(in []string) []string {
	if len(in) > maxDoctorList {
		in = in[:maxDoctorList]
	}
	out := make([]string, len(in))
	for i := range in {
		out[i] = sanitizeDoctor(in[i])
	}
	return out
}
func sanitizeDoctor(in string) string {
	in = strings.TrimSpace(in)
	if doctorSensitive.MatchString(in) {
		return "[redacted]"
	}
	if len(in) > maxDoctorString {
		return in[:maxDoctorString]
	}
	return in
}

// RunDoctor executes one exact, single-flight diagnostic probe. A nonzero
// process exit is accepted when stdout is still a valid schema-v1 report.
func (p *Provider) RunDoctor(ctx context.Context) (DoctorReport, error) {
	p.doctorMu.Lock()
	if flight := p.doctor; flight != nil {
		p.doctorMu.Unlock()
		select {
		case <-flight.done:
			return flight.report, flight.err
		case <-ctx.Done():
			return DoctorReport{}, ctx.Err()
		}
	}
	flight := &doctorFlight{done: make(chan struct{})}
	p.doctor = flight
	p.doctorMu.Unlock()

	runCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	runner := p.doctorRun
	if runner == nil {
		runner = runDoctorCommand
	}
	raw, runErr := runner(runCtx, p.cfg.Bin, "doctor", "--json")
	report, projectErr := ProjectDoctorReport(raw)
	if projectErr != nil {
		if runErr != nil {
			flight.err = fmt.Errorf("codex doctor: %w", runErr)
		} else {
			flight.err = projectErr
		}
	} else {
		flight.report = report
	}
	close(flight.done)
	p.doctorMu.Lock()
	if p.doctor == flight {
		p.doctor = nil
	}
	p.doctorMu.Unlock()
	return flight.report, flight.err
}

type boundedDoctorOutput struct {
	mu       sync.Mutex
	stdout   bytes.Buffer
	total    int
	overflow bool
}

type boundedDoctorStream struct {
	output *boundedDoctorOutput
	stdout bool
}

func (w boundedDoctorStream) Write(p []byte) (int, error) {
	w.output.mu.Lock()
	defer w.output.mu.Unlock()
	n := len(p)
	w.output.total += n
	if w.output.total > MaxDoctorOutputBytes {
		w.output.overflow = true
	}
	if w.stdout {
		remaining := MaxDoctorOutputBytes + 1 - w.output.stdout.Len()
		if remaining > len(p) {
			remaining = len(p)
		}
		if remaining > 0 {
			_, _ = w.output.stdout.Write(p[:remaining])
		}
	}
	return n, nil
}

func runDoctorCommand(ctx context.Context, bin string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	output := &boundedDoctorOutput{}
	cmd.Stdout = boundedDoctorStream{output: output, stdout: true}
	cmd.Stderr = boundedDoctorStream{output: output}
	err := cmd.Run()
	if output.overflow {
		return nil, errors.New("doctor output exceeds bound")
	}
	return output.stdout.Bytes(), err
}
