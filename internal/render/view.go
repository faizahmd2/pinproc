package render

import (
	"fmt"
	"html/template"
	"sort"
	"strconv"
	"strings"

	"github.com/faizahmd2/pinproc/internal/contract"
	"github.com/faizahmd2/pinproc/internal/doc"
	"github.com/faizahmd2/pinproc/internal/inventory"
)

// view is everything a template needs, pre-formatted, so templates stay dumb.
type view struct {
	D        *doc.Document
	S        *inventory.Snapshot
	Time     string
	HostIP   string
	Banner   banner
	Pillars  []pillar
	Devices  []devRow
	Services []svcCard
	Timeline []tlRow
	TLNote   string
	Specs    []spec
	Mem      breakdown
	CPU      breakdown
	Disks    []breakdown
	NICs     []nicRow
	NetLine  string
	Procs    []procRow
	Notes    []string
	Footer   string
	Cause    *causeView

	Listeners []listenRow
	Checks    []checkRow
	ChecksOK  int
	ChecksBad int
	Logs      []logSection
	// Which machine sections an alert shows: only the ones for its resource.
	ShowMem, ShowCPU, ShowDisks, ShowNet bool
}

type banner struct{ Badge, Title, Sub, Class string }

type pillar struct {
	Name, State, Class, Stat, Path, Detail string
	Focus                                  bool
}

type devRow struct{ Name, Read, Write, Queue, Util, UtilClass string }

type kv struct {
	K, V  string
	Class string
}

type svcCard struct {
	Rank    int
	Name    string
	Badge   string // the dimension value shown in brackets
	Class   string
	Metrics string
	Cause   bool
	Open    bool
	Groups  []kvGroup
	Cmds    []string
}

type tlRow struct{ At, Armed, Top, CPU, Mem, IO string }

type spec struct {
	Label string
	Vals  []string
	Sub   string
}

type seg struct {
	Class, Title string
	Width        template.CSS
}

type bRow struct{ Class, Label, Detail, Source, Value, Pct string }

type breakdown struct {
	Title, Cmd, Left    string
	Segs                []seg
	Rows                []bRow
	TotalLabel, TotalBy string
	TotalValue, Unit    string
	Mounts              []mountRow
	IO                  string
	More                []kv // expandable detail list
	MoreTitle           string
}

type mountRow struct{ Path, Device, FS, Size, Used, Avail, UsePct, Inode, Class string }

type nicRow struct{ Name, Addr, Link, Rx, Tx, Drops, State string }

type procRow struct {
	PID, User, Service, CPU, Mem, Read, Write, FDs, Cmd string
	State, Threads, Up                                  string
	Gone                                                bool
}

type causeView struct {
	Service, Value string
	Components     []string
	Ports          string
}

const sysCgroup = "/sys/fs/cgroup"

func newView(d *doc.Document, o Options) *view {
	s := d.Snapshot
	if s == nil {
		s = &inventory.Snapshot{}
	}
	v := &view{D: d, S: s, Time: o.Time(d.GeneratedAt), Notes: d.Notes}
	v.HostIP = s.Host.PrimaryIP
	if s.Host.PublicIP != "" {
		v.HostIP = s.Host.PublicIP
	}
	if s.Host.Hostname == "" {
		s.Host.Hostname = "unknown-host"
	}
	v.Banner = makeBanner(d)
	v.Pillars = pillars(s, d.Dimension)
	v.Devices = devices(s)
	v.Services = services(s, d, o)
	v.Timeline, v.TLNote = timeline(d.Incident)
	v.Specs = specs(s)
	v.Mem = memBreakdown(s)
	v.CPU = cpuBreakdown(s)
	v.Disks = diskBreakdowns(s)
	v.NICs, v.NetLine = nics(s)
	v.Procs = procs(s)
	v.Cause = cause(d.Incident)
	v.Listeners = listeners(s)
	v.Checks = checks(s)
	for _, c := range v.Checks {
		if c.State == "warn" {
			v.ChecksBad++
		} else if c.State == "ok" {
			v.ChecksOK++
		}
	}
	v.Logs = logSections(d.Logs, o)
	switch d.Dimension {
	case contract.DimensionMemory:
		v.ShowMem = true
	case contract.DimensionIO, contract.DimensionFilesystem:
		v.ShowDisks = true
	case contract.DimensionNetwork:
		v.ShowNet = true
	default:
		v.ShowCPU = true
	}
	if d.Kind == doc.KindReport {
		v.ShowMem, v.ShowCPU, v.ShowDisks, v.ShowNet = true, true, true, true
	}
	v.Footer = fmt.Sprintf("pinproc · read-only · collected in %.1fs window · %s", s.Window.Seconds(), o.Time(d.GeneratedAt))
	return v
}

