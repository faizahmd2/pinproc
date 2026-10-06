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

// main runs the pinproc daemon or a CLI command.
func main() {
	if len(os.Args) == 1 {
		os.Args = append(os.Args, "service", "run")
	}
	logger = slog.New(slog.NewTextHandler(os.Stdout, nil))
	druntime.Local(logger)
	root := &cobra.Command{Use: "pinproc", Short: "pinproc — read-only Linux incident watcher", Version: version}
	root.PersistentFlags().StringVar(&cfgPath, "config", "", "config path")
	root.AddCommand(newServiceCmd(), newSetupCmd(), newConfigCommand(), newDoctorCmd(), newReportCmd())
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}
