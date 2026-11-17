package inventory

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/faizahmd2/pinproc/internal/identity"
	"github.com/faizahmd2/pinproc/internal/procfs"
)

// Options bounds what a collection reads. The defaults are tuned for a one-shot
// report; the live terminal view turns the expensive parts off.
type Options struct {
	Proc   string        // default /proc
	Sys    string        // default /sys
	Etc    string        // default /etc
	Window time.Duration // gap between the two samples (default 1s)

	TopProcs  int  // processes kept in Snapshot.Processes (default 15; <0 = all)
	ReadSmaps bool // PSS from smaps_rollup for the heaviest services/processes
	Enrich    bool // exe, cmdline, fds, oom, ports for top processes/services

	Ports      bool          // sockets per service: listeners, clients, outbound peers (walks fds)
	AllSockets bool          // attribute every listener on the host, not only the top services
	Units      bool          // ask systemd about each service unit (systemctl show, 2s bound)
	Threads    bool          // short extra window measuring the busiest processes' threads
	Checks     bool          // host runtime checks (swap, limits, clock, journal, ssh, ...)
	ScanDirs   bool          // bounded largest-directory scan per mount (report only)
	ScanBudget time.Duration // total wall time for ScanDirs (default 4s)
}

func (o *Options) normalize() {
	if o.Proc == "" {
		o.Proc = "/proc"
	}
	if o.Sys == "" {
		o.Sys = "/sys"
	}
	if o.Etc == "" {
		o.Etc = "/etc"
	}
	if o.Window <= 0 {
		o.Window = time.Second
	}
	if o.TopProcs == 0 {
		o.TopProcs = 15
	}
	if o.ScanBudget <= 0 {
		o.ScanBudget = 4 * time.Second
	}
}

// ReportOptions is the full, detailed collection used by `pinproc report`.
func ReportOptions() Options {
	return Options{TopProcs: 15, ReadSmaps: true, Enrich: true, Ports: true, AllSockets: true, Units: true, Threads: true, Checks: true, ScanDirs: true}
}

// AlertOptions is the collection attached to a captured incident: the workloads
// in full, without the host checks and the directory scan, so a capture stays
// fast while the machine is struggling.
func AlertOptions() Options {
	return Options{TopProcs: 15, ReadSmaps: true, Enrich: true, Ports: true, Units: true, Threads: true}
}

// TerminalOptions is the one-shot collection behind bare `pinproc`.
func TerminalOptions() Options {
	return Options{TopProcs: 10, Enrich: true, Ports: true, AllSockets: true, Units: true, Checks: true}
}

// Collector produces Snapshots. It keeps the previous sample so a repeat caller can
// compute rates from one read pass per refresh instead of sleeping each time.
type Collector struct {
	opt     Options
	prev    *sample
	users   map[uint64]string
	static  *Host
	boot    int64 // boot time (unix seconds) of the current build
	pssSeen bool  // whether smaps_rollup was readable in the current build
}

// New returns a collector.
func New(opt Options) *Collector {
	opt.normalize()
	return &Collector{opt: opt}
}

// Collect returns a snapshot. Rates are measured against the previous sample when
// one is recent; otherwise it samples, waits Window, and samples again.
func (c *Collector) Collect(ctx context.Context) (*Snapshot, error) {
	if c.users == nil {
		c.users = passwd(filepath.Join(c.opt.Etc, "passwd"))
	}
	if c.prev == nil || time.Since(c.prev.at) > 30*time.Second {
		c.prev = c.take(nil)
		t := time.NewTimer(c.opt.Window)
		select {
		case <-ctx.Done():
			t.Stop()
			return nil, ctx.Err()
		case <-t.C:
		}
	}
	cur := c.take(c.prev)
	snap := c.build(ctx, c.prev, cur)
	c.prev = cur
	return snap, nil
}