func makeBanner(d *doc.Document) banner {
	b := banner{Title: d.Headline, Sub: d.TriggerDetail}
	switch d.Severity {
	case doc.SeverityCritical:
		b.Class, b.Badge = "crit", "CRITICAL"
	case doc.SeverityWarning:
		b.Class, b.Badge = "warn", "WARNING"
	default:
		b.Class, b.Badge = "info", "REPORT"
	}
	if d.Dimension != "" {
		b.Badge += " · " + strings.ToUpper(dimName(d.Dimension))
	}
	if d.Kind == doc.KindAlert && d.Trigger == "manual" {
		b.Badge = "MANUAL CHECK"
		b.Class = "info"
	}
	return b
}

func dimName(dim contract.Dimension) string {
	switch dim {
	case contract.DimensionMemory:
		return "memory"
	case contract.DimensionIO:
		return "disk i/o"
	case contract.DimensionNetwork:
		return "network"
	case contract.DimensionFilesystem:
		return "disk space"
	}
	return string(dim)
}

func state(crit, warn bool, critWord, warnWord string) (string, string) {
	switch {
	case crit:
		return critWord, "crit"
	case warn:
		return warnWord, "warn"
	}
	return "NOMINAL", "ok"
}

func busiestDisk(s *inventory.Snapshot) *inventory.Disk {
	var best *inventory.Disk
	for i := range s.Disks {
		if best == nil || s.Disks[i].UtilPct > best.UtilPct {
			best = &s.Disks[i]
		}
	}
	return best
}

func primaryNIC(s *inventory.Snapshot) *inventory.NIC {
	if len(s.NICs) == 0 {
		return nil
	}
	return &s.NICs[0]
}

