package main

import (
	"context"
	"fmt"
	"github.com/faizahmd2/vm-native-diagnos/internal/source"
	"github.com/spf13/cobra"
	"time"
)

// newCaptureCmd records a minimal local fixture.
func newCaptureCmd() *cobra.Command {
	var out string
	cmd := &cobra.Command{Use: "capture [host]", Short: "record local evidence into a replay fixture", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if out == "" {
			return fmt.Errorf("--out is required")
		}
		s := source.NewLocal("/proc", "/sys", 1<<20)
		defer s.Close()
		r := source.NewRecorder(s, out)
		_, e := r.Sample(context.Background(), []source.Read{{Key: "proc.stat", Path: "/proc/stat", Kind: source.ReadFile}, {Key: "proc.meminfo", Path: "/proc/meminfo", Kind: source.ReadFile}, {Key: "proc.loadavg", Path: "/proc/loadavg", Kind: source.ReadFile}}, time.Second)
		if e != nil {
			return e
		}
		fmt.Fprintln(cmd.OutOrStdout(), "fixture written to", out)
		return nil
	}}
	cmd.Flags().StringVar(&out, "out", "", "fixture directory")
	return cmd
}