// sample is the set of cumulative counters read at one instant.
type sample struct {
	at      time.Time
	stat    procfs.Stat
	disks   map[string]procfs.DiskStat
	nets    map[string]procfs.NetDev
	vm      map[string]uint64
	retrans uint64
	procs   map[int]*procSample
	cgThrot map[string][2]uint64 // cgroup path -> nr_periods, nr_throttled
}

func (c *Collector) take(prev *sample) *sample {
	s := &sample{at: time.Now()}
	s.stat, _ = procfs.ParseStat(readFile(filepath.Join(c.opt.Proc, "stat")))
	s.disks = map[string]procfs.DiskStat{}
	if ds, err := procfs.ParseDiskStats(readFile(filepath.Join(c.opt.Proc, "diskstats"))); err == nil {
		for _, d := range ds {
			s.disks[d.Name] = d
		}
	}
	s.nets = map[string]procfs.NetDev{}
	if ns, err := procfs.ParseNetDev(readFile(filepath.Join(c.opt.Proc, "net/dev"))); err == nil {
		for _, n := range ns {
			s.nets[n.Name] = n
		}
	}
	s.vm = kvFields(readFile(filepath.Join(c.opt.Proc, "vmstat")))
	if t, err := procfs.ParseNetSNMP(readFile(filepath.Join(c.opt.Proc, "net/snmp"))); err == nil {
		s.retrans = t.RetransSegs
	}
	s.procs = c.scanProcs(prev)
	s.cgThrot = c.readThrottle(s.procs)
	return s
}

func (c *Collector) build(ctx context.Context, a, b *sample) *Snapshot {
	secs := b.at.Sub(a.at).Seconds()
	snap := &Snapshot{Schema: Schema, CollectedAt: b.at.UTC(), Window: b.at.Sub(a.at)}
	c.boot = b.stat.BootTime
	snap.Host = c.host(b)
	snap.CPU = c.cpu(a, b)
	snap.Net = c.netSummary(a, b, secs)
	snap.NICs = c.nics(a, b, secs)
	procs, services, tasks, listeners := c.processes(ctx, a, b, secs)
	snap.Tasks = tasks
	snap.Listeners = listeners
	snap.Memory = c.memory(a, b, secs, services)
	snap.Disks = c.disks(a, b, secs, &snap.Notes)
	snap.IOPSI = readPSI(filepath.Join(c.opt.Proc, "pressure/io"))
	cpuParts(&snap.CPU, services)
	if c.opt.Threads && ctx.Err() == nil {
		c.hotThreads(ctx, services, procs)
	}
	snap.Services = services
	snap.Processes = procs
	if c.opt.Checks && ctx.Err() == nil {
		snap.Checks = c.checks(ctx, snap)
	}
	if !snap.Memory.PSSAccurate && c.opt.ReadSmaps {
		snap.Notes = append(snap.Notes, "per-process PSS unavailable (smaps_rollup unreadable) — memory per service uses RSS; run as root for exact shares")
	}
	if os.Geteuid() != 0 && !hasPrivilegedReads(c.opt.Proc) {
		snap.Notes = append(snap.Notes, "running unprivileged: other users' disk I/O, open files and program paths may be missing — use sudo")
	}
	snap.Notes = uniqueStrings(snap.Notes)
	return snap
}

// hasPrivilegedReads reports whether /proc/1/io is readable (root or the service's
// CAP_DAC_READ_SEARCH), i.e. whether other users' process files are visible.
func hasPrivilegedReads(proc string) bool {
	_, err := os.ReadFile(filepath.Join(proc, "1/io"))
	return err == nil
}

