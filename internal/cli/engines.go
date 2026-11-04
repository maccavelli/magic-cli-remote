package cli

import (
	"fmt"
	"io"
	"runtime"
	"text/tabwriter"

	"github.com/maccavelli/magic-cli-remote/internal/appdirs"
	"github.com/maccavelli/magic-cli-remote/internal/procutil"
	"github.com/spf13/cobra"
)

func newEnginesCmd() *cobra.Command {
	var reap bool
	cmd := &cobra.Command{
		Use:   "engines",
		Short: "List agent engine processes started by mcremote",
		Long: "Lists agent engine processes spawned by any mcremote on this host, showing\n" +
			"whether the daemon that owns each one is still running. PROVIDER names\n" +
			"each: an ACP agent by its provider (`opencode acp` as opencode, grok's\n" +
			"agent as grok), OpenCode's `serve --stdio` control plane as\n" +
			"opencode-control, kilo's `serve` engine as kilo, and codex's `app-server`\n" +
			"as codex.\n\n" +
			"An engine whose owner is gone is an orphan: it holds a port (or stdio pipes)\n" +
			"and a few hundred MB for nothing. The daemon sweeps these at startup, so\n" +
			"this is for inspecting or clearing them without waiting for a restart.\n\n" +
			"Ownership is recorded in the XDG state engine registry (all platforms) and\n" +
			"additionally stamped into the process environment on Linux. Only processes\n" +
			"carrying our marker/record are ever listed or stopped.",
		Example: enginesExample,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			paths, _, err := appdirs.SystemPaths(appdirs.ProductMcremote, "")
			if err != nil {
				return err
			}
			regDir := paths.EngineRegistryDir
			// Also scan sibling instance dirs under StateDir.
			allRecs, _ := procutil.ListEngineRecordsUnderState(paths.StateDir)
			// Include default instance dir records if not already covered.
			if recs, err := procutil.ListEngineRecords(regDir); err == nil {
				seen := map[string]bool{}
				for _, r := range allRecs {
					seen[r.ID] = true
				}
				for _, r := range recs {
					if !seen[r.ID] {
						allRecs = append(allRecs, r)
					}
				}
			}

			if reap {
				n := procutil.ReapOrphanEngines(nil, regDir)
				// Reap other instance registries under state.
				if instances, err := listInstanceEngineDirs(paths.StateDir); err == nil {
					for _, d := range instances {
						if d != regDir {
							n += procutil.ReapRegistryOrphans(nil, d)
						}
					}
				}
				if n == 0 {
					_, err := fmt.Fprintln(out, "No orphaned engines found.")
					return err
				}
				_, err := fmt.Fprintf(out, "Stopped %d orphaned engine(s).\n", n)
				return err
			}

			// Merge registry view with Linux env discovery.
			orphans := map[int]bool{}
			for _, o := range procutil.FindOrphanEngines() {
				orphans[o.PID] = true
			}
			for _, o := range procutil.FindRegistryOrphans(regDir) {
				orphans[o.PID] = true
			}
			rows, seenPID := recordRows(allRecs)
			if runtime.GOOS == "linux" {
				for _, pid := range procutil.FindByEnv(procutil.EnvEngineID) {
					if seenPID[pid] {
						continue
					}
					env, ok := procutil.ProcessEnv(pid)
					if !ok {
						continue
					}
					owner := env[procutil.EnvEngineOwner]
					if owner == "" {
						owner = "(unstamped)"
					}
					state := "live"
					if orphans[pid] {
						state = "ORPHAN"
					}
					// The environment marks an engine but does not name its
					// provider; only a registry record does.
					rows = append(rows, engineRow{
						pid:      fmt.Sprintf("%d", pid),
						owner:    owner,
						state:    state,
						provider: "(unrecorded)",
						id:       env[procutil.EnvEngineID],
					})
				}
			}
			return renderEngineRows(out, rows)
		},
	}
	cmd.Flags().BoolVar(&reap, "reap", false,
		"stop every engine whose owning daemon is gone")
	return cmd
}

func listInstanceEngineDirs(stateDir string) ([]string, error) {
	return procutil.ListEngineRegistryDirs(stateDir)
}

// engineRow is one line of `mcremote engines`.
type engineRow struct {
	pid, owner, state, provider, id string
}

// recordRows is a row for each registry record whose process still runs, and
// the PIDs it covered. The provider is the record's own: an ACP agent's
// provider id, opencode-control for OpenCode's control plane, or the engine's
// provider for kilo and codex (0214 PLAN P8 Deviation 1).
func recordRows(recs []procutil.EngineRecord) ([]engineRow, map[int]bool) {
	var rows []engineRow
	seen := map[int]bool{}
	for _, rec := range recs {
		if !procutil.RecordLive(rec) {
			continue
		}
		state := "live"
		if rec.Owner == "" || !procutil.OwnerAlive(rec.Owner) {
			state = "ORPHAN"
		}
		provider := rec.Provider
		if provider == "" {
			provider = "(unrecorded)"
		}
		rows = append(rows, engineRow{
			pid:      fmt.Sprintf("%d", rec.PID),
			owner:    rec.Owner,
			state:    state,
			provider: provider,
			id:       rec.ID,
		})
		seen[rec.PID] = true
	}
	return rows, seen
}

// renderEngineRows prints the table, and how to stop the orphans in it.
func renderEngineRows(out io.Writer, rows []engineRow) error {
	if len(rows) == 0 {
		_, err := fmt.Fprintln(out, "No mcremote-spawned engines are running.")
		return err
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "PID\tOWNER\tSTATE\tPROVIDER\tENGINE ID")
	orphanCount := 0
	for _, r := range rows {
		if r.state == "ORPHAN" {
			orphanCount++
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", r.pid, r.owner, r.state, r.provider, r.id)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if orphanCount > 0 {
		_, err := fmt.Fprintf(out,
			"\n%d orphaned engine(s). Run `mcremote engines --reap` to stop them.\n",
			orphanCount)
		return err
	}
	return nil
}
