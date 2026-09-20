package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"

	druntime "github.com/faizahmd2/vm-native-diagnos/internal/runtime"
	"github.com/spf13/cobra"
)

var cfgPath string
var logger *slog.Logger
var version = "dev"
var commit = "none"
var date = "unknown"

// main starts the local native agent.
func main() {
	if len(os.Args) == 1 {
		os.Args = append(os.Args, "serve")
	}
	logger = slog.New(slog.NewTextHandler(os.Stdout, nil))
	druntime.Local(logger)
	root := &cobra.Command{Use: "diagnos", Short: "Diagnos — adaptive Linux resource investigation", Version: version}
	root.PersistentFlags().StringVar(&cfgPath, "config", "", "config path")
	root.AddCommand(newServeCmd(), newInvestigateCmd(), newCaptureCmd(), newReplayCmd())
	if err := root.Execute(); err != nil {
		var ce commandError
		if errors.As(err, &ce) {
			os.Exit(int(ce))
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}