func (c *Collector) host(b *sample) Host {
	if c.static == nil {
		h := Host{Arch: unameArch(runtime.GOARCH)}
		h.Hostname, _ = os.Hostname()
		h.OS = osRelease(c.opt.Etc)
		h.Kernel = readString(filepath.Join(c.opt.Proc, "sys/kernel/osrelease"))
		h.CPUModel, h.CPUMHz = cpuModel(c.opt.Proc, c.opt.Sys)
		h.Vendor = readString(filepath.Join(c.opt.Sys, "class/dmi/id/sys_vendor"))
		h.Product = readString(filepath.Join(c.opt.Sys, "class/dmi/id/product_name"))
		if c.opt.Proc == "/proc" {
			h.PublicIP, h.PrimaryIP = identity.InterfaceIPs()
		}
		c.static = &h
	}
	h := *c.static
	if b.stat.BootTime > 0 {
		h.BootTime = time.Unix(b.stat.BootTime, 0).UTC()
	}
	if f := strings.Fields(readString(filepath.Join(c.opt.Proc, "uptime"))); len(f) > 0 {
		sec, _ := strconv.ParseFloat(f[0], 64)
		h.Uptime = time.Duration(sec * float64(time.Second))
	}
	return h
}

func unameArch(goarch string) string {
	switch goarch {
	case "amd64":
		return "x86_64"
	case "arm64":
		return "aarch64"
	case "386":
		return "i686"
	}
	return goarch
}

func cpuModel(proc, sys string) (string, float64) {
	var model string
	var mhz float64
	for _, line := range strings.Split(string(readFile(filepath.Join(proc, "cpuinfo"))), "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch k {
		case "model name", "Model", "cpu model":
			if model == "" {
				model = v
			}
		case "cpu MHz":
			if mhz == 0 {
				mhz, _ = strconv.ParseFloat(v, 64)
			}
		}
	}
	if mhz == 0 {
		if khz := readUint(filepath.Join(sys, "devices/system/cpu/cpu0/cpufreq/cpuinfo_max_freq")); khz > 0 {
			mhz = float64(khz) / 1000
		}
	}
	return strings.Join(strings.Fields(model), " "), mhz
}

func readPSI(path string) PSI {
	b := readFile(path)
	if len(b) == 0 {
		return PSI{}
	}
	p, err := procfs.ParsePressure(b)
	if err != nil {
		return PSI{}
	}
	return PSI{SomeAvg10: p.Some.Avg10, SomeAvg60: p.Some.Avg60, SomeAvg300: p.Some.Avg300, FullAvg10: p.Full.Avg10, FullAvg60: p.Full.Avg60, Available: true}
}

func (c *Collector) cpu(a, b *sample) CPU {
	out := CPU{Cores: len(b.stat.PerCore)}
	if out.Cores == 0 {
		out.Cores = runtime.NumCPU()
	}
	d := func(x, y uint64) float64 { return float64(sub(x, y)) }
	ca, cb := a.stat.CPU, b.stat.CPU
	user := d(cb.User, ca.User) + d(cb.Nice, ca.Nice)
	sys := d(cb.System, ca.System)
	irq := d(cb.IRQ, ca.IRQ) + d(cb.SoftIRQ, ca.SoftIRQ)
	iow := d(cb.IOWait, ca.IOWait)
	steal := d(cb.Steal, ca.Steal)
	idle := d(cb.Idle, ca.Idle)
	total := user + sys + irq + iow + steal + idle
	if total > 0 {
		out.UserPct, out.SystemPct, out.IRQPct = pct(user, total), pct(sys, total), pct(irq, total)
		out.IOWaitPct, out.StealPct, out.IdlePct = pct(iow, total), pct(steal, total), pct(idle, total)
		out.BusyPct = 100 - out.IdlePct - out.IOWaitPct
	}
	for i := range b.stat.PerCore {
		if i >= len(a.stat.PerCore) {
			break
		}
		x, y := a.stat.PerCore[i], b.stat.PerCore[i]
		busy := d(y.User, x.User) + d(y.Nice, x.Nice) + d(y.System, x.System) + d(y.IRQ, x.IRQ) + d(y.SoftIRQ, x.SoftIRQ) + d(y.Steal, x.Steal)
		all := busy + d(y.Idle, x.Idle) + d(y.IOWait, x.IOWait)
		out.PerCoreBusy = append(out.PerCoreBusy, pct(busy, all))
	}
	if la, err := procfs.ParseLoadAvg(readFile(filepath.Join(c.opt.Proc, "loadavg"))); err == nil {
		out.Load1, out.Load5, out.Load15 = la.One, la.Five, la.Fifteen
	}
	out.CtxPerSec = rate(b.stat.Ctxt, a.stat.Ctxt, b.at.Sub(a.at).Seconds())
	out.PSI = readPSI(filepath.Join(c.opt.Proc, "pressure/cpu"))
	cores := float64(out.Cores)
	out.Buckets = []Bucket{
		{Key: "user", Label: "Applications (user + nice)", Source: "/proc/stat user+nice", Value: cores * out.UserPct / 100},
		{Key: "kernel", Label: "Kernel (system + interrupts)", Source: "/proc/stat system+irq+softirq", Value: cores * (out.SystemPct + out.IRQPct) / 100},
		{Key: "wait", Label: "Waiting (I/O wait + steal)", Source: "/proc/stat iowait+steal", Value: cores * (out.IOWaitPct + out.StealPct) / 100},
		{Key: "idle", Label: "Idle headroom", Source: "/proc/stat idle", Value: cores * out.IdlePct / 100},
	}
	return out
}

