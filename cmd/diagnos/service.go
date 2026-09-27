package main

import "github.com/spf13/cobra"

// newServiceCmd exposes only the service runtime entrypoint. Installation,
// account creation, enablement and removal are owned by the Debian package.
func newServiceCmd() *cobra.Command {
	run := newServeCmd()
	run.Use = "run"
	run.Short = "run the persistent inspection service"

	cmd := &cobra.Command{
		Use:   "service",
		Short: "run the persistent pinproc service"
		RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	cmd.AddCommand(run)
	return cmd
}