func pillars(s *inventory.Snapshot, dim contract.Dimension) []pillar {
	c, m := s.CPU, s.Memory
	busy := float64(c.Cores) * c.BusyPct / 100
	st, cl := state(c.BusyPct >= 90 || c.PSI.SomeAvg10 >= 40, c.BusyPct >= 70 || c.PSI.SomeAvg10 >= 20, "SATURATED", "BUSY")
	out := []pillar{{
		Name: fmt.Sprintf("CPU (%d CORES)", c.Cores), State: st, Class: cl,
		Stat: fmt.Sprintf("%.2f / %d.00 c", busy, c.Cores), Path: "/proc/pressure/cpu",
		Detail: trend(c.PSI), Focus: dim == contract.DimensionCPU || dim == contract.DimensionScheduling,
	}}
	st, cl = state(m.UsedPct >= 90 || m.PSI.SomeAvg10 >= 30 || m.SwapOutBPS > 1<<20, m.UsedPct >= 80 || m.PSI.SomeAvg10 >= 10, "OOM RISK", "TIGHT")
	out = append(out, pillar{
		Name: "MEMORY (" + gibShort(m.TotalBytes) + ")", State: st, Class: cl,
		Stat: fmt.Sprintf("%s / %s GiB", GiB(float64(m.UsedBytes)), GiB(float64(m.TotalBytes))), Path: "/proc/pressure/memory",
		Detail: trend(m.PSI), Focus: dim == contract.DimensionMemory,
	})
	ioPSI := s.IOPSI.SomeAvg10
	if d := busiestDisk(s); d != nil {
		st, cl = state(d.UtilPct >= 90 || ioPSI >= 60, d.UtilPct >= 60 || ioPSI >= 20, "SATURATED", "BUSY")
		out = append(out, pillar{
			Name: "DISK IO (" + strings.ToUpper(shortKind(d.Kind)) + ")", State: st, Class: cl,
			Stat: fmt.Sprintf("%.1f%% util · %.1f ms", d.UtilPct, d.AwaitMS), Path: "/proc/pressure/io",
			Detail: trend(s.IOPSI), Focus: dim == contract.DimensionIO || dim == contract.DimensionFilesystem,
		})
	}
	if n := primaryNIC(s); n != nil {
		st, cl = state(false, n.DropsPS > 0 || s.Net.RetransPerSec >= 50, "", "LOSSY")
		stat := Bits(n.RxBPS + n.TxBPS)
		if n.SpeedMbps > 0 {
			stat += " / " + Link(n.SpeedMbps)
		}
		addr := n.Name
		if len(n.Addrs) > 0 {
			addr += " (" + n.Addrs[0] + ")"
		}
		out = append(out, pillar{
			Name: "NETWORK (" + Link(n.SpeedMbps) + ")", State: st, Class: cl, Stat: stat, Path: addr,
			Detail: "RX " + Bits(n.RxBPS) + " · TX " + Bits(n.TxBPS), Focus: dim == contract.DimensionNetwork,
		})
	}
	return out
}

// trend is the kernel's stall average over 10s, 1m and 5m: rising, steady or easing.
func trend(p inventory.PSI) string {
	if !p.Available {
		return "pressure n/a (kernel without PSI)"
	}
	return fmt.Sprintf("stalled 10s %.1f%% · 1m %.1f%% · 5m %.1f%%", p.SomeAvg10, p.SomeAvg60, p.SomeAvg300)
}

func shortKind(k string) string {
	if i := strings.IndexByte(k, ' '); i > 0 {
		return k[:i]
	}
	return k
}

func gibShort(v uint64) string {
	g := float64(v) / (1 << 30)
	if g >= 10 {
		return fmt.Sprintf("%.0f GiB", g)
	}
	return fmt.Sprintf("%.1f GiB", g)
}

func devices(s *inventory.Snapshot) []devRow {
	var out []devRow
	for _, d := range s.Disks {
		cls := "ok"
		if d.UtilPct >= 90 || d.AwaitMS >= 50 {
			cls = "crit"
		} else if d.UtilPct >= 60 || d.AwaitMS >= 20 {
			cls = "warn"
		}
		out = append(out, devRow{
			Name:  d.Name + " (block)",
			Read:  fmt.Sprintf("%s (%s IOPS)", Rate(d.ReadBPS), Count(d.ReadIOPS)),
			Write: fmt.Sprintf("%s (%s IOPS)", Rate(d.WriteBPS), Count(d.WriteIOPS)),
			Queue: fmt.Sprintf("aqu-sz: %.1f", d.QueueDepth),
			Util:  fmt.Sprintf("%.1fms · %.1f%%", d.AwaitMS, d.UtilPct), UtilClass: cls,
		})
	}
	for _, n := range s.NICs {
		cls := "ok"
		if n.DropsPS > 0 {
			cls = "warn"
		}
		errs := float64(n.RxErrors + n.TxErrors)
		out = append(out, devRow{
			Name:  n.Name + " (NIC)",
			Read:  fmt.Sprintf("%s (%s pps)", Bits(n.RxBPS), Count(n.RxPPS)),
			Write: fmt.Sprintf("%s (%s pps)", Bits(n.TxBPS), Count(n.TxPPS)),
			Queue: fmt.Sprintf("rx_dropped: %d", n.RxDrops),
			Util:  fmt.Sprintf("%s err · retrans %.1f/s", Count(errs), s.Net.RetransPerSec), UtilClass: cls,
		})
	}
	return out
}

