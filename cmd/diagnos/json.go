package main

import (
	"encoding/json"
	"io"
)

// writeJSON emits stable pretty JSON.
func writeJSON(w io.Writer, value any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}
