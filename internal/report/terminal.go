package report

import (
	"fmt"
	"github.com/faizahmd2/vm-native-diagnos/internal/contract"
	"io"
)

// Terminal prints a compact investigation summary.
func Terminal(w io.Writer, inv *contract.Investigation) error {
	if inv == nil {
		return fmt.Errorf("investigation is nil")
	}
	fmt.Fprintf(w, "status=%s stop=%s evidence=%d hypotheses=%d steps=%d\n", status(inv), inv.StopReason, len(inv.Evidence), len(inv.Hypotheses), len(inv.Path))
	for _, h := range inv.Hypotheses {
		fmt.Fprintf(w, "[%s] %.2f %s\n", h.Grade, h.Confidence, h.Statement)
	}
	return nil
}
func status(inv *contract.Investigation) string {
	if inv.StopReason == contract.StopNoAnomaly {
		return "healthy"
	}
	return "anomaly"
}