// services orders the owners by the alert's dimension and builds their cards.
func services(s *inventory.Snapshot, d *doc.Document, o Options) []svcCard {
	list := make([]inventory.Service, 0, len(s.Services))
	for _, sv := range s.Services {
		if sv.Kind != "kernel" || sv.CPUCores >= 0.05 {
			list = append(list, sv)
		}
	}
	dim := d.Dimension
	val := func(sv inventory.Service) float64 {
		switch dim {
		case contract.DimensionCPU, contract.DimensionScheduling:
			return sv.CPUCores
		case contract.DimensionIO, contract.DimensionFilesystem:
			return sv.ReadBPS + sv.WriteBPS
		}
		return float64(sv.MemBytes)
	}
	sort.SliceStable(list, func(i, j int) bool { return val(list[i]) > val(list[j]) })
	n := 5
	if d.Kind == doc.KindReport {
		n = 8
	}
	if len(list) > n {
		list = list[:n]
	}
	causeKey, causeName := "", ""
	causeSvc := map[string]bool{} // services owning the cause's component PIDs
	if d.Incident != nil && d.Incident.Cause != nil {
		causeKey, causeName = d.Incident.Cause.Key, d.Incident.Cause.Service
		for _, comp := range d.Incident.Cause.Components {
			for _, p := range s.Processes {
				if p.PID == comp.PID {
					causeSvc[p.Service] = true
				}
			}
		}
	}
	var out []svcCard
	for i, sv := range list {
		c := svcCard{Rank: i + 1, Name: sv.Name, Cause: causeKey != "" && (sv.Key == causeKey || sv.Name == causeName || causeSvc[sv.Name])}
		if sv.Unit != "" && sv.Kind == "systemd" {
			c.Name = sv.Unit
		}
		var share float64
		switch dim {
		case contract.DimensionCPU, contract.DimensionScheduling:
			c.Badge = "CPU: " + Cores(sv.CPUCores)
			share = sv.CPUCores / float64(max(1, s.CPU.Cores))
		case contract.DimensionIO, contract.DimensionFilesystem:
			c.Badge = "IO: " + Rate(sv.ReadBPS+sv.WriteBPS)
			if d := busiestDisk(s); d != nil && d.ReadBPS+d.WriteBPS > 0 {
				share = (sv.ReadBPS + sv.WriteBPS) / (d.ReadBPS + d.WriteBPS) * 0.6
			}
		default:
			label := "RSS"
			if s.Memory.PSSAccurate {
				label = "PSS"
			}
			c.Badge = label + ": " + BytesU(sv.MemBytes)
			share = float64(sv.MemBytes) / float64(max(1, s.Memory.TotalBytes))
		}
		if d.Kind == doc.KindAlert {
			switch {
			case share >= 0.4 || c.Cause:
				c.Class = "crit"
			case share >= 0.15:
				c.Class = "warn"
			}
		}
		c.Metrics = fmt.Sprintf("CPU %s · MEM %s · IO %s · PID %d", Cores(sv.CPUCores), BytesU(sv.MemBytes), Rate(sv.ReadBPS+sv.WriteBPS), sv.MainPID)
		if n := len(sv.Listen); n > 0 {
			cl := 0
			for _, l := range sv.Listen {
				cl += l.Clients
			}
			c.Metrics += fmt.Sprintf(" · %d client%s", cl, plural(cl))
		}
		if sv.Systemd != nil && sv.Systemd.Restarts > 0 {
			c.Metrics += fmt.Sprintf(" · ↻%d", sv.Systemd.Restarts)
		}
		c.Groups, c.Cmds = workloadGroups(sv, s, o)
		c.Open = c.Cause || (d.Kind == doc.KindReport && i == 0)
		out = append(out, c)
	}
	return out
}

