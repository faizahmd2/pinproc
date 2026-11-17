// Package tui is pinproc's terminal view: a top-like live screen (Bubble Tea +
// Lip Gloss) built from the same inventory snapshot the reports use, so what you
// see in the terminal is exactly what an alert or report would say.
package tui

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/faizahmd2/pinproc/internal/inventory"
	"github.com/faizahmd2/pinproc/internal/render"
)

var (
	cMuted  = lipgloss.AdaptiveColor{Light: "#64748b", Dark: "#7c8aa0"}
	cAccent = lipgloss.AdaptiveColor{Light: "#0284c7", Dark: "#38bdf8"}
	cOK     = lipgloss.AdaptiveColor{Light: "#059669", Dark: "#10b981"}
	cWarn   = lipgloss.AdaptiveColor{Light: "#d97706", Dark: "#f59e0b"}
	cCrit   = lipgloss.AdaptiveColor{Light: "#dc2626", Dark: "#ef4444"}
	cPurple = lipgloss.AdaptiveColor{Light: "#7c3aed", Dark: "#a855f7"}
	cTeal   = lipgloss.AdaptiveColor{Light: "#0d9488", Dark: "#2dd4bf"}
	cText   = lipgloss.AdaptiveColor{Light: "#0f172a", Dark: "#f1f5f9"}
	cTrack  = lipgloss.AdaptiveColor{Light: "#e2e8f0", Dark: "#334155"}

	muted  = lipgloss.NewStyle().Foreground(cMuted)
	accent = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
	bold   = lipgloss.NewStyle().Foreground(cText).Bold(true)
	head   = lipgloss.NewStyle().Foreground(cMuted).Bold(true)
	badge  = lipgloss.NewStyle().Background(cTrack).Foreground(cAccent).Padding(0, 1)
	box    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cMuted).Padding(0, 1)
)

// Sort keys for the process table.
const (
	SortCPU = 'c'
	SortMem = 'm'
	SortIO  = 'i'
	SortPID = 'p'
)

// View holds what one frame needs.
type View struct {
	Snap     *inventory.Snapshot
	Width    int
	Height   int // 0 = no limit (one-shot print)
	Sort     rune
	Services bool // expanded services table instead of processes
	Opt      render.Options
	Interval time.Duration
	Status   string
}

