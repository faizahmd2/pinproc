// Package console prints bare `pinproc`: one structured look at the machine as
// it is right now — resources with their pressure trend, the workloads that are
// running and what they hold, the busiest processes and anything that needs
// attention. It is a snapshot, not a live screen (that is what top and htop are
// for); it is built from the same inventory Snapshot the reports use.
package console

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/faizahmd2/pinproc/internal/inventory"
	"github.com/faizahmd2/pinproc/internal/render"
)

// Style decides colours and width.
type Style struct {
	Color bool
	Width int
}

// AutoStyle uses colour on a terminal unless NO_COLOR is set.
func AutoStyle(isTTY bool, width int) Style {
	_, noColor := os.LookupEnv("NO_COLOR")
	if width <= 0 {
		width = 150 // piped: keep whole lines
	}
	return Style{Color: isTTY && !noColor && os.Getenv("TERM") != "dumb", Width: width}
}

const (
	reset  = "\x1b[0m"
	bold   = "\x1b[1m"
	dim    = "\x1b[2m"
	red    = "\x1b[31m"
	green  = "\x1b[32m"
	yellow = "\x1b[33m"
	blue   = "\x1b[34m"
	cyan   = "\x1b[36m"
)

type painter struct{ on bool }

func (p painter) c(code, s string) string {
	if !p.on || s == "" {
		return s
	}
	return code + s + reset
}

