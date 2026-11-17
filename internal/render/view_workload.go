package render

import (
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/faizahmd2/pinproc/internal/inventory"
	"github.com/faizahmd2/pinproc/internal/journal"
)

// kvGroup is one titled block inside a workload card.
type kvGroup struct {
	Title string
	Rows  []kv
}

type listenRow struct {
	Proto, Addr, Port, Service, PID, Clients, Scope, ScopeClass string
}

type checkRow struct {
	Area, Name, State, Icon, Value, Hint, Cmd string
}

type logSection struct {
	Title, Cmd, Note string
	Lines            []logLine
}

type logLine struct {
	At, Prio, PrioClass, Who, Msg string
	Count                         int
}

func since(now, t time.Time) string {
	if t.IsZero() || now.Before(t) {
		return ""
	}
	return Duration(now.Sub(t))
}

// workloadGroups is the card body for one service: how it runs, what it uses,
// whom it talks to, how much pressure it is under, and the commands that verify
// every line of it.
func workloadGroups(sv inventory.Service, s *inventory.Snapshot, o Options) ([]kvGroup, []string) {
	now := s.CollectedAt
	var groups []kvGroup
	g := kvGroup{}
	add := func(k, v, cls string) {
		if strings.TrimSpace(v) != "" {
			g.Rows = append(g.Rows, kv{k, v, cls})
		}
	}
	flush := func(title string) {
		if len(g.Rows) > 0 {
			g.Title = title
			groups = append(groups, g)
		}
		g = kvGroup{}
	}
	pid := strconv.Itoa(sv.MainPID)
	u := sv.Systemd

	// RUNS — what it is and how it is kept alive.
	cmd := sv.Cmdline
	if cmd == "" {
		cmd = sv.Exe
	}
	add("Command", cmd, "")
	if sv.Exe != "" && !strings.HasPrefix(cmd, sv.Exe) {
		add("Binary (/proc/"+pid+"/exe)", sv.Exe, "")
	}
	if u != nil {
		if u.Description != "" {
			add("Unit", sv.Unit+" — "+u.Description, "")
		}
		file := u.File
		if len(u.DropIns) > 0 {
			file += " + " + strings.Join(u.DropIns, ", ")
		}
		add("Unit file", file, "")
		pol := strings.TrimSpace(strings.Join(nonEmptyStr("Type="+u.Type, "Restart="+u.Restart), " · "))
		add("Supervision", pol, "")
		if u.WorkingDir != "" {
			add("Working directory", u.WorkingDir, "")
		}
	} else if sv.Kind == "container" {
		add("Container", sv.Name+" ("+strings.TrimPrefix(sv.Key, "container:")+")", "")
	}
	owner := sv.Kind
	if sv.Unit != "" {
		owner += " · " + sv.Unit
	}
	add("Owner", owner+" · user "+sv.User, "")
	if len(sv.Parents) > 0 {
		add("Launched by", strings.Join(sv.Parents, " → ")+" → "+sv.Name+" ("+pid+")", "")
	}
	started := ""
	if !sv.StartedAt.IsZero() {
		started = o.Time(sv.StartedAt) + " · up " + since(now, sv.StartedAt)
	}
	if u != nil && u.Restarts > 0 {
		started += fmt.Sprintf(" · restarted %d× by systemd", u.Restarts)
		add("Running since", started, "warn")
	} else {
		add("Running since", started, "")
	}
	flush("Runs")

	// USES — now, lifetime and limits.
	cpu := Cores(sv.CPUCores) + " now"
	if cg := sv.Cgroup; cg != nil && cg.CPUUsageSec > 0 && !sv.StartedAt.IsZero() {
		if age := now.Sub(sv.StartedAt).Seconds(); age > 60 {
			cpu += fmt.Sprintf(" · %s average since start (%s of CPU time)", Cores(cg.CPUUsageSec/age), Duration(time.Duration(cg.CPUUsageSec*float64(time.Second))))
		}
	}
	add("CPU", cpu, "")
	if len(sv.HotThreads) > 0 {
		var ts []string
		for _, t := range sv.HotThreads {
			ts = append(ts, fmt.Sprintf("%s (tid %d) %s", t.Name, t.TID, Cores(t.Cores)))
		}
		add("Hottest threads (pid "+strconv.Itoa(sv.HotThreadsPID)+")", strings.Join(ts, " · "), "")
	}
	memLabel := "RSS"
	if s.Memory.PSSAccurate {
		memLabel = "PSS"
	}
	mem := memLabel + " " + BytesU(sv.MemBytes)
	cls := ""
	if cg := sv.Cgroup; cg != nil {
		mem += " · cgroup " + BytesU(cg.MemCurrent)
		if cg.MemPeak > 0 {
			mem += " · peak " + BytesU(cg.MemPeak)
		}
		switch {
		case cg.MemMax > 0:
			mem += " · limit " + BytesU(cg.MemMax)
			if float64(cg.MemCurrent) >= 0.9*float64(cg.MemMax) {
				cls = "crit"
			}
		case cg.MemHigh > 0:
			mem += " · throttled above " + BytesU(cg.MemHigh)
		default:
			mem += " · no limit"
		}
	}
	if sv.SwapBytes > 0 {
		mem += " · swap " + BytesU(sv.SwapBytes)
	}
	add("Memory", mem, cls)
	io := "read " + Rate(sv.ReadBPS) + " · write " + Rate(sv.WriteBPS) + " now"
	if cg := sv.Cgroup; cg != nil && cg.IOReadTotal+cg.IOWriteTotal > 0 {
		io += " · " + BytesU(cg.IOReadTotal) + " read / " + BytesU(cg.IOWriteTotal) + " written since start"
	}
	add("Disk I/O", io, "")
	tasks := fmt.Sprintf("%d process(es) · %d thread(s)", sv.Procs, sv.Threads)
	cls = ""
	if cg := sv.Cgroup; cg != nil && cg.PidsMax > 0 {
		tasks += fmt.Sprintf(" · tasks %d of %d (pids.max)", cg.PidsCurrent, cg.PidsMax)
		if float64(cg.PidsCurrent) >= 0.8*float64(cg.PidsMax) {
			cls = "warn"
		}
	}
	add("Processes", tasks, cls)
	if sv.FDs > 0 {
		v := strconv.Itoa(sv.FDs) + " open"
		cls = ""
		if sv.FDLimit > 0 {
			v += " · limit " + strconv.FormatUint(sv.FDLimit, 10) + " per process"
			per := sv.FDs / max(1, sv.Procs)
			if float64(per) >= 0.7*float64(sv.FDLimit) {
				cls = "warn"
			}
		}
		add("Open files", v, cls)
	}
	if u != nil {
		lim := strings.Join(nonEmptyStr(prefix("MemoryMax=", u.MemoryMax), prefix("CPUQuota=", u.CPUQuota), prefix("LimitNOFILE=", u.LimitNOFILE)), " · ")
		add("Unit limits", lim, "")
	}
	flush("Uses")

	// CONNECTS — who depends on it and what it depends on.
	if len(sv.Listen) > 0 {
		var ls []string
		for _, l := range sv.Listen {
			v := l.Proto + " " + hostPort(l.Addr, l.Port)
			if strings.HasPrefix(l.Proto, "tcp") {
				v += fmt.Sprintf(" (%d client%s)", l.Clients, plural(l.Clients))
			}
			ls = append(ls, v)
		}
		add("Accepts on", strings.Join(ls, " · "), "")
	}
	if len(sv.Peers) > 0 {
		var ps []string
		for _, p := range sv.Peers {
			v := hostPort(p.Addr, p.Port)
			if p.Name != "" {
				v = p.Name + " " + v
			}
			ps = append(ps, fmt.Sprintf("%s ×%d", v, p.Conns))
		}
		add("Talks to", strings.Join(ps, " · "), "")
	}
	if u != nil && len(u.DependsOn) > 0 {
		add("Depends on (systemd)", strings.Join(u.DependsOn, " · "), "")
	}
	flush("Connects")

	// PRESSURE — is the kernel making it wait?
	if cg := sv.Cgroup; cg != nil {
		if cg.PSIAvailable {
			cls = ""
			if cg.MemPSISome10 >= 10 || cg.CPUPSISome10 >= 40 || cg.IOPSISome10 >= 40 {
				cls = "crit"
			}
			add("Stalled (cgroup PSI avg10)", fmt.Sprintf("cpu %.1f%% · memory %.1f%% (full %.1f%%) · io %.1f%%", cg.CPUPSISome10, cg.MemPSISome10, cg.MemPSIFull10, cg.IOPSISome10), cls)
		}
		if cg.CPUMaxCores > 0 || cg.ThrottledPct > 0 {
			v := fmt.Sprintf("%.0f%% of periods throttled", cg.ThrottledPct)
			if cg.CPUMaxCores > 0 {
				v += fmt.Sprintf(" · quota %.2f cores (cpu.max)", cg.CPUMaxCores)
			}
			cls = ""
			if cg.ThrottledPct >= 25 {
				cls = "warn"
			}
			add("CPU throttling", v, cls)
		}
		if cg.OOMKills > 0 {
			add("OOM kills in this cgroup", strconv.FormatUint(cg.OOMKills, 10), "crit")
		}
	}
	cls = ""
	if sv.OOMScore >= 500 {
		cls = "crit"
	} else if sv.OOMScore >= 200 {
		cls = "warn"
	}
	oom := strconv.Itoa(sv.OOMScore) + " of 1000"
	if sv.OOMAdj != 0 {
		oom += fmt.Sprintf(" (oom_score_adj %d)", sv.OOMAdj)
	}
	add("OOM killer score", oom, cls)
	flush("Pressure")

	// VERIFY — the same facts from the source.
	var cmds []string
	if sv.Unit != "" && strings.HasSuffix(sv.Unit, ".service") {
		cmds = append(cmds, "systemctl status "+sv.Unit, "journalctl -u "+sv.Unit+" -n 100 --no-pager")
	}
	if sv.Kind == "container" {
		cmds = append(cmds, "docker stats --no-stream "+sv.Name, "docker logs --tail 100 "+sv.Name)
	}
	if sv.CgroupPath != "" {
		cg := path.Join(sysCgroup, sv.CgroupPath)
		cmds = append(cmds, "cat "+cg+"/{memory.current,memory.peak,memory.max,cpu.stat,memory.pressure}")
	}
	if sv.MainPID > 0 {
		cmds = append(cmds, "cat /proc/"+pid+"/status", "ls -l /proc/"+pid+"/fd | wc -l", "ss -tnp | grep 'pid="+pid+",'")
		if len(sv.HotThreads) > 0 {
			cmds = append(cmds, "top -H -p "+strconv.Itoa(sv.HotThreadsPID))
		}
	}
	return groups, cmds
}