// Frame renders one screen.
func Frame(v View) string {
	s := v.Snap
	w := max(v.Width, 72)
	inner := w - 4
	var b strings.Builder

	// Header box.
	h := s.Host
	ip := h.PrimaryIP
	if h.PublicIP != "" {
		ip = h.PublicIP
	}
	l1 := bold.Render(h.Hostname)
	if ip != "" {
		l1 += " " + badge.Render(ip)
	}
	l1 += "  " + muted.Render(strings.Join(nonEmpty(h.OS, h.Kernel, h.Arch, "up "+render.Duration(h.Uptime)), " · "))
	meta := fmt.Sprintf("%s · %d vCPU · %s RAM", v.Opt.Clock(s.CollectedAt), s.CPU.Cores, render.BytesU(max(s.Memory.PhysicalBytes, s.Memory.TotalBytes)))
	if v.Interval > 0 {
		meta += fmt.Sprintf(" · every %s", v.Interval)
	}
	l2 := muted.Render(meta)
	if v.Status != "" {
		l2 += "  " + lipgloss.NewStyle().Foreground(cWarn).Render(v.Status)
	}
	b.WriteString(box.Width(w-2).Render(clip(l1, inner)+"\n"+clip(l2, inner)) + "\n")

	// Resource lines.
	bar := max(16, min(32, w/4))
	c := s.CPU
	cpuSegs := []segment{{c.UserPct, cAccent}, {c.SystemPct + c.IRQPct, cWarn}, {c.IOWaitPct + c.StealPct, cPurple}}
	b.WriteString(line("CPU", stackBar(cpuSegs, bar), fmt.Sprintf("%3.0f%%", c.BusyPct), level(c.BusyPct, 70, 90),
		fmt.Sprintf("usr %.0f · sys %.0f · wait %.0f · steal %.0f   load %.2f %.2f %.2f   psi %.1f", c.UserPct, c.SystemPct+c.IRQPct, c.IOWaitPct, c.StealPct, c.Load1, c.Load5, c.Load15, c.PSI.SomeAvg10)) + "\n")

	m := s.Memory
	var memSegs []segment
	var legend []string
	colors := map[string]lipgloss.AdaptiveColor{"apps": cAccent, "shmem": cTeal, "cache": cPurple, "kernel": cWarn, "huge": cCrit, "other": cMuted}
	for _, bk := range m.Buckets {
		if col, ok := colors[bk.Key]; ok {
			memSegs = append(memSegs, segment{bk.Value / float64(max(1, m.AccountedBytes)) * 100, col})
		}
		if bk.Value >= 1<<20 && bk.Key != "firmware" {
			legend = append(legend, lipgloss.NewStyle().Foreground(colorOr(colors, bk.Key)).Render("■")+" "+shortLabel(bk.Label)+" "+render.Bytes(bk.Value))
		}
	}
	b.WriteString(line("MEM", stackBar(memSegs, bar), fmt.Sprintf("%3.0f%%", m.UsedPct), level(m.UsedPct, 80, 90),
		fmt.Sprintf("%s used · %s available of %s · swap %s/%s · psi %.1f", render.BytesU(m.UsedBytes), render.BytesU(m.AvailableBytes), render.BytesU(m.TotalBytes), render.BytesU(m.SwapUsed), render.BytesU(m.SwapTotal), m.PSI.SomeAvg10)) + "\n")
	b.WriteString("      " + clip(strings.Join(legend, "  "), w-6) + "\n")

	for i, d := range s.Disks {
		if i >= 3 {
			break
		}
		var used, total, free uint64
		var mounts []string
		for _, mu := range d.Mounts {
			used += mu.UsedBytes
			total += mu.UsedBytes + mu.AvailBytes
			free += mu.AvailBytes
			mounts = append(mounts, mu.Path)
		}
		fill := 0.0
		where := "not mounted"
		if total > 0 {
			fill = float64(used) / float64(total) * 100
			where = fmt.Sprintf("%s free on %s", render.BytesU(free), strings.Join(mounts, " "))
		}
		ioSt := muted
		if d.UtilPct >= 60 {
			ioSt = level(d.UtilPct, 60, 90)
		}
		b.WriteString(line("DISK", stackBar([]segment{{fill, cAccent}}, bar), fmt.Sprintf("%3.0f%%", fill), level(fill, 75, 90),
			fmt.Sprintf("%s %s · %s · ", d.Name, render.Bytes(float64(d.SizeBytes)), where)) +
			ioSt.Render(fmt.Sprintf("busy %.0f%% · r %s w %s · %.1fms", d.UtilPct, render.Rate(d.ReadBPS), render.Rate(d.WriteBPS), d.AwaitMS)) + "\n")
	}
	for i, n := range s.NICs {
		if i >= 2 {
			break
		}
		util := 0.0
		if n.SpeedMbps > 0 {
			util = (n.RxBPS + n.TxBPS) * 8 / (float64(n.SpeedMbps) * 1e6) * 100
		}
		b.WriteString(line("NET", stackBar([]segment{{util, cTeal}}, bar), fmt.Sprintf("%3.0f%%", util), level(util, 60, 85),
			fmt.Sprintf("%s %s · ↓ %s ↑ %s · drops %d · retrans %.1f/s · tcp %d tw %d", n.Name, render.Link(n.SpeedMbps), render.Bits(n.RxBPS), render.Bits(n.TxBPS), n.RxDrops+n.TxDrops, s.Net.RetransPerSec, s.Net.TCPInUse, s.Net.TCPTimeWait)) + "\n")
	}
	t := s.Tasks
	b.WriteString(muted.Render(fmt.Sprintf("      tasks %d · running %d · blocked(D) %d · zombie %d · threads %d · open files %d/%d",
		t.Total, t.Running, t.Blocked, t.Zombie, t.Threads, s.Net.FDUsed, s.Net.FDMax)) + "\n\n")

	used := strings.Count(b.String(), "\n")
	rows := 0
	if v.Height > 0 {
		rows = max(4, v.Height-used-5)
	}
	if v.Services {
		b.WriteString(servicesTable(s, w, rows))
	} else {
		svcRows := 5
		if rows > 0 {
			svcRows = min(5, max(2, rows/3))
		}
		b.WriteString(servicesTable(s, w, svcRows))
		b.WriteString("\n")
		prows := 0
		if rows > 0 {
			prows = max(3, rows-svcRows-3)
		}
		b.WriteString(processTable(s, w, prows, v.Sort))
	}
	if v.Height > 0 {
		b.WriteString("\n" + muted.Render(clip("q quit · c cpu · m mem · i io · p pid · tab services · +/- speed", w)))
	}
	return b.String()
}