// Render writes the snapshot.
func Render(w io.Writer, s *inventory.Snapshot, o render.Options, st Style) {
	p := painter{st.Color}
	width := max(80, min(st.Width, 160))
	var b strings.Builder
	line := func(s string) { b.WriteString(clip(s, width) + "\n") }

	// Identity.
	h := s.Host
	ip := h.PublicIP
	if ip == "" {
		ip = h.PrimaryIP
	}
	left := p.c(bold+cyan, " pinproc ") + " " + p.c(bold, h.Hostname)
	if ip != "" {
		left += p.c(dim, " · ") + ip
	}
	right := o.Time(s.CollectedAt)
	line(left + pad(width-visible(left)-visible(right)) + p.c(dim, right))
	plat := strings.TrimSpace(h.Vendor + " " + h.Product)
	line(" " + p.c(dim, strings.Join(nonEmpty(h.OS, h.Kernel, h.Arch, plat, "up "+render.Duration(h.Uptime)), " · ")))
	b.WriteString("\n")

	// Resources: now, plus the kernel's 10s / 60s / 300s pressure trend.
	line(p.c(bold, " RESOURCES") + pad(64) + p.c(dim, "stall 10s · 1m · 5m"))
	bar := 22
	c := s.CPU
	busyCores := float64(c.Cores) * c.BusyPct / 100
	line(fmt.Sprintf(" %-7s %-9s %s %s  %s  %s", "CPU", fmt.Sprintf("%d vCPU", c.Cores), p.meter(c.BusyPct, bar, 70, 90),
		rpad(fmt.Sprintf("%.2f cores busy", busyCores), 19), p.level(fmt.Sprintf("%3.0f%%", c.BusyPct), c.BusyPct, 70, 90),
		psi(p, c.PSI)+p.c(dim, fmt.Sprintf("   load %.2f %.2f %.2f", c.Load1, c.Load5, c.Load15))))
	m := s.Memory
	total := max(m.PhysicalBytes, m.TotalBytes)
	line(fmt.Sprintf(" %-7s %-9s %s %s  %s  %s", "MEMORY", size(total), p.meter(m.UsedPct, bar, 80, 90),
		rpad(size(m.UsedBytes)+" used", 19), p.level(fmt.Sprintf("%3.0f%%", m.UsedPct), m.UsedPct, 80, 90),
		psi(p, m.PSI)+p.c(dim, "   "+size(m.AvailableBytes)+" available · swap "+swap(m))))
	for i, d := range s.Disks {
		if i >= 3 {
			break
		}
		fsPct, fsLine := diskUse(d)
		label := ""
		if i == 0 {
			label = "DISK"
		}
		io := fmt.Sprintf("r %s · w %s · %.0f%% busy · %.1f ms", render.Rate(d.ReadBPS), render.Rate(d.WriteBPS), d.UtilPct, d.AwaitMS)
		trend := ""
		if i == 0 {
			trend = psi(p, s.IOPSI)
		}
		line(fmt.Sprintf(" %-7s %-9s %s %s  %s  %s", label, cut(d.Name, 9), p.meter(fsPct, bar, 75, 90), rpad(fsLine, 19),
			p.level(fmt.Sprintf("%3.0f%%", fsPct), fsPct, 75, 90), trend+p.c(dim, "   "+io)))
	}
	for i, n := range s.NICs {
		if i >= 2 {
			break
		}
		label := ""
		if i == 0 {
			label = "NETWORK"
		}
		link := "virtual"
		if n.SpeedMbps > 0 {
			link = render.Link(n.SpeedMbps)
		}
		extra := ""
		if i == 0 {
			extra = fmt.Sprintf(" · %d tcp · %d time-wait · %.1f retrans/s", s.Net.TCPInUse, s.Net.TCPTimeWait, s.Net.RetransPerSec)
		}
		drops := ""
		if n.DropsPS > 0 {
			drops = p.c(yellow, fmt.Sprintf(" · %.0f drops/s", n.DropsPS))
		}
		line(fmt.Sprintf(" %-7s %-9s rx %s · tx %s · %s%s%s", label, cut(n.Name, 9), render.Bits(n.RxBPS), render.Bits(n.TxBPS), link, p.c(dim, extra), drops))
	}
	b.WriteString("\n")

	// Workloads.
	svcs := workloads(s)
	if len(svcs) > 0 {
		line(p.c(bold, " WORKLOADS") + p.c(dim, "  what is running, heaviest first"))
		line(p.c(dim, fmt.Sprintf(" %-24s %-10s %8s %7s %10s %10s %7s  %-22s %s", "SERVICE", "USER", "MAIN PID", "CPU", "MEMORY", "DISK I/O", "THREADS", "LISTENS (clients)", "UP")))
		for _, sv := range svcs {
			name := sv.Name
			if sv.Kind == "systemd" && sv.Unit != "" {
				name = sv.Unit
			}
			up := ""
			if !sv.StartedAt.IsZero() {
				up = render.Duration(s.CollectedAt.Sub(sv.StartedAt))
			}
			if sv.Systemd != nil && sv.Systemd.Restarts > 0 {
				up += p.c(yellow, " ↻"+strconv.Itoa(sv.Systemd.Restarts))
			}
			line(fmt.Sprintf(" %-24s %-10s %8d %7s %10s %10s %7d  %-22s %s", cut(name, 24), cut(sv.User, 10), sv.MainPID,
				render.Cores(sv.CPUCores), size(sv.MemBytes), render.Rate(sv.ReadBPS+sv.WriteBPS), sv.Threads,
				cut(listens(sv), 22), up))
			if dep := peers(sv); dep != "" {
				line(p.c(dim, "   └ talks to "+dep))
			}
		}
		b.WriteString("\n")
	}

	// Processes.
	ps := append([]inventory.Process(nil), s.Processes...)
	sort.SliceStable(ps, func(i, j int) bool { return ps[i].CPUCores > ps[j].CPUCores })
	if len(ps) > 6 {
		ps = ps[:6]
	}
	if len(ps) > 0 {
		line(p.c(bold, " PROCESSES") + p.c(dim, "  busiest now, full program path"))
		line(p.c(dim, fmt.Sprintf(" %7s %-10s %6s %9s %5s  %s", "PID", "USER", "CPU", "MEM", "STATE", "COMMAND")))
		for _, pr := range ps {
			mem := pr.PSSBytes
			if mem == 0 {
				mem = pr.RSSBytes
			}
			cmd := pr.Cmdline
			if cmd == "" {
				cmd = pr.Exe
			}
			if cmd == "" {
				cmd = "[" + pr.Comm + "]"
			}
			if pr.Exe != "" && !strings.HasPrefix(cmd, "/") && !strings.HasPrefix(cmd, "[") {
				if i := strings.IndexByte(cmd, ' '); i > 0 {
					cmd = pr.Exe + cmd[i:]
				} else {
					cmd = pr.Exe
				}
			}
			line(fmt.Sprintf(" %7d %-10s %6s %9s %5s  %s", pr.PID, cut(pr.User, 10), render.Cores(pr.CPUCores), size(mem), pr.State, cmd))
		}
		t := s.Tasks
		blocked := strconv.Itoa(t.Blocked)
		if t.Blocked > 0 {
			blocked = p.c(yellow, blocked)
		}
		line(p.c(dim, fmt.Sprintf(" %d tasks · %d running · ", t.Total, t.Running)) + blocked + p.c(dim, fmt.Sprintf(" blocked (D) · %d zombie · %d threads", t.Zombie, t.Threads)))
		b.WriteString("\n")
	}

	// Attention.
	ok, warn := 0, []string{}
	for _, ch := range s.Checks {
		switch ch.State {
		case "ok":
			ok++
		case "warn":
			warn = append(warn, ch.Name+": "+ch.Value)
		}
	}
	if len(s.Checks) > 0 {
		head := p.c(bold, " CHECKS ") + p.c(green, fmt.Sprintf(" ✓ %d ok", ok))
		if len(warn) == 0 {
			line(head)
		} else {
			line(head + p.c(yellow, fmt.Sprintf("   ! %d need attention", len(warn))))
			for _, w := range warn {
				line(p.c(yellow, "   ! ") + w)
			}
		}
		b.WriteString("\n")
	}
	for _, n := range s.Notes {
		line(p.c(dim, " note: "+n))
	}
	line(p.c(dim, " pinproc report") + p.c(dim, " → full machine description (HTML + JSON) · ") + p.c(dim, "pinproc config list → settings"))
	io.WriteString(w, b.String())
}

