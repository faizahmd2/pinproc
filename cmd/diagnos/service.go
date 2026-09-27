package main

import "github.com/spf13/cobra"

// newServiceCmd exposes only the runtime entrypoint. Installation and removal
// are owned by the OS package manager.
func newServiceCmd() *cobra.Command {
	run := newServeCmd()
	run.Use = "run"
	run.Hidden = true
	run.Short = "run the persistent inspection service"

	cmd := &cobra.Command{
		Use:   "service",
		Short: "internal service lifecycle commands",
	}
	cmd.AddCommand(run)
	return cmd
}
