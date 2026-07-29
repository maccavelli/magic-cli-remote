package cli

import (
	"fmt"
	"runtime"
	"text/tabwriter"

	"github.com/maccavelli/magic-cli-remote/internal/procutil"
	"github.com/spf13/cobra"
)

func newEnginesCmd() *cobra.Command {
	var reap bool
	cmd := &cobra.Command{
		Use:   "engines",
		Short: "List agent engine processes started by mcremote",
		Long: "Lists agent engine processes spawned by any mcremote on this host — goose\n" +
			"and opencode's `serve` engines, codex's `app-server` — showing whether the\n" +
			"daemon that owns each one is still running.\n\n" +
			"An engine whose owner is gone is an orphan: it holds a port (or stdio pipes)\n" +
			"and a few hundred MB for nothing. The daemon sweeps these at startup, so\n" +
			"this is for inspecting or clearing them without waiting for a restart.\n\n" +
			"Only processes carrying mcremote's ownership marker are ever listed or\n" +
			"stopped — an engine you started by hand is never touched.",
		Example: enginesExample,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			if runtime.GOOS != "linux" {
				return fmt.Errorf("engine discovery needs /proc and is only supported on Linux (this is %s)", runtime.GOOS)
			}

			if reap {
				n := procutil.ReapOrphanEngines(nil)
				if n == 0 {
					_, err := fmt.Fprintln(out, "No orphaned engines found.")
					return err
				}
				_, err := fmt.Fprintf(out, "Stopped %d orphaned engine(s).\n", n)
				return err
			}

			pids := procutil.FindByEnv(procutil.EnvEngineID)
			if len(pids) == 0 {
				_, err := fmt.Fprintln(out, "No mcremote-spawned engines are running.")
				return err
			}
			tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "PID\tOWNER\tSTATE\tENGINE ID")
			orphans := map[int]bool{}
			for _, o := range procutil.FindOrphanEngines() {
				orphans[o.PID] = true
			}
			for _, pid := range pids {
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
				fmt.Fprintf(tw, "%d\t%s\t%s\t%s\n", pid, owner, state, env[procutil.EnvEngineID])
			}
			if err := tw.Flush(); err != nil {
				return err
			}
			if len(orphans) > 0 {
				_, err := fmt.Fprintf(out,
					"\n%d orphaned engine(s). Run `mcremote engines --reap` to stop them.\n",
					len(orphans))
				return err
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&reap, "reap", false,
		"stop every engine whose owning daemon is gone")
	return cmd
}
