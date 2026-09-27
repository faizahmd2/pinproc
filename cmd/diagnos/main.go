package main

import (
	"fmt"
	"log/slog"
	"os"

	druntime "github.com/faizahmd2/pinproc/internal/runtime"
	"github.com/spf13/cobra"
)

var cfgPath string
var logger *slog.Logger
var version = "dev"
var commit = "none"
var date = "unknown"

func main() {
	logger = slog.New(slog.NewTextHandler(os.Stdout, nil))
	druntime.Local(logger)

	root := &cobra.Command{
		Use:     "pinproc",
		Short:   "pinproc — adaptive Linux resource investigation",
		Version: version,
	}
	root.PersistentFlags().StringVar(&cfgPath, "config", "", "configuration path for development commands")

	root.AddCommand(
		newServiceCmd(),
		newSetupCmd(),
		newProviderCmd(),
		newAIStatusCmd(),
		func() *cobra.Command {
			c := newServeCmd()
			c.Hidden = true
			return c
		}(),
		newInvestigateCmd(),
		newCaptureCmd(),
		newReplayCmd(),
	)

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}
