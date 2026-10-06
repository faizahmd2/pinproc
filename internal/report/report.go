package report

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/faizahmd2/pinproc/internal/contract"
)

const reportFile = "report.md"

// WriteText stores the rendered report (plain text) for `pinproc report --last`.
func WriteText(root, text string) error {
	if err := os.MkdirAll(root, 0750); err != nil {
		return err
	}
	return atomic(filepath.Join(root, reportFile), []byte(text))
}

// ReadText returns the last stored report text, or os.ErrNotExist if none.
func ReadText(root string) (string, error) {
	b, err := os.ReadFile(filepath.Join(root, reportFile))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func EnsureWritable(root string) error {
	if err := os.MkdirAll(root, 0750); err != nil {
		return err
	}
	f, err := os.CreateTemp(root, ".pinproc-startup-*")
	if err != nil {
		return err
	}
	name := f.Name()
	if _, err := f.Write([]byte("ok")); err != nil {
		_ = f.Close()
		_ = os.Remove(name)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(name)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	return os.Remove(name)
}

func RenderMarkdown(inv *contract.Investigation) string {
	var b strings.Builder
	if inv.IncidentCheckedAt.IsZero() {
		inv.IncidentCheckedAt = inv.StartedAt
	}
	// Header: one identity line (host · ip · vCPU · uptime) + the human time.
	fmt.Fprintf(&b, "pinproc · %s%s\n", headerIdentity(inv), headerTime(inv))
	b.WriteString("\n")

	// One-line verdict a service owner can read at a glance.
	fmt.Fprintf(&b, "%s\n\n", verdict(inv))

	// The definitive cause (service + components + action), then how it built up.
	renderCause(&b, inv.Incident)
	renderIncidentTimeline(&b, inv.Incident)

	// Supporting findings — skip the owner finding(s) already stated in ## Cause, so
	// the report never says the same thing twice.
	renderFindings(&b, inv)

	// Runtime machine context (no fixed identity like OS/kernel/arch — it never
	// changes and only adds noise for a developer who knows their box).
	renderMachine(&b, inv)

	if detailedDiagnostics(inv) {
		b.WriteString("\n## Diagnostics\n\n")
		if len(inv.Hypotheses) > 0 {
			renderChain(&b, inv, inv.Hypotheses[0].Entity)
			if cmds := verifyForTopFinding(inv, inv.Hypotheses[0].Entity); len(cmds) > 0 {
				b.WriteString("Verify\n")
				for _, cmd := range cmds {
					fmt.Fprintf(&b, "   → %s\n", cmd)
				}
			}
		}
		if len(inv.NotInvestigated) > 0 {
			b.WriteString("\nNot investigated\n")
			for _, c := range inv.NotInvestigated {
				fmt.Fprintf(&b, "   %s(%s) — %s\n", c.Capability, c.Scope.ID, c.Reason)
			}
		}
	}

	notes := unique(inv.Limitations)
	if len(notes) > 0 || len(inv.Notices) > 0 {
		b.WriteString("\n## Notes\n\n")
		for _, n := range inv.Notices {
			fmt.Fprintf(&b, "- %s", n.Message)
			if n.Count > 1 {
				fmt.Fprintf(&b, " (x%d)", n.Count)
			}
			b.WriteString("\n")
		}
		for _, x := range notes {
			fmt.Fprintf(&b, "- %s\n", x)
		}
	}
	return b.String()
}

// headerIdentity renders "host · ip · N vCPU · up 2h".
func headerIdentity(inv *contract.Investigation) string {
	parts := []string{displayHost(inv)}
	if inv.Machine.PrimaryIP != "" {
		parts = append(parts, inv.Machine.PrimaryIP)
	}
	if inv.Machine.CPUs > 0 {
		parts = append(parts, fmt.Sprintf("%d vCPU", inv.Machine.CPUs))
	}
	if inv.Machine.Uptime > 0 {
		parts = append(parts, "up "+humanDuration(inv.Machine.Uptime))
	}
	return strings.Join(parts, " · ")
}

// headerTime renders the human, no-dependency timestamp, e.g.
// "   ·   27 Sep 2026, 09:00 PM (IST)".
func headerTime(inv *contract.Investigation) string {
	t := inv.IncidentCheckedAt
	if t.IsZero() {
		return ""
	}
	s := t.Format("02 Jan 2006, 03:04 PM (MST)")
	// If the zone resolved to a numeric offset (no tzdata), fall back to a plain,
	// unambiguous dd-mm-yyyy HH:MM:SS so we never print "(+0530)".
	if strings.Contains(s, "(+") || strings.Contains(s, "(-") {
		s = t.Format("02-01-2006 15:04:05")
	}
	return "    ·    " + s
}

// verdict is the single human sentence at the top.
func verdict(inv *contract.Investigation) string {
	if inv.Incident != nil && inv.Incident.Cause != nil {
		c := inv.Incident.Cause
		return fmt.Sprintf("%s pressure — %s is the cause: %s.", dimWord(c.Dimension), c.Service, causeValue(c.Value, c.Unit))
	}
	if len(inv.Hypotheses) > 0 {
		return inv.Hypotheses[0].Statement + "."
	}
	return "No material anomaly — the machine looks healthy."
}

func dimWord(d contract.Dimension) string {
	switch d {
	case contract.DimensionMemory:
		return "Memory"
	case contract.DimensionIO:
		return "Disk I/O"
	case contract.DimensionNetwork:
		return "Network"
	case contract.DimensionScheduling:
		return "Scheduling"
	default:
		return "CPU"
	}
}

// renderFindings lists supporting findings, skipping any already stated as the
// cause so the report does not repeat itself.
func renderFindings(b *strings.Builder, inv *contract.Investigation) {
	caused := contract.Dimension("")
	if inv.Incident != nil && inv.Incident.Cause != nil {
		caused = inv.Incident.Cause.Dimension
	}
	var shown []contract.Hypothesis
	for _, h := range inv.Hypotheses {
		// Drop the owner finding for the caused dimension (it is the ## Cause).
		if caused != "" && h.Dimension == caused && strings.HasPrefix(h.Source, "owner:") {
			continue
		}
		shown = append(shown, h)
	}
	if len(shown) == 0 {
		return
	}
	b.WriteString("## Findings\n\n")
	for _, h := range shown {
		fmt.Fprintf(b, "- %s", h.Statement)
		if h.Action != "" {
			fmt.Fprintf(b, "\n  → %s", h.Action)
		}
		if h.LogContext != nil {
			line := strings.ReplaceAll(h.LogContext.Line, "\"", "'")
			extra := ""
			if h.LogContext.Count > 1 {
				extra = fmt.Sprintf(" (x%d)", h.LogContext.Count)
			}
			fmt.Fprintf(b, "\n  log: %s — %q%s", h.LogContext.Path, line, extra)
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
}

func displayHost(inv *contract.Investigation) string {
	if inv == nil {
		return "unknown"
	}
	if inv.Host != "" && !isLocalReportHost(inv.Host) {
		return inv.Host
	}
	if inv.Machine.Hostname != "" {
		return inv.Machine.Hostname
	}
	return inv.Host
}

func isLocalReportHost(host string) bool {
	return host == "" || strings.EqualFold(host, "localhost") || host == "127.0.0.1"
}

// renderMachine shows only the runtime machine context — the numbers that change.
// Fixed identity (OS, kernel, arch) is intentionally omitted: it does not change
// for a developer's known box and only adds noise.
func renderMachine(b *strings.Builder, inv *contract.Investigation) {
	if inv == nil {
		return
	}
	s := inv.MachineSnapshot
	b.WriteString("## Machine\n\n")
	if s.CPUs > 0 {
		fmt.Fprintf(b, "CPU: %.0f%% used · load %.2f / %.2f / %.2f (1/5/15m)%s\n",
			s.CPUUtilizationPct, s.Load1, s.Load5, s.Load15, loadTrend(s.Load1, s.Load15))
	}
	if s.MemoryTotalBytes > 0 {
		fmt.Fprintf(b, "Memory: %s / %s used · %.0f%% · swap %.0f%%\n",
			formatBytes(s.MemoryUsedBytes), formatBytes(s.MemoryTotalBytes), s.MemoryUsedPct, s.SwapUsedPct)
	}
	if s.PrimaryDiskDevice != "" {
		fmt.Fprintf(b, "Disk: %s · read %.1f MB/s · write %.1f MB/s · util %.1f%% · await %.1f ms\n",
			s.PrimaryDiskDevice, s.DiskReadBPS/1024/1024, s.DiskWriteBPS/1024/1024, s.DiskUtilizationPct, s.DiskAwaitMS)
	} else if s.RootDiskTotalBytes > 0 {
		fmt.Fprintf(b, "Disk %s: %s / %s used · %.0f%%\n",
			s.RootDiskPath, formatBytes(s.RootDiskUsedBytes), formatBytes(s.RootDiskTotalBytes), s.RootDiskUsedPct)
	}
	if s.NetworkRxBPS > 0 || s.NetworkTxBPS > 0 {
		fmt.Fprintf(b, "Network: RX %.1f MB/s · TX %.1f MB/s · retrans %.1f/s\n",
			s.NetworkRxBPS/1024/1024, s.NetworkTxBPS/1024/1024, s.NetworkRetransmitsPerSec)
	}
	if s.SocketsUsed > 0 || s.TCPInUse > 0 || s.TCPTimeWait > 0 || s.TCPListenOverflow > 0 {
		fmt.Fprintf(b, "Sockets: %d · TCP in-use %d · TIME_WAIT %d · orphan %d · listen overflows %d\n",
			s.SocketsUsed, s.TCPInUse, s.TCPTimeWait, s.TCPOrphan, s.TCPListenOverflow)
	}
	b.WriteString("\n")
}

// renderCause states, definitively, which service caused the incident and which of
// its component processes carried the load — app-level, no probabilities.
func renderCause(b *strings.Builder, inc *contract.Incident) {
	if inc == nil || inc.Cause == nil {
		return
	}
	c := inc.Cause
	fmt.Fprintf(b, "## Cause\n\n%s — %s across %d process(es)\n",
		c.Service, causeValue(c.Value, c.Unit), c.Procs)
	for _, comp := range c.Components {
		name := comp.Comm
		if name == "" {
			name = "process"
		}
		fmt.Fprintf(b, "    • %s (pid %d)  %s  (%.0f%% of service)\n",
			name, comp.PID, causeValue(comp.Value, c.Unit), comp.Pct)
	}
	for _, p := range c.Ports {
		fmt.Fprintf(b, "    listening %s:%d\n", p.Proto, p.Port)
	}
	b.WriteString("\n")
}

func causeValue(v float64, unit string) string {
	switch unit {
	case "bytes":
		return formatBytes(uint64(v))
	case "bytes_per_sec":
		return fmt.Sprintf("%.1f MB/s", v/1024/1024)
	case "connections":
		return fmt.Sprintf("%.0f connections", v)
	default: // percent (of one core)
		return fmt.Sprintf("%.0f%% CPU (%.1f cores)", v, v/100)
	}
}

// renderIncidentTimeline shows how the incident built up before capture, with the
// armed dimension's climb, its top contender at each step, and the other resources
// for context (so a CPU incident still shows memory/io moving).
func renderIncidentTimeline(b *strings.Builder, inc *contract.Incident) {
	if inc == nil || len(inc.Timeline) == 0 {
		return
	}
	fmt.Fprintf(b, "## Incident — %s (%s)\n\n", inc.Dimension, inc.Reason)
	fmt.Fprintf(b, "Built up over %s before capture.\n\n", formatDuration(inc.FiredAt.Sub(inc.StartedAt)))
	b.WriteString("    +time   armed   top consumer                cpu   mem   io(psi)\n")
	start := inc.StartedAt
	for _, p := range inc.Timeline {
		top := "—"
		if len(p.Top) > 0 {
			top = fmt.Sprintf("%s (pid %d) %s", p.Top[0].Comm, p.Top[0].PID, incidentValue(p.Top[0]))
		}
		fmt.Fprintf(b, "    %5ds  %5.0f%%  %-26s %4.0f%% %4.0f%% %5.1f\n",
			int(p.At.Sub(start).Seconds()), p.ArmedLevel, truncate(top, 26), p.CPUUtil, p.MemUsed, p.IOPSI)
	}
	b.WriteString("\n")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

func incidentValue(c contract.IncidentContender) string {
	switch c.Unit {
	case "percent":
		return fmt.Sprintf("%.0f%%", c.Value)
	case "bytes":
		return formatBytes(uint64(c.Value))
	case "bytes_per_sec":
		return fmt.Sprintf("%.1f MB/s", c.Value/1024/1024)
	default:
		return fmt.Sprintf("%.0f", c.Value)
	}
}

func detailedDiagnostics(inv *contract.Investigation) bool {
	if inv == nil {
		return false
	}
	if inv.StopReason == contract.StopError || inv.StopReason == contract.StopBudgetTime {
		return true
	}
	for _, e := range inv.Evidence {
		if e.Err != "" || e.Unavailable != "" || e.TimedOut {
			return true
		}
	}
	return false
}

// loadTrend summarizes whether CPU pressure is building or easing by comparing
// the 1-minute to the 15-minute load average.
func loadTrend(load1, load15 float64) string {
	if load1 <= 0 || load15 <= 0 {
		return ""
	}
	switch {
	case load1 > load15*1.25:
		return " · rising"
	case load1 < load15*0.75:
		return " · easing"
	default:
		return " · steady"
	}
}

func humanDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.0fs", d.Seconds())
	}
	h := int(d / time.Hour)
	d -= time.Duration(h) * time.Hour
	m := int(d / time.Minute)
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

func formatBytes(v uint64) string {
	const unit = 1024.0
	if v == 0 {
		return "0 B"
	}
	if float64(v) >= unit*unit*unit {
		return fmt.Sprintf("%.1f GiB", float64(v)/(unit*unit*unit))
	}
	if float64(v) >= unit*unit {
		return fmt.Sprintf("%.1f MiB", float64(v)/(unit*unit))
	}
	if float64(v) >= unit {
		return fmt.Sprintf("%.1f KiB", float64(v)/unit)
	}
	return fmt.Sprintf("%d B", v)
}

func findingDetail(inv *contract.Investigation, h contract.Hypothesis) string {
	var parts []string
	if h.Entity.Display != "" {
		parts = append(parts, h.Entity.Display)
	}
	for _, id := range h.Support {
		for _, e := range inv.Evidence {
			if e.ID != id {
				continue
			}
			for _, o := range e.Observations {
				if len(parts) >= 3 {
					break
				}
				switch o.Unit {
				case "percent":
					parts = append(parts, fmt.Sprintf("%.0f%% %s", o.Value, shortKey(o.Key)))
				case "ms":
					parts = append(parts, fmt.Sprintf("%.0fms %s", o.Value, shortKey(o.Key)))
				case "bytes_per_sec":
					parts = append(parts, fmt.Sprintf("%.1fMB/s %s", o.Value/1024/1024, shortKey(o.Key)))
				case "count", "count_per_sec":
					parts = append(parts, fmt.Sprintf("%.0f %s", o.Value, shortKey(o.Key)))
				}
			}
		}
	}
	if len(parts) == 0 && len(h.Support) > 0 {
		return "evidence: " + strings.Join(h.Support, ", ")
	}
	return strings.Join(parts, " · ")
}

func shortKey(k string) string {
	if i := strings.LastIndex(k, ":"); i >= 0 {
		k = k[:i]
	}
	if i := strings.LastIndex(k, "."); i >= 0 {
		k = k[i+1:]
	}
	return strings.ReplaceAll(k, "_", " ")
}

func renderChain(b *strings.Builder, inv *contract.Investigation, top contract.Entity) {
	b.WriteString("machine")
	e := top
	seen := map[string]bool{}
	for {
		key := string(e.Kind) + "|" + e.ID
		if seen[key] {
			break
		}
		seen[key] = true
		label := e.Display
		if label == "" {
			label = e.ID
		}
		if e.Kind == contract.EntityProcess && e.Service != nil {
			label = e.Service.Name + " (" + e.ID + ")"
			if e.Service.Container != nil && e.Service.Container.Name != "" {
				b.WriteString(" → container " + e.Service.Container.Name)
			}
		}
		b.WriteString(" → " + strings.ToLower(string(e.Kind)) + " " + label)
		found := false
		for _, x := range inv.ObservedEntities {
			if x.ID == e.ParentID && x.Kind != contract.EntityMachine {
				e = x
				found = true
				break
			}
		}
		if !found {
			break
		}
	}
	b.WriteString("\n")
}

func verifyForTopFinding(inv *contract.Investigation, top contract.Entity) []string {
	allowed := map[string]bool{top.ID: true}
	current := top
	for current.ParentID != "" {
		found := false
		for _, e := range inv.ObservedEntities {
			if e.ID == current.ParentID {
				allowed[e.ID] = true
				current = e
				found = true
				break
			}
		}
		if !found {
			break
		}
	}
	seen := map[string]bool{}
	var out []string
	for _, e := range inv.Evidence {
		if !allowed[e.Entity.ID] {
			continue
		}
		for _, v := range e.Verify {
			if v != "" && !seen[v] {
				seen[v] = true
				out = append(out, v)
			}
		}
	}
	sort.Strings(out)
	return out
}

func unique(in []string) []string {
	m := map[string]bool{}
	out := []string{}
	for _, x := range in {
		if x != "" && !m[x] {
			m[x] = true
			out = append(out, x)
		}
	}
	return out
}

func formatDuration(d time.Duration) string {
	if d <= 0 {
		return "0.0s"
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

func atomic(path string, b []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".pinproc-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err = f.Write(b); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err = f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
