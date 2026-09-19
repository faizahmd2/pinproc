package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/faizahmd2/vm-native-diagnos/internal/source"
	"github.com/spf13/cobra"
)

// newReplayCmd reads the next fixture snapshot.
func newReplayCmd() *cobra.Command {
	return &cobra.Command{Use: "replay DIR", Short: "replay a captured fixture", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		r := source.NewReplay(args[0])
		s, e := r.Snapshot(context.Background(), nil)
		if e != nil {
			return e
		}
		b, e := json.MarshalIndent(s, "", "  ")
		if e != nil {
			return e
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(b))
		return nil
	}}
}
