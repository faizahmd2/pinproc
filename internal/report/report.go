package report

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/faizahmd2/pinproc/internal/contract"
)

type RunningStatus struct {
	ID        string    `json:"id"`
	Host      string    `json:"host"`
	Trigger   string    `json:"trigger,omitempty"`
	StartedAt time.Time `json:"started_at"`
}

func WriteRunning(root, id, host, trigger string) error {
	if id == "" {
		return fmt.Errorf("running status id is empty")
	}
	status := RunningStatus{ID: id, Host: host, Trigger: trigger, StartedAt: time.Now()}
	b, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return err
	}
	return atomic(filepath.Join(root, "running.json"), append(b, '\n'))
}

func ReadRunning(root string) (RunningStatus, error) {
	b, err := os.ReadFile(filepath.Join(root, "running.json"))
	if err != nil {
		return RunningStatus{}, err
	}
	var status RunningStatus
	if err := json.Unmarshal(b, &status); err != nil {
		return RunningStatus{}, err
	}
	return status, nil
}

func WriteFailure(root, id, host, trigger, hint string, budget contract.Budget, reason string, stop contract.StopReason) error {
	if stop == "" {
		stop = contract.StopError
	}
	inv := &contract.Investigation{SchemaVersion: contract.SchemaVersion, ID: id, Host: host, Trigger: trigger, Hint: hint, StartedAt: time.Now(), Budget: budget, StopReason: stop, Limitations: []string{"investigation failed completely: " + reason}}
	return Write(inv, root)
}

func Write(inv *contract.Investigation, root string) error {
	if inv == nil {
		return fmt.Errorf("investigation is nil")
	}
	if inv.ID == "" {
		return fmt.Errorf("investigation id is empty")
	}
	if err := os.MkdirAll(root, 0755); err != nil {
		return err
	}
	dir := filepath.Join(root, inv.ID)
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
	if err := atomic(filepath.Join(dir, "report.md"), []byte(RenderMarkdown(inv))); err != nil {
		return err
	}
	if err := retain(root, 3); err != nil {
		return err
	}
	_ = os.Remove(filepath.Join(root, "running.json"))
	return atomic(filepath.Join(root, "latest.txt"), []byte(inv.ID+"\n"))
}

func EnsureWritable(root string) error {
	if err := os.MkdirAll(root, 0755); err != nil {
		return err
	}
	f, err := os.CreateTemp(root, ".diagnos-startup-*")
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

func LatestDir(root string) (string, error) {
	b, err := os.ReadFile(filepath.Join(root, "latest.txt"))
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(string(b))
	if id == "" {
		return "", fmt.Errorf("latest report pointer is empty")
	}
	return filepath.Join(root, id), nil
}

func retain(root string, keep int) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "inv-") {
			ids = append(ids, e.Name())
		}
	}
	sort.Strings(ids)
	if len(ids) <= keep {
		return nil
	}
	for _, id := range ids[:len(ids)-keep] {
		if err := os.RemoveAll(filepath.Join(root, id)); err != nil {
			return err
		}
	}
	return nil
}

func atomic(path string, b []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".diagnos-*")
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
	return os.Rename(tmp, path)
}