func (p painter) meter(v float64, width int, warn, crit float64) string {
	v = max(0, min(v, 100))
	fill := int(v/100*float64(width) + 0.5)
	col := green
	if v >= crit {
		col = red
	} else if v >= warn {
		col = yellow
	}
	return p.c(col, strings.Repeat("█", fill)) + p.c(dim, strings.Repeat("░", width-fill))
}

func (p painter) level(s string, v, warn, crit float64) string {
	switch {
	case v >= crit:
		return p.c(bold+red, s)
	case v >= warn:
		return p.c(yellow, s)
	}
	return s
}

func psi(p painter, x inventory.PSI) string {
	if !x.Available {
		return p.c(dim, "psi n/a")
	}
	s := fmt.Sprintf("psi %4.1f · %4.1f · %4.1f", x.SomeAvg10, x.SomeAvg60, x.SomeAvg300)
	switch {
	case x.SomeAvg10 >= 30:
		return p.c(red, s)
	case x.SomeAvg10 >= 10:
		return p.c(yellow, s)
	}
	return p.c(dim, s)
}

func swap(m inventory.Memory) string {
	if m.SwapTotal == 0 {
		return "none"
	}
	return size(m.SwapUsed) + " / " + size(m.SwapTotal)
}

// size is a compact human size (one decimal under 100).
func size(v uint64) string {
	f := float64(v)
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	i := 0
	for f >= 1000 && i < len(units)-1 {
		f /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%.0f B", f)
	}
	if f >= 100 {
		return fmt.Sprintf("%.0f %s", f, units[i])
	}
	return fmt.Sprintf("%.1f %s", f, units[i])
}

// diskUse is the fill of the disk's filesystems (the root one first).
func diskUse(d inventory.Disk) (float64, string) {
	var used, sz uint64
	for _, m := range d.Mounts {
		used += m.UsedBytes
		sz += m.UsedBytes + m.AvailBytes
	}
	if sz == 0 {
		return 0, size(d.SizeBytes) + " unmounted"
	}
	return float64(used) / float64(sz) * 100, size(used) + " / " + size(sz)
}

// workloads picks the services worth a line: everything doing real work, the
// heaviest first, never more than 8.
func workloads(s *inventory.Snapshot) []inventory.Service {
	var out []inventory.Service
	total := float64(max(s.Memory.TotalBytes, 1))
	cores := float64(max(s.CPU.Cores, 1))
	for _, sv := range s.Services {
		if sv.Kind == "kernel" {
			continue
		}
		out = append(out, sv)
	}
	weight := func(sv inventory.Service) float64 {
		return float64(sv.MemBytes)/total + sv.CPUCores/cores
	}
	sort.SliceStable(out, func(i, j int) bool { return weight(out[i]) > weight(out[j]) })
	if len(out) > 8 {
		out = out[:8]
	}
	return out
}

func listens(sv inventory.Service) string {
	var parts []string
	seen := map[int]bool{}
	for _, l := range sv.Listen {
		if seen[l.Port] || !strings.HasPrefix(l.Proto, "tcp") {
			continue
		}
		seen[l.Port] = true
		v := ":" + strconv.Itoa(l.Port)
		if l.Clients > 0 {
			v += " (" + strconv.Itoa(l.Clients) + ")"
		}
		parts = append(parts, v)
	}
	if len(parts) == 0 {
		return "—"
	}
	return strings.Join(parts, " ")
}

func peers(sv inventory.Service) string {
	var parts []string
	for i, p := range sv.Peers {
		if i >= 3 {
			break
		}
		v := p.Addr + ":" + strconv.Itoa(p.Port)
		if p.Name != "" {
			v = p.Name + " " + v
		}
		parts = append(parts, fmt.Sprintf("%s ×%d", v, p.Conns))
	}
	return strings.Join(parts, " · ")
}

func nonEmpty(in ...string) []string {
	var out []string
	for _, s := range in {
		if strings.TrimSpace(s) != "" && s != "up " {
			out = append(out, s)
		}
	}
	return out
}

func cut(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

func rpad(s string, n int) string {
	if k := utf8.RuneCountInString(s); k < n {
		return s + strings.Repeat(" ", n-k)
	}
	return s
}

func pad(n int) string {
	if n < 1 {
		return " "
	}
	return strings.Repeat(" ", n)
}

// visible is the printed width of s, ignoring ANSI escapes.
func visible(s string) int {
	n, esc := 0, false
	for _, r := range s {
		switch {
		case r == 0x1b:
			esc = true
		case esc:
			if r == 'm' {
				esc = false
			}
		default:
			n++
		}
	}
	return n
}

// clip trims a line to width without cutting through an escape sequence.
func clip(s string, width int) string {
	if visible(s) <= width {
		return s
	}
	var b strings.Builder
	n, esc := 0, false
	for _, r := range s {
		if r == 0x1b {
			esc = true
		}
		if esc {
			b.WriteRune(r)
			if r == 'm' {
				esc = false
			}
			continue
		}
		if n >= width-1 {
			continue
		}
		b.WriteRune(r)
		n++
	}
	out := b.String() + "…"
	if strings.Contains(s, "\x1b[") {
		out += reset
	}
	return out
}
