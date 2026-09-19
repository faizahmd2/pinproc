package report

import (
	"encoding/json"
	"fmt"
	"github.com/faizahmd2/diagnos/internal/contract"
	"os"
	"path/filepath"
	"strings"
)

// Write persists both the stable JSON contract and Markdown rendering atomically.
func Write(inv *contract.Investigation, dir string) error {
	if inv == nil {
		return fmt.Errorf("investigation is nil")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(inv, "", "  ")
	if err != nil {
		return err
	}
	if err := atomic(filepath.Join(dir, "investigation.json"), append(b, '\n')); err != nil {
		return err
	}
	return atomic(filepath.Join(dir, "report.md"), []byte(RenderMarkdown(inv)))
}
func atomic(path string, b []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".diagnos-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmp)
		}
	}()
	if _, err = f.Write(b); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmp, path); err != nil {
		return err
	}
	ok = true
	return nil
}

// RenderMarkdown renders only facts represented by the investigation contract.
func RenderMarkdown(inv *contract.Investigation) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Diagnos Report — %s\n\n", inv.Host)
	fmt.Fprintf(&b, "Machine    %s · %s · %s · kernel %s · %d vCPU · %s GiB\n", inv.Machine.Hostname, inv.Machine.PrimaryIP, inv.Machine.OS, inv.Machine.Kernel, inv.Machine.CPUs, formatGiB(inv.Machine.MemTotal))
	fmt.Fprintf(&b, "Trigger    %s", inv.Trigger)
	if inv.Hint != "" {
		fmt.Fprintf(&b, " · %q", inv.Hint)
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "Duration   %s · %d evidence · %d decision calls · budget: depth %d\n\n", inv.Duration, len(inv.Evidence), inv.Spent.DecisionCalls, inv.Budget.MaxDepth)
	b.WriteString("## Summary\n\n")
	if inv.Narrative != "" {
		b.WriteString(inv.Narrative)
		b.WriteString("\n")
	}
	if len(inv.Hypotheses) == 0 {
		b.WriteString("No material anomaly was established.\n")
	} else {
		b.WriteString(inv.Hypotheses[0].Statement + "\n")
	}
	b.WriteString("\n## Findings\n\n")
	for i, h := range inv.Hypotheses {
		fmt.Fprintf(&b, "### %d. %s [%s] confidence %.2f\n", i+1, h.Statement, h.Grade, h.Confidence)
		if len(h.Support) > 0 {
			fmt.Fprintf(&b, "  → evidence %s\n", strings.Join(h.Support, ", "))
		}
		if len(h.Contradicts) > 0 {
			fmt.Fprintf(&b, "  ↯ contradicts %s\n", strings.Join(h.Contradicts, ", "))
		}
	}
	b.WriteString("\n## Attribution chain\n\n")
	renderChain(&b, inv)
	b.WriteString("\n## Verify this yourself\n\n")
	seen := map[string]bool{}
	for _, e := range inv.Evidence {
		for _, v := range e.Verify {
			if !seen[v] {
				fmt.Fprintf(&b, "  %s\n", v)
				seen[v] = true
			}
		}
	}
	if len(seen) == 0 {
		b.WriteString("  none\n")
	}
	b.WriteString("\n## Not investigated (budget)\n\n")
	if len(inv.NotInvestigated) == 0 {
		b.WriteString("  none\n")
	} else {
		for _, c := range inv.NotInvestigated {
			fmt.Fprintf(&b, "  %s(%s) — %s\n", c.Capability, c.Scope.ID, c.Reason)
		}
	}
	b.WriteString("\n## Limitations\n\n")
	if len(inv.Limitations) == 0 {
		b.WriteString("  none\n")
	} else {
		for _, x := range inv.Limitations {
			fmt.Fprintf(&b, "  - %s\n", x)
		}
	}
	return b.String()
}
func renderChain(b *strings.Builder, inv *contract.Investigation) {
	chain := []string{}
	for _, e := range inv.Evidence {
		if e.Entity.Kind == contract.EntityProcess || e.Entity.Kind == contract.EntityThread || e.Entity.Kind == contract.EntityContainer || e.Entity.Kind == contract.EntityCgroup {
			d := e.Entity.Display
			if d == "" {
				d = e.Entity.ID
			}
			chain = append(chain, "  └── "+d)
		}
	}
	if len(chain) == 0 {
		b.WriteString("  machine\n")
		return
	}
	b.WriteString("  machine\n")
	for _, x := range chain {
		b.WriteString(x + "\n")
	}
}
func formatGiB(v uint64) string {
	if v == 0 {
		return "0"
	}
	return fmt.Sprintf("%.1f", float64(v)/(1024*1024*1024))
}