func timeline(inc *contract.Incident) ([]tlRow, string) {
	if inc == nil || len(inc.Timeline) == 0 {
		return nil, ""
	}
	pts := inc.Timeline
	if len(pts) > 12 { // keep the shape: first, evenly spaced middle, last
		step := float64(len(pts)-1) / 11
		var keep []contract.IncidentPoint
		for i := 0; i < 12; i++ {
			keep = append(keep, pts[int(float64(i)*step+0.5)])
		}
		pts = keep
	}
	var out []tlRow
	for _, p := range pts {
		top := "—"
		if len(p.Top) > 0 {
			t := p.Top[0]
			top = fmt.Sprintf("%s (pid %d) %s", t.Comm, t.PID, contenderValue(t))
		}
		out = append(out, tlRow{
			At: fmt.Sprintf("+%ds", int(p.At.Sub(inc.StartedAt).Seconds())), Armed: Pct0(p.ArmedLevel),
			Top: top, CPU: Pct0(p.CPUUtil), Mem: Pct0(p.MemUsed), IO: fmt.Sprintf("%.1f", p.IOPSI),
		})
	}
	note := fmt.Sprintf("Built up over %s before capture (%s).", Duration(inc.FiredAt.Sub(inc.StartedAt)), inc.Reason)
	if inc.FiredAt.Sub(inc.StartedAt).Minutes() < 1 {
		note = fmt.Sprintf("Built up over %.0fs before capture (%s).", inc.FiredAt.Sub(inc.StartedAt).Seconds(), inc.Reason)
	}
	return out, note
}

func contenderValue(c contract.IncidentContender) string {
	switch c.Unit {
	case "percent":
		return Pct0(c.Value)
	case "bytes":
		return Bytes(c.Value)
	case "bytes_per_sec":
		return Rate(c.Value)
	}
	return fmt.Sprintf("%.0f", c.Value)
}

func cause(inc *contract.Incident) *causeView {
	if inc == nil || inc.Cause == nil {
		return nil
	}
	c := inc.Cause
	cv := &causeView{Service: c.Service, Value: causeValue(c.Value, c.Unit)}
	for _, comp := range c.Components {
		name := comp.Comm
		if name == "" {
			name = "process"
		}
		cv.Components = append(cv.Components, fmt.Sprintf("%s (pid %d) %s · %.0f%% of service", name, comp.PID, causeValue(comp.Value, c.Unit), comp.Pct))
	}
	var ps []string
	for _, p := range c.Ports {
		ps = append(ps, fmt.Sprintf("%s/%d", p.Proto, p.Port))
	}
	cv.Ports = strings.Join(ps, ", ")
	return cv
}

func causeValue(v float64, unit string) string {
	switch unit {
	case "bytes":
		return Bytes(v)
	case "bytes_per_sec":
		return Rate(v)
	case "connections":
		return fmt.Sprintf("%.0f connections", v)
	}
	return fmt.Sprintf("%.0f%% CPU (%.2f cores)", v, v/100)
}

