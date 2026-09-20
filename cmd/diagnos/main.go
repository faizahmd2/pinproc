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

// main starts the pinproc service and CLI.
func main() {
	if len(os.Args) == 1 {
		os.Args = append(os.Args, "service", "run")
	}
	logger = slog.New(slog.NewTextHandler(os.Stdout, nil))
	druntime.Local(logger)
	root := &cobra.Command{Use: "pinproc", Short: "Diagnos — adaptive Linux resource investigation", Version: version}
	root.PersistentFlags().StringVar(&cfgPath, "config", "", "config path")
	legacyServe := newServeCmd(); legacyServe.Hidden = true
	root.AddCommand(newServiceCmd(), legacyServe, newInvestigateCmd(), newCaptureCmd(), newReplayCmd())
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}