type segment struct {
	pct   float64
	color lipgloss.AdaptiveColor
}

func stackBar(segs []segment, width int) string {
	var b strings.Builder
	usedCells := 0
	for _, sg := range segs {
		n := int(sg.pct/100*float64(width) + 0.5)
		if n > width-usedCells {
			n = width - usedCells
		}
		if n > 0 {
			b.WriteString(lipgloss.NewStyle().Foreground(sg.color).Render(strings.Repeat("█", n)))
			usedCells += n
		}
	}
	if rest := width - usedCells; rest > 0 {
		b.WriteString(lipgloss.NewStyle().Foreground(cTrack).Render(strings.Repeat("░", rest)))
	}
	return b.String()
}

func level(v, warn, crit float64) lipgloss.Style {
	switch {
	case v >= crit:
		return lipgloss.NewStyle().Foreground(cCrit).Bold(true)
	case v >= warn:
		return lipgloss.NewStyle().Foreground(cWarn).Bold(true)
	}
	return lipgloss.NewStyle().Foreground(cOK).Bold(true)
}

func line(name, bar, val string, st lipgloss.Style, detail string) string {
	return accent.Render(fmt.Sprintf("%-5s", name)) + " " + bar + " " + st.Render(val) + "  " + muted.Render(detail)
}

func servicesTable(s *inventory.Snapshot, w, rows int) string {
	list := append([]inventory.Service(nil), s.Services...)
	sort.SliceStable(list, func(i, j int) bool { return list[i].MemBytes > list[j].MemBytes })
	if rows <= 0 {
		rows = 8
	}
	nameW := max(18, min(32, w-62))
	var b strings.Builder
	b.WriteString(head.Render(fmt.Sprintf("%-*s %5s %7s %10s %10s %10s  %s", nameW, "SERVICES (by memory)", "PROCS", "CPU", "MEM", "READ", "WRITE", "KIND")) + "\n")
	for i, sv := range list {
		if i >= rows {
			break
		}
		name := sv.Name
		if sv.Kind == "systemd" && sv.Unit != "" {
			name = sv.Unit
		}
		b.WriteString(fmt.Sprintf("%-*s %5d %7s %10s %10s %10s  %s\n", nameW, trunc(name, nameW), sv.Procs, render.Cores(sv.CPUCores),
			render.BytesU(sv.MemBytes), render.Rate(sv.ReadBPS), render.Rate(sv.WriteBPS), muted.Render(sv.Kind)))
	}
	return b.String()
}

func processTable(s *inventory.Snapshot, w, rows int, key rune) string {
	ps := append([]inventory.Process(nil), s.Processes...)
	sort.SliceStable(ps, func(i, j int) bool {
		a, b := ps[i], ps[j]
		switch key {
		case SortMem:
			return a.RSSBytes > b.RSSBytes
		case SortIO:
			return a.ReadBPS+a.WriteBPS > b.ReadBPS+b.WriteBPS
		case SortPID:
			return a.PID < b.PID
		}
		return a.CPUCores > b.CPUCores
	})
	if rows <= 0 {
		rows = 15
	}
	names := map[rune]string{SortCPU: "cpu", SortMem: "mem", SortIO: "io", SortPID: "pid"}
	cmdW := max(20, w-62)
	var b strings.Builder
	b.WriteString(head.Render(fmt.Sprintf("%7s %-9s %6s %9s %9s %9s  %-*s", "PID", "USER", "CPU", "MEM", "READ", "WRITE", cmdW, "COMMAND (sort: "+names[key]+")")) + "\n")
	for i, p := range ps {
		if i >= rows {
			break
		}
		cmd := p.Cmdline
		if cmd == "" {
			cmd = p.Exe
		}
		if cmd == "" {
			cmd = "[" + p.Comm + "]"
		} else if p.Exe != "" && !strings.HasPrefix(cmd, "/") {
			if j := strings.IndexByte(cmd, ' '); j > 0 {
				cmd = p.Exe + cmd[j:]
			} else {
				cmd = p.Exe
			}
		}
		cpuSt := lipgloss.NewStyle()
		if p.CPUCores >= 0.9 {
			cpuSt = cpuSt.Foreground(cCrit)
		} else if p.CPUCores >= 0.5 {
			cpuSt = cpuSt.Foreground(cWarn)
		}
		b.WriteString(fmt.Sprintf("%7d %-9s %s %9s %9s %9s  %s\n", p.PID, trunc(p.User, 9), cpuSt.Render(fmt.Sprintf("%6.2f", p.CPUCores)),
			render.BytesU(p.RSSBytes), render.Rate(p.ReadBPS), render.Rate(p.WriteBPS), trunc(cmd, cmdW)))
	}
	return b.String()
}