func specs(s *inventory.Snapshot) []spec {
	h := s.Host
	osName := h.OS
	if osName == "" {
		osName = "Linux"
	}
	osSub := strings.Join(nonEmptyStr(prefix("kernel ", h.Kernel), h.Arch), " · ")
	cpuSub := h.CPUModel
	if h.CPUMHz > 0 {
		cpuSub = strings.TrimSpace(fmt.Sprintf("%s @ %.2f GHz", h.CPUModel, h.CPUMHz/1000))
	}
	if cpuSub == "" {
		cpuSub = "/proc/cpuinfo"
	}
	// Every card has the same shape: one value, then a muted detail line.
	out := []spec{
		{Label: "OPERATING SYSTEM", Vals: []string{osName}, Sub: osSub},
		{Label: "CPU", Vals: []string{fmt.Sprintf("%d vCPU · %.0f%% busy", s.CPU.Cores, s.CPU.BusyPct)}, Sub: cpuSub},
	}
	m := s.Memory
	ram := spec{Label: "PHYSICAL RAM", Sub: fmt.Sprintf("MemTotal: %s MiB · %s available · /proc/meminfo", thousands(float64(m.TotalBytes)/(1<<20), 0), BytesU(m.AvailableBytes))}
	if m.PhysicalBytes > 0 {
		ram.Vals = []string{fmt.Sprintf("%s GiB (%s MiB)", GiB(float64(m.PhysicalBytes)), thousands(float64(m.PhysicalBytes)/(1<<20), 0))}
	} else {
		ram.Vals = []string{fmt.Sprintf("%s GiB (%s MiB)", GiB(float64(m.TotalBytes)), thousands(float64(m.TotalBytes)/(1<<20), 0))}
	}
	out = append(out, ram)
	if len(s.Disks) > 0 {
		var total uint64
		var names []string
		for _, d := range s.Disks {
			total += d.SizeBytes
			names = append(names, "/dev/"+d.Name)
		}
		kind := shortKind(s.Disks[0].Kind)
		if len(s.Disks) > 1 {
			kind = fmt.Sprintf("· %d disks", len(s.Disks))
		}
		out = append(out, spec{Label: "BLOCK STORAGE", Vals: []string{GiB(float64(total)) + " GiB " + kind}, Sub: strings.Join(names, ", ") + " · lsblk -b"})
	}
	if n := primaryNIC(s); n != nil {
		val := "link speed n/a (virtual NIC)"
		if n.SpeedMbps > 0 {
			val = thousands(float64(n.SpeedMbps), 0) + " Mb/s"
			if n.Duplex != "" {
				val += " " + strings.ToUpper(n.Duplex[:1]) + n.Duplex[1:]
			}
		}
		out = append(out, spec{Label: "NIC LINK CAPACITY", Vals: []string{val}, Sub: n.Name + " · ethtool " + n.Name})
	}
	plat := strings.TrimSpace(h.Vendor + " " + h.Product)
	if plat == "" {
		plat = "platform n/a"
	}
	out = append(out, spec{Label: "UPTIME", Vals: []string{Duration(h.Uptime)}, Sub: plat})
	return out
}

var segClass = map[string]string{
	"apps": "seg-1", "shmem": "seg-5", "cache": "seg-2", "kernel": "seg-3", "huge": "seg-6",
	"other": "seg-free", "free": "seg-4", "firmware": "seg-free",
	"user": "seg-1", "wait": "seg-2", "idle": "seg-4",
	"swap": "seg-2", "reserved": "seg-3", "unmounted": "seg-free", "unpartitioned": "seg-free",
}

func bucketsToBreakdown(b *breakdown, buckets []inventory.Bucket, total float64, value func(float64) string, part func(float64) string) {
	usedIdx := 0
	for _, bk := range buckets {
		cls := segClass[bk.Key]
		if strings.HasPrefix(bk.Key, "used:") {
			cls = []string{"seg-1", "seg-5", "seg-6", "seg-2"}[usedIdx%4]
			usedIdx++
		}
		p := share(bk.Value, total)
		if p >= 0.05 {
			b.Segs = append(b.Segs, seg{Class: cls, Title: bk.Label, Width: template.CSS(fmt.Sprintf("width:%.2f%%", p))})
		}
		detail := ""
		if len(bk.Parts) > 0 {
			var ps []string
			for _, pt := range bk.Parts {
				ps = append(ps, fmt.Sprintf("%s (%s)", pt.Name, part(pt.Value)))
			}
			detail = strings.Join(ps, ", ")
		}
		b.Rows = append(b.Rows, bRow{Class: cls, Label: bk.Label, Detail: detail, Source: bk.Source, Value: value(bk.Value), Pct: Pct(p)})
	}
}