func RenderMarkdown(inv *contract.Investigation) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# pinproc — %s\n\n", displayHost(inv))
	renderMachineSnapshot(&b, inv)

	if len(inv.Hypotheses) > 0 {
		b.WriteString(inv.Hypotheses[0].Statement)
	} else {
		b.WriteString("No material anomaly was established.")
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "%s · %d levels · budget: %s\n", formatDuration(inv.Duration), inv.Spent.Depth, budgetName(inv))

	for i, h := range inv.Hypotheses {
		fmt.Fprintf(&b, "\n%d. %s [%s] %.2f\n", i+1, h.Statement, h.Grade, h.Confidence)
		if detail := findingDetail(inv, h); detail != "" {
			fmt.Fprintf(&b, "   %s\n", detail)
		}
		if h.LogContext != nil {
			line := strings.ReplaceAll(h.LogContext.Line, "\"", "'")
			extra := ""
			if h.LogContext.Count > 1 {
				extra = fmt.Sprintf(" (x %d)", h.LogContext.Count)
			}
			fmt.Fprintf(&b, "   log: %s — %q%s\n", h.LogContext.Path, line, extra)
		}
	}

	if detailedDiagnostics(inv) {
		b.WriteString("\n")
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

	if len(inv.Limitations) > 0 {
		b.WriteString("\nLimitations\n")
		for _, x := range unique(inv.Limitations) {
			fmt.Fprintf(&b, "   - %s\n", x)
		}
	}
	if len(inv.Notices) > 0 {
		b.WriteString("\n## Notices\n\n")
		for _, n := range inv.Notices {
			fmt.Fprintf(&b, "⚠ %s — %s", n.Capability, n.Message)
			if n.Count > 1 {
				fmt.Fprintf(&b, " (x%d)", n.Count)
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

func displayHost(inv *contract.Investigation) string {
	if inv != nil && isLocalReportHost(inv.Host) && inv.Machine.Hostname != "" {
		return inv.Machine.Hostname
	}
	if inv != nil && inv.Host != "" {
		return inv.Host
	}
	return "localhost"
}

func isLocalReportHost(host string) bool {
	return host == "" || strings.EqualFold(host, "localhost") || host == "127.0.0.1"
}

func renderMachineSnapshot(b *strings.Builder, inv *contract.Investigation) {
	if inv == nil {
		return
	}
	m := inv.Machine
	s := inv.MachineSnapshot
	b.WriteString("## Machine snapshot\n\n")
	if m.PrimaryIP != "" {
		fmt.Fprintf(b, "IP: %s\n", m.PrimaryIP)
	}
	if m.OS != "" {
		fmt.Fprintf(b, "OS: %s\n", m.OS)
	}
	if m.Kernel != "" {
		fmt.Fprintf(b, "Kernel: %s\n", kernelShort(m.Kernel))
	}
	if m.Architecture != "" || m.CPUs > 0 {
		fmt.Fprintf(b, "Arch: %s · CPUs: %d\n", m.Architecture, m.CPUs)
	}
	if m.Uptime > 0 {
		fmt.Fprintf(b, "Uptime: %s\n", humanDuration(m.Uptime))
	}
	if s.CPUs > 0 {
		fmt.Fprintf(b, "CPU: %.0f%% used · load1 %.2f\n", s.CPUUtilizationPct, s.Load1)
	}
	if s.MemoryTotalBytes > 0 {
		fmt.Fprintf(b, "Memory: %s / %s used · %.0f%% · swap %.0f%%\n",
			formatBytes(s.MemoryUsedBytes), formatBytes(s.MemoryTotalBytes), s.MemoryUsedPct, s.SwapUsedPct)
	}
	if s.RootDiskTotalBytes > 0 {
		fmt.Fprintf(b, "Disk %s: %s / %s used · %.0f%%\n",
			s.RootDiskPath, formatBytes(s.RootDiskUsedBytes), formatBytes(s.RootDiskTotalBytes), s.RootDiskUsedPct)
	}
	b.WriteString("\n")
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

func kernelShort(kernel string) string {
	kernel = strings.TrimSpace(kernel)
	if strings.HasPrefix(kernel, "Linux version ") {
		kernel = strings.TrimPrefix(kernel, "Linux version ")
	}
	if i := strings.IndexByte(kernel, ' '); i >= 0 {
		kernel = kernel[:i]
	}
	return kernel
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
func sameEntity(a, b contract.Entity) bool {
	return a.Kind == b.Kind && a.ID == b.ID && a.ParentID == b.ParentID
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
func budgetName(inv *contract.Investigation) string {
	if inv.Budget.MaxDepth <= 2 {
		return "fast"
	}
	if inv.Budget.MaxDepth >= 5 {
		return "deep"
	}
	return "normal"
}
func formatDuration(d time.Duration) string {
	if d <= 0 {
		return "0.0s"
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}