func nonEmpty(xs ...string) []string {
	var out []string
	for _, x := range xs {
		if strings.TrimSpace(x) != "" && x != "up 0m" {
			out = append(out, x)
		}
	}
	return out
}

func shortLabel(l string) string {
	if i := strings.Index(l, " ("); i > 0 {
		l = l[:i]
	}
	return strings.ToLower(l)
}

func colorOr(m map[string]lipgloss.AdaptiveColor, k string) lipgloss.AdaptiveColor {
	if c, ok := m[k]; ok {
		return c
	}
	if k == "free" {
		return cOK
	}
	return cMuted
}

func trunc(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}

// clip cuts a styled line to n visible cells.
func clip(s string, n int) string {
	if lipgloss.Width(s) <= n {
		return s
	}
	return lipgloss.NewStyle().MaxWidth(n).Render(s)
}

// --- live program ----------------------------------------------------------

type snapMsg struct {
	s   *inventory.Snapshot
	err error
}
type tickMsg struct{}

type model struct {
	col      *inventory.Collector
	snap     *inventory.Snapshot
	err      error
	w, h     int
	sort     rune
	services bool
	interval time.Duration
	opt      render.Options
	busy     bool
}

func (m *model) collect() tea.Cmd {
	m.busy = true
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		s, err := m.col.Collect(ctx)
		return snapMsg{s, err}
	}
}

func (m *model) Init() tea.Cmd { return m.collect() }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "c", "m", "i", "p":
			m.sort = rune(msg.String()[0])
		case "tab", "s":
			m.services = !m.services
		case "+", "=":
			m.interval = min(m.interval*2, 30*time.Second)
		case "-", "_":
			m.interval = max(m.interval/2, 500*time.Millisecond)
		}
	case snapMsg:
		m.busy = false
		if msg.err == nil {
			m.snap, m.err = msg.s, nil
		} else {
			m.err = msg.err
		}
		return m, tea.Tick(m.interval, func(time.Time) tea.Msg { return tickMsg{} })
	case tickMsg:
		if !m.busy {
			return m, m.collect()
		}
	}
	return m, nil
}

func (m *model) View() string {
	if m.snap == nil {
		if m.err != nil {
			return "pinproc: " + m.err.Error() + "\n"
		}
		return muted.Render("pinproc · sampling the machine…") + "\n"
	}
	status := ""
	if m.err != nil {
		status = m.err.Error()
	}
	return Frame(View{Snap: m.snap, Width: m.w, Height: m.h, Sort: m.sort, Services: m.services, Opt: m.opt, Interval: m.interval, Status: status})
}

// Run starts the live view until the user quits.
func Run(opt render.Options, interval time.Duration) error {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	o := inventory.LiveOptions()
	o.Window = time.Second
	m := &model{col: inventory.New(o), sort: SortCPU, interval: interval, opt: opt}
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

// Once prints a single frame (no alternate screen), like `top -b -n 1`.
func Once(w io.Writer, opt render.Options, width int) error {
	o := inventory.LiveOptions()
	s, err := inventory.New(o).Collect(context.Background())
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, Frame(View{Snap: s, Width: width, Sort: SortCPU, Opt: opt}))
	return err
}