func memBreakdown(s *inventory.Snapshot) breakdown {
	m := s.Memory
	total := float64(m.AccountedBytes)
	b := breakdown{
		Title: fmt.Sprintf("Memory Zero-Sum Breakdown (%s GiB)", GiB(total)),
		Cmd:   "cat /proc/meminfo && ps -eo rss,comm", Unit: "Size (MiB)",
		Left:       fmt.Sprintf("%s available to applications now · swap %s of %s used", BytesU(m.AvailableBytes), BytesU(m.SwapUsed), BytesU(m.SwapTotal)),
		TotalLabel: "TOTAL PROVISIONED RAM", TotalValue: MiB(total),
	}
	if m.PhysicalBytes > 0 {
		b.TotalBy = "Exact hardware match (online memory blocks)"
	} else {
		b.TotalBy = "MemTotal (kernel-usable RAM)"
	}
	bucketsToBreakdown(&b, m.Buckets, total, MiB, func(v float64) string { return thousands(v/(1<<20), 0) })
	b.MoreTitle = "Expand full process memory list"
	label := "RSS"
	if m.PSSAccurate {
		label = "PSS"
	}
	ps := append([]inventory.Process(nil), s.Processes...)
	sort.Slice(ps, func(i, j int) bool { return memOf(ps[i]) > memOf(ps[j]) })
	for i, p := range ps {
		if i >= 12 {
			break
		}
		b.More = append(b.More, kv{K: fmt.Sprintf("PID %d · %s", p.PID, procCmd(p, 90)), V: label + " " + BytesU(memOf(p))})
	}
	return b
}

func memOf(p inventory.Process) uint64 {
	if p.PSSBytes > 0 {
		return p.PSSBytes
	}
	return p.RSSBytes
}

func procCmd(p inventory.Process, n int) string {
	c := p.Cmdline
	if c == "" {
		c = p.Exe
	}
	if c == "" {
		c = "[" + p.Comm + "]"
	}
	if p.Exe != "" && p.Cmdline != "" && !strings.HasPrefix(p.Cmdline, "/") {
		// show the real program path when argv[0] is a bare name
		if i := strings.IndexByte(c, ' '); i > 0 {
			c = p.Exe + c[i:]
		} else {
			c = p.Exe
		}
	}
	if len(c) > n {
		c = c[:n-1] + "…"
	}
	return c
}

func cpuBreakdown(s *inventory.Snapshot) breakdown {
	c := s.CPU
	total := float64(c.Cores)
	by := fmt.Sprintf("%d logical cores", c.Cores)
	if s.Host.CPUMHz > 0 {
		by += fmt.Sprintf(" @ %.2f GHz", s.Host.CPUMHz/1000)
	}
	b := breakdown{
		Title: fmt.Sprintf("CPU Time & Scheduler Capacity (%d.00 Cores)", c.Cores),
		Cmd:   "mpstat -P ALL 1 1 && cat /proc/stat", Unit: "Cores Used",
		Left:       fmt.Sprintf("%.2f cores idle · load %.2f, %.2f, %.2f (1/5/15m) · %s context switches/s", float64(c.Cores)*c.IdlePct/100, c.Load1, c.Load5, c.Load15, Count(c.CtxPerSec)),
		TotalLabel: "TOTAL CPU CAPACITY", TotalBy: by, TotalValue: fmt.Sprintf("%.2f", total),
	}
	bucketsToBreakdown(&b, c.Buckets, total, func(v float64) string { return fmt.Sprintf("%.2f", v) }, Cores)
	for i := range b.Rows {
		if b.Rows[i].Label == "Idle headroom" {
			b.Rows[i].Detail = fmt.Sprintf("Available compute headroom (load avg: %.2f, %.2f, %.2f)", c.Load1, c.Load5, c.Load15)
		}
	}
	b.MoreTitle = "Expand top CPU processes"
	ps := append([]inventory.Process(nil), s.Processes...)
	sort.Slice(ps, func(i, j int) bool { return ps[i].CPUCores > ps[j].CPUCores })
	for i, p := range ps {
		if i >= 10 || p.CPUCores < 0.005 {
			break
		}
		b.More = append(b.More, kv{K: fmt.Sprintf("PID %d · %s", p.PID, procCmd(p, 90)), V: fmt.Sprintf("%s (usr %.2f · sys %.2f)", Cores(p.CPUCores), p.UserCores, p.SysCores)})
	}
	return b
}

