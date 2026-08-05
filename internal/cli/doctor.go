package cli

import (
	"fmt"
	"io"
	"runtime"

	"github.com/maccavelli/magic-cli-remote/internal/cli/service"
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
			return nil
		},
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