func prefix(p, v string) string {
	if v == "" {
		return ""
	}
	return p + v
}

func nonEmptyStr(in ...string) []string {
	var out []string
	for _, s := range in {
		if s != "" && !strings.HasSuffix(s, "=") {
			out = append(out, s)
		}
	}
	return out
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func hostPort(addr string, port int) string {
	if strings.Contains(addr, ":") {
		return "[" + addr + "]:" + strconv.Itoa(port)
	}
	return addr + ":" + strconv.Itoa(port)
}

func listeners(s *inventory.Snapshot) []listenRow {
	var out []listenRow
	for _, l := range s.Listeners {
		r := listenRow{Proto: l.Proto, Addr: l.Addr, Port: strconv.Itoa(l.Port), Service: l.Service}
		if l.PID > 0 {
			r.PID = strconv.Itoa(l.PID)
		}
		if strings.HasPrefix(l.Proto, "tcp") {
			r.Clients = strconv.Itoa(l.Clients)
		}
		if r.Service == "" {
			r.Service = "—"
		}
		switch {
		case !l.Public:
			r.Scope, r.ScopeClass = "local only", "ok"
		case l.Addr == "0.0.0.0" || l.Addr == "::":
			r.Scope, r.ScopeClass = "all interfaces", "warn"
		default:
			r.Scope, r.ScopeClass = "this address", "muted"
		}
		out = append(out, r)
	}
	return out
}

func checks(s *inventory.Snapshot) []checkRow {
	var out []checkRow
	for _, c := range s.Checks {
		r := checkRow{Area: c.Area, Name: c.Name, State: c.State, Value: c.Value, Hint: c.Hint, Cmd: c.Cmd}
		switch c.State {
		case "ok":
			r.Icon = "✓"
		case "warn":
			r.Icon = "!"
		default:
			r.Icon = "i"
		}
		out = append(out, r)
	}
	return out
}

var prioName = []string{"emerg", "alert", "crit", "err", "warn", "notice", "info", "debug"}

func logSections(ex *journal.Excerpt, o Options) []logSection {
	if ex == nil {
		return nil
	}
	var out []logSection
	for _, s := range ex.Sections {
		ls := logSection{Title: s.Title, Cmd: s.Cmd, Note: s.Note}
		for _, l := range s.Lines {
			ll := logLine{At: o.Clock(l.At), Msg: l.Message, Count: l.Count}
			if l.Priority >= 0 && l.Priority < len(prioName) {
				ll.Prio = prioName[l.Priority]
			}
			switch {
			case l.Priority <= 3:
				ll.PrioClass = "crit"
			case l.Priority == 4:
				ll.PrioClass = "warn"
			default:
				ll.PrioClass = "muted"
			}
			who := l.Ident
			if who == "" {
				who = strings.TrimSuffix(l.Unit, ".service")
			}
			if l.PID > 0 {
				who += "[" + strconv.Itoa(l.PID) + "]"
			}
			ll.Who = who
			ls.Lines = append(ls.Lines, ll)
		}
		out = append(out, ls)
	}
	return out
}