func diskBreakdowns(s *inventory.Snapshot) []breakdown {
	var out []breakdown
	for _, d := range s.Disks {
		total := float64(d.SizeBytes)
		title := fmt.Sprintf("Block Storage & Partition Accounting · /dev/%s (%s GiB %s)", d.Name, GiB(total), d.Kind)
		var free uint64
		for _, m := range d.Mounts {
			free += m.AvailBytes
		}
		b := breakdown{
			Title: title, Cmd: "lsblk -b && df -B1", Unit: "Size (GiB)",
			Left:       fmt.Sprintf("%s free across %d mounted filesystem(s)", BytesU(free), len(d.Mounts)),
			TotalLabel: "TOTAL BLOCK DEVICE", TotalValue: GiB(total),
			TotalBy: strings.TrimSpace(fmt.Sprintf("/dev/%s %s (%dB logical sector)", d.Name, d.Model, d.SectorBytes)),
			IO:      fmt.Sprintf("I/O now: read %s (%s IOPS) · write %s (%s IOPS) · %.1f%% busy · %.1f ms await · queue %.1f", Rate(d.ReadBPS), Count(d.ReadIOPS), Rate(d.WriteBPS), Count(d.WriteIOPS), d.UtilPct, d.AwaitMS, d.QueueDepth),
		}
		bucketsToBreakdown(&b, d.Buckets, total, GiB, func(v float64) string { return Bytes(v) })
		for _, m := range d.Mounts {
			cls := ""
			if m.UsedPct >= 90 {
				cls = "crit"
			} else if m.UsedPct >= 75 {
				cls = "warn"
			}
			b.Mounts = append(b.Mounts, mountRow{Path: m.Path, Device: m.Device, FS: m.FSType, Size: BytesU(m.TotalBytes), Used: BytesU(m.UsedBytes), Avail: BytesU(m.AvailBytes), UsePct: Pct0(m.UsedPct), Inode: Pct0(m.InodePct), Class: cls})
		}
		out = append(out, b)
	}
	return out
}

func nics(s *inventory.Snapshot) ([]nicRow, string) {
	var out []nicRow
	for _, n := range s.NICs {
		out = append(out, nicRow{
			Name: n.Name, Addr: strings.Join(n.Addrs, ", "), Link: Link(n.SpeedMbps), State: n.State,
			Rx: Bits(n.RxBPS), Tx: Bits(n.TxBPS), Drops: fmt.Sprintf("%d / %d", n.RxDrops, n.TxDrops),
		})
	}
	nt := s.Net
	fdMax := thousands(float64(nt.FDMax), 0)
	if nt.FDMax >= 1<<62 {
		fdMax = "no limit"
	}
	line := fmt.Sprintf("TCP: %d active · %d TIME_WAIT · %d orphaned · %.1f retransmits/s · %d listen-queue overflows since boot · open files %s of %s",
		nt.TCPInUse, nt.TCPTimeWait, nt.TCPOrphan, nt.RetransPerSec, nt.ListenOverflows, thousands(float64(nt.FDUsed), 0), fdMax)
	return out, line
}

func procs(s *inventory.Snapshot) []procRow {
	var out []procRow
	for _, p := range s.Processes {
		fds := ""
		if p.FDs > 0 {
			fds = strconv.Itoa(p.FDs)
			if p.FDLimit > 0 {
				fds += " / " + strconv.FormatUint(p.FDLimit, 10)
			}
		}
		out = append(out, procRow{
			PID: strconv.Itoa(p.PID), User: p.User, Service: p.Service, CPU: Cores(p.CPUCores), Mem: BytesU(memOf(p)),
			Read: Rate(p.ReadBPS), Write: Rate(p.WriteBPS), FDs: fds, Cmd: procCmd(p, 110), Gone: p.ExeGone,
			State: p.State, Threads: strconv.Itoa(p.Threads), Up: since(s.CollectedAt, p.StartedAt),
		})
	}
	return out
}
