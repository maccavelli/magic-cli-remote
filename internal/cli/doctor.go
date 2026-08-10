package cli

import (
	"fmt"
	"io"
	"runtime"
	"strings"

	"github.com/maccavelli/magic-cli-remote/internal/cli/service"
	"github.com/maccavelli/magic-cli-remote/internal/provider/credstore"
	"github.com/maccavelli/magic-cli-remote/internal/tcc"
	"github.com/spf13/cobra"
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose host-level problems (service + macOS privacy)",
		Long: "Checks the host for conditions that break phone reconnect or " +
			"agent sessions in ways the daemon cannot fix itself: user " +
			"service install/load/active (MADR 0072 P2) and macOS privacy " +
			"protection (TCC / Full Disk Access; MADR 0069 D5).",
		RunE: func(cmd *cobra.Command, args []string) error {
			w := cmd.OutOrStdout()
			renderServiceDoctor(w, service.ProbeStatus("mcremote"))
			fmt.Fprintln(w)
			renderDoctor(w, runtime.GOOS, tcc.Probe())
			fmt.Fprintln(w)
			renderCredentialDoctor(w, probeCredentialStores())
			return nil
		},
	}
}

// credentialStore is one agent's credential location and what is in it,
// without any of the values (MADR 0074 §9).
type credentialStore struct {
	Agent     string
	Path      string
	Present   bool
	Upstreams []string
	Note      string
}

// probeCredentialStores reads each agent's store for presence only. Key
// material is dropped inside credstore, so nothing here can print a secret
// even by accident.
func probeCredentialStores() []credentialStore {
	out := make([]credentialStore, 0, 5)

	add := func(agent, path string, ids []string, note string) {
		out = append(out, credentialStore{
			Agent:     agent,
			Path:      path,
			Present:   credstore.FileExists(path),
			Upstreams: ids,
			Note:      note,
		})
	}

	if p, err := credstore.OpenCodeAuthPath(); err == nil {
		ids := make([]string, 0, 4)
		if entries, err := credstore.ReadJSONAuth(p); err == nil {
			for _, e := range entries {
				ids = append(ids, e.ID)
			}
		}
		add("opencode", p, ids, "")
	}
	if p, err := credstore.KiloAuthPath(); err == nil {
		ids := make([]string, 0, 4)
		if entries, err := credstore.ReadJSONAuth(p); err == nil {
			for _, e := range entries {
				ids = append(ids, e.ID)
			}
		}
		add("kilo", p, ids, "engine API is the write path; this file is the fallback")
	}
	if p, err := credstore.GooseConfigPath(); err == nil {
		var ids []string
		var note string
		if cfg, err := credstore.ReadGooseConfig(p); err == nil {
			ids = cfg.Providers
			if cfg.ActiveProvider != "" {
				note = "active: " + cfg.ActiveProvider
			}
		}
		add("goose", p, ids, note)
	}
	if p, err := credstore.CodexAuthPath(); err == nil {
		add("codex", p, nil, "device sign-in deletes this file at start (MADR 0074 D8)")
	}
	if p, err := credstore.GrokAuthPath(); err == nil {
		add("grok", p, nil, "")
	}
	return out
}

// renderCredentialDoctor prints where each agent keeps credentials and which
// upstreams are configured. Values are never read, so this output is safe to
// paste into an issue (MADR 0074 §9).
func renderCredentialDoctor(w io.Writer, stores []credentialStore) {
	fmt.Fprintln(w, "provider credentials (names only; no values are read)")
	for _, s := range stores {
		fmt.Fprintf(w, "  %-9s %s\n", s.Agent+":", s.Path)
		fmt.Fprintf(w, "    present:   %s\n", yesNo(s.Present))
		if len(s.Upstreams) > 0 {
			fmt.Fprintf(w, "    upstreams: %s\n", strings.Join(s.Upstreams, ", "))
		}
		if s.Note != "" {
			fmt.Fprintf(w, "    note:      %s\n", s.Note)
		}
	}
}

// renderServiceDoctor prints LaunchAgent / user-unit status (MADR 0072 P2).
func renderServiceDoctor(w io.Writer, st service.Status) {
	fmt.Fprintln(w, "mcremote user service")
	fmt.Fprintf(w, "  path:    %s\n", st.PlistOrUnit)
	fmt.Fprintf(w, "  present: %s\n", yesNo(st.PlistPresent))
	fmt.Fprintf(w, "  loaded:  %s\n", yesNo(st.Loaded))
	fmt.Fprintf(w, "  active:  %s\n", yesNo(st.Active))
	if st.Hint != "" {
		fmt.Fprintf(w, "  hint:    %s\n", st.Hint)
	} else if st.Active {
		fmt.Fprintln(w, "  OK: service is loaded and running")
	}
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

// renderDoctor writes the TCC diagnosis for one probe result. Split from the
// command so tests can cover every state without owning the host's TCC
// database.
func renderDoctor(w io.Writer, goos string, res tcc.ProbeResult) {
	fmt.Fprintln(w, "macOS privacy (TCC / Full Disk Access)")
	if goos != "darwin" {
		fmt.Fprintln(w, "  not applicable on this platform")
		return
	}
	// TCC attributes access to the *responsible process*: run from a
	// terminal, this probe reports the terminal app's grant, not the
	// LaunchAgent's. The service's authoritative verdict is the daemon's
	// own startup log line.
	fmt.Fprintln(w, "  (probing under this process's identity — the running service's")
	fmt.Fprintln(w, "  verdict is its startup log line; a terminal's grant does not")
	fmt.Fprintln(w, "  transfer to the LaunchAgent)")
	switch res {
	case tcc.Granted:
		fmt.Fprintln(w, "  OK: the daemon can read ~/Downloads — a privacy grant covers it.")
		fmt.Fprintln(w, "  Note: this is a lower-bound signal; iCloud Drive and external")
		fmt.Fprintln(w, "  volumes have separate protections.")
	case tcc.Denied:
		fmt.Fprintln(w, "  DENIED: macOS is blocking the daemon from protected folders.")
		fmt.Fprintln(w, "  Agent sessions under ~/Documents, ~/Desktop or ~/Downloads will")
		fmt.Fprintln(w, "  fail with \"operation not permitted\".")
		fmt.Fprintln(w, "")
		fmt.Fprintln(w, "  Fix: System Settings → Privacy & Security → Full Disk Access →")
		fmt.Fprintln(w, "  \"+\" → add the mcremote binary, then restart the service.")
		fmt.Fprintln(w, "  If a grant exists but stopped working after an upgrade, the")
		fmt.Fprintln(w, "  binary's code identity changed (unsigned builds change identity")
		fmt.Fprintln(w, "  every rebuild): remove and re-add it, or reset stale rows with")
		fmt.Fprintln(w, "    tccutil reset SystemPolicyAllFiles")
		fmt.Fprintln(w, "  To confirm a live denial:")
		fmt.Fprintln(w, "    log stream --predicate 'subsystem == \"com.apple.TCC\"' | grep -i deny")
		fmt.Fprintln(w, "  Details: docs/ops-macos-tcc.md")
	default:
		fmt.Fprintln(w, "  UNKNOWN: the probe location (~/Downloads) is missing or errored")
		fmt.Fprintln(w, "  in a non-TCC way; no verdict. See docs/ops-macos-tcc.md.")
	}
}