// cpuParts names the services inside the user and kernel CPU buckets.
func cpuParts(cpu *CPU, services []Service) {
	for i := range cpu.Buckets {
		b := &cpu.Buckets[i]
		switch b.Key {
		case "user", "kernel":
			var parts []Share
			for _, s := range services {
				v := s.userCores
				if b.Key == "kernel" {
					v = s.sysCores
				}
				if v >= 0.01 {
					parts = append(parts, Share{Name: s.Name, Value: v})
				}
			}
			b.Parts = topShares(parts, 4, b.Value, "other")
		case "wait":
			b.Parts = []Share{{Name: "iowait", Value: float64(cpu.Cores) * cpu.IOWaitPct / 100}, {Name: "hypervisor steal", Value: float64(cpu.Cores) * cpu.StealPct / 100}}
		}
	}
}

// topShares keeps the n largest parts and adds a remainder so parts never exceed
// (and, when total > 0, exactly fill) the bucket.
func topShares(parts []Share, n int, total float64, otherLabel string) []Share {
	sortShares(parts)
	if len(parts) > n {
		parts = parts[:n]
	}
	sum := 0.0
	for _, p := range parts {
		sum += p.Value
	}
	if total > 0 && sum > total {
		scale := total / sum
		for i := range parts {
			parts[i].Value *= scale
		}
		sum = total
	}
	if total > 0 && total-sum > total*0.005 && otherLabel != "" {
		parts = append(parts, Share{Name: otherLabel, Value: total - sum})
	}
	return parts
}

func (c *Collector) netSummary(a, b *sample, secs float64) NetSummary {
	var n NetSummary
	if ss, err := procfs.ParseSockStat(readFile(filepath.Join(c.opt.Proc, "net/sockstat"))); err == nil {
		n.TCPInUse, n.TCPTimeWait, n.TCPOrphan = ss.TCPInUse, ss.TCPTimeWait, ss.TCPOrphan
	}
	if ext, err := procfs.ParseNetStat(readFile(filepath.Join(c.opt.Proc, "net/netstat"))); err == nil {
		n.ListenOverflows = ext.ListenOverflows
	}
	n.RetransPerSec = rate(b.retrans, a.retrans, secs)
	if f := strings.Fields(readString(filepath.Join(c.opt.Proc, "sys/fs/file-nr"))); len(f) == 3 {
		n.FDUsed, _ = strconv.ParseUint(f[0], 10, 64)
		n.FDMax, _ = strconv.ParseUint(f[2], 10, 64)
	}
	return n
}
