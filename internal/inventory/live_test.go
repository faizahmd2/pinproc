package inventory

import (
	"context"
	"encoding/json"
	"os"
	"testing"
)

// TestLiveDump prints a real snapshot as JSON. Run on Linux with PINPROC_LIVE=1.
func TestLiveDump(t *testing.T) {
	if os.Getenv("PINPROC_LIVE") == "" {
		t.Skip("set PINPROC_LIVE=1 on a Linux host")
	}
	snap, err := New(ReportOptions()).Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.MarshalIndent(snap, "", "  ")
	os.WriteFile(os.Getenv("PINPROC_LIVE"), b, 0644)
}
