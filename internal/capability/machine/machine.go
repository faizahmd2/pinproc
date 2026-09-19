package machine

import (
	"github.com/faizahmd2/diagnos/internal/capability/spec"
	"github.com/faizahmd2/diagnos/internal/contract"
	"github.com/faizahmd2/diagnos/internal/procfs"
	"github.com/faizahmd2/diagnos/internal/source"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// CPUFacts contains machine CPU evidence.
type CPUFacts struct {
	Utilization, UserPct, SystemPct, IOWaitPct, IRQPct, SoftIRQPct, StealPct, PerCoreMax, CoreImbalance, LoadOnePerCore, PSISome, PSIFull float64
	Runnable, Blocked                                                                                                                     uint64
}

// MemoryFacts contains machine memory evidence.
type MemoryFacts struct{ AvailablePct, SwapUsedPct, SwapinRate, DirectReclaimRate, MajorFaultRate, OOMKillDelta, PSIFull, WorkingsetRefaultRate float64 }

// IODeviceFacts contains one device's IO evidence.
type IODeviceFacts struct {
	Name                                      string
	ReadBPS, WriteBPS, IOPS, UtilPct, AwaitMS float64
	Inflight                                  uint64
}

// IOFacts contains device IO evidence.
type IOFacts struct {
	Devices []IODeviceFacts
	PSIFull float64
}

// NetworkFacts contains interface network evidence.
type NetworkFacts struct{ RxBPS, TxBPS, RxDropRate float64 }

// LimitsFacts contains global limit evidence.
type LimitsFacts struct {
	FDUsedPct, PIDUsedPct          float64
	FDUsed, FDMax, PIDUsed, PIDMax uint64
}

// ProcessRow is one bounded attribution row.
type ProcessRow struct {
	PID        int64
	PPID       int64
	State      string
	RSS        uint64
	CPUPercent float64
	IOBPS      float64
	Threads    int64
	Starttime  uint64
	UID        uint64
	Comm       string
}

// ProcessFacts contains top process rows and state counters.
type ProcessFacts struct {
	Total, DState, ZState, ThreadTotal uint64
	Rows                               []ProcessRow
}

// CPU returns the machine CPU capability.
func CPU() spec.Capability {
	return spec.Capability{ID: "machine.cpu", Dimension: contract.DimensionCPU, Level: contract.L1Machine, Kind: contract.KindSampled, Accepts: contract.EntityMachine, Cost: contract.CostLow, Summary: "machine CPU utilization, pressure, load and per-core balance", LeadsTo: []string{"machine.processes", "machine.io"}, Reads: func(contract.Entity, contract.Facts) []source.Read {
		return []source.Read{{Key: "proc.stat", Path: "/proc/stat", Kind: source.ReadFile}, {Key: "proc.loadavg", Path: "/proc/loadavg", Kind: source.ReadFile}, {Key: "proc.cpu_pressure", Path: "/proc/pressure/cpu", Kind: source.ReadFile, Optional: true}}
	}, Parse: parseCPU}
}

// Memory returns the machine memory capability.
func Memory() spec.Capability {
	return spec.Capability{ID: "machine.memory", Dimension: contract.DimensionMemory, Level: contract.L1Machine, Kind: contract.KindSampled, Accepts: contract.EntityMachine, Cost: contract.CostLow, Summary: "machine memory, reclaim, swap and memory pressure", LeadsTo: []string{"machine.processes"}, Reads: func(contract.Entity, contract.Facts) []source.Read {
		return []source.Read{{Key: "proc.meminfo", Path: "/proc/meminfo", Kind: source.ReadFile}, {Key: "proc.vmstat", Path: "/proc/vmstat", Kind: source.ReadFile}, {Key: "proc.mem_pressure", Path: "/proc/pressure/memory", Kind: source.ReadFile, Optional: true}}
	}, Parse: parseMemory}
}

// IO returns the machine block-IO capability.
func IO() spec.Capability {
	return spec.Capability{ID: "machine.io", Dimension: contract.DimensionIO, Level: contract.L1Machine, Kind: contract.KindSampled, Accepts: contract.EntityMachine, Cost: contract.CostLow, Summary: "block-device throughput, utilization, latency and pressure", Reads: func(contract.Entity, contract.Facts) []source.Read {
		return []source.Read{{Key: "proc.diskstats", Path: "/proc/diskstats", Kind: source.ReadFile}, {Key: "proc.io_pressure", Path: "/proc/pressure/io", Kind: source.ReadFile, Optional: true}}
	}, Parse: parseIO}
}

// Network returns the machine network capability.
func Network() spec.Capability {
	return spec.Capability{ID: "machine.network", Dimension: contract.DimensionNetwork, Level: contract.L1Machine, Kind: contract.KindSampled, Accepts: contract.EntityMachine, Cost: contract.CostLow, Summary: "interface, TCP and socket pressure", LeadsTo: []string{"machine.processes"}, Reads: func(contract.Entity, contract.Facts) []source.Read {
		return []source.Read{{Key: "proc.netdev", Path: "/proc/net/dev", Kind: source.ReadFile}, {Key: "proc.snmp", Path: "/proc/net/snmp", Kind: source.ReadFile, Optional: true}, {Key: "proc.netstat", Path: "/proc/net/netstat", Kind: source.ReadFile, Optional: true}, {Key: "proc.sockstat", Path: "/proc/net/sockstat", Kind: source.ReadFile, Optional: true}}
	}, Parse: parseNetwork}
}

// Limits returns the machine limits capability.
func Limits() spec.Capability {
	return spec.Capability{ID: "machine.limits", Dimension: contract.DimensionLimits, Level: contract.L1Machine, Kind: contract.KindSnapshot, Accepts: contract.EntityMachine, Cost: contract.CostLow, Summary: "global file-descriptor, PID and socket limits", Reads: func(contract.Entity, contract.Facts) []source.Read {
		return []source.Read{{Key: "proc.file_nr", Path: "/proc/sys/fs/file-nr", Kind: source.ReadFile, Optional: true}, {Key: "proc.pid_max", Path: "/proc/sys/kernel/pid_max", Kind: source.ReadFile, Optional: true}, {Key: "proc.somaxconn", Path: "/proc/sys/net/core/somaxconn", Kind: source.ReadFile, Optional: true}}
	}, Parse: parseLimits}
}

// Processes returns the process attribution hinge.
func Processes() spec.Capability {
	return spec.Capability{ID: "machine.processes", Dimension: contract.DimensionCPU, Level: contract.L1Machine, Kind: contract.KindSampled, Accepts: contract.EntityMachine, Cost: contract.CostMedium, Summary: "single-pass process attribution by CPU, RSS, IO and state", Reads: func(contract.Entity, contract.Facts) []source.Read {
		return []source.Read{{Key: "pid.stat", Path: "/proc/[0-9]*/stat", Kind: source.ReadGlob}, {Key: "pid.status", Path: "/proc/[0-9]*/status", Kind: source.ReadGlob}, {Key: "pid.io", Path: "/proc/[0-9]*/io", Kind: source.ReadGlob, Optional: true}}
	}, Parse: parseProcesses}
}

func latest(s source.Snapshot, k string) []source.Raw { return s.Reads[k] }
func first(s source.Snapshot, k string) []byte {
	r := latest(s, k)
	if len(r) == 0 {
		return nil
	}
	return r[0].Data
}
func ev(id, capID string, d contract.Dimension, l contract.Level, f any, obs []contract.Observation, src ...string) contract.Evidence {
	return contract.Evidence{ID: id, Capability: capID, Entity: contract.Entity{Kind: contract.EntityMachine, ID: "machine"}, Dimension: d, Level: l, CollectedAt: time.Now(), Facts: f, Observations: obs, Sources: src, Verify: src}
}
func o(k string, v float64, u string) contract.Observation {
	return contract.Observation{Key: k, Value: v, Unit: u}
}
func du(a, b uint64) uint64 {
	if b >= a {
		return b - a
	}
	return 0
}
func cpuTotal(c procfs.CPUTimes) uint64 {
	return c.User + c.Nice + c.System + c.Idle + c.IOWait + c.IRQ + c.SoftIRQ + c.Steal
}

func parseCPU(in spec.ParseInput) (contract.Evidence, error) {
	a, e := procfs.ParseStat(first(in.Sample.T0, "proc.stat"))
	if e != nil {
		return contract.Evidence{}, e
	}
	b, e := procfs.ParseStat(first(in.Sample.T1, "proc.stat"))
	if e != nil {
		return contract.Evidence{}, e
	}
	ta, tb := cpuTotal(a.CPU), cpuTotal(b.CPU)
	dt := float64(du(ta, tb))
	if dt <= 0 {
		dt = 1
	}
	busy := du(a.CPU.Idle, b.CPU.Idle)
	util := 100 - float64(busy)/dt*100
	u := float64(du(a.CPU.User+a.CPU.Nice, b.CPU.User+b.CPU.Nice)) / dt * 100
	sy := float64(du(a.CPU.System, b.CPU.System)) / dt * 100
	iw := float64(du(a.CPU.IOWait, b.CPU.IOWait)) / dt * 100
	irq := float64(du(a.CPU.IRQ, b.CPU.IRQ)) / dt * 100
	soft := float64(du(a.CPU.SoftIRQ, b.CPU.SoftIRQ)) / dt * 100
	steal := float64(du(a.CPU.Steal, b.CPU.Steal)) / dt * 100
	max, avg := 0.0, 0.0
	for i, c := range b.PerCore {
		if i >= len(a.PerCore) {
			break
		}
		x, y := cpuTotal(a.PerCore[i]), cpuTotal(c)
		d := float64(du(x, y))
		if d <= 0 {
			continue
		}
		p := 100 - float64(du(a.PerCore[i].Idle, c.Idle))/d*100
		if p > max {
			max = p
		}
		avg += p
	}
	if len(b.PerCore) > 0 {
		avg /= float64(len(b.PerCore))
	}
	load := procfs.LoadAvg{}
	if x := first(in.Sample.T1, "proc.loadavg"); len(x) > 0 {
		load, _ = procfs.ParseLoadAvg(x)
	}
	pressure := procfs.Pressure{}
	if x := first(in.Sample.T1, "proc.cpu_pressure"); len(x) > 0 {
		pressure, _ = procfs.ParsePressure(x)
	}
	cores := len(b.PerCore)
	if cores < 1 {
		cores = 1
	}
	f := CPUGFacts{Utilization: util, UserPct: u, SystemPct: sy, IOWaitPct: iw, IRQPct: irq, SoftIRQPct: soft, StealPct: steal, PerCoreMax: max, CoreImbalance: max - avg, LoadOnePerCore: load.One / float64(cores), PSISome: pressure.Some.Avg10, PSIFull: pressure.Full.Avg10, Runnable: uint64(maxInt(load.Runnable, 0)), Blocked: b.ProcsBlocked}
	return ev("ev-machine-cpu", "machine.cpu", contract.DimensionCPU, contract.L1Machine, f, []contract.Observation{o("cpu.utilization", util, "percent"), o("cpu.user_pct", u, "percent"), o("cpu.system_pct", sy, "percent"), o("cpu.iowait_pct", iw, "percent"), o("cpu.irq_pct", irq, "percent"), o("cpu.softirq_pct", soft, "percent"), o("cpu.steal_pct", steal, "percent"), o("cpu.per_core_max", max, "percent"), o("cpu.core_imbalance", max-avg, "percent"), o("load.one_per_core", f.LoadOnePerCore, "ratio"), o("cpu.psi_some_avg10", f.PSISome, "percent"), o("cpu.psi_full_avg10", f.PSIFull, "percent"), o("procs.running", float64(f.Runnable), "count"), o("procs.blocked", float64(f.Blocked), "count")}, "/proc/stat", "/proc/loadavg", "/proc/pressure/cpu"), nil
}

type CPUGFacts = CPUFacts

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func parseMemory(in spec.ParseInput) (contract.Evidence, error) {
	_, e := procfs.ParseMemInfo(first(in.Sample.T0, "proc.meminfo"))
	if e != nil {
		return contract.Evidence{}, e
	}
	b, e := procfs.ParseMemInfo(first(in.Sample.T1, "proc.meminfo"))
	if e != nil {
		return contract.Evidence{}, e
	}
	va, _ := procfs.ParseVMStat(first(in.Sample.T0, "proc.vmstat"))
	vb, _ := procfs.ParseVMStat(first(in.Sample.T1, "proc.vmstat"))
	p := procfs.Pressure{}
	if x := first(in.Sample.T1, "proc.mem_pressure"); len(x) > 0 {
		p, _ = procfs.ParsePressure(x)
	}
	avail, swap := 0.0, 0.0
	if b.MemTotal > 0 {
		avail = float64(b.MemAvailable) / float64(b.MemTotal) * 100
	}
	if b.SwapTotal > 0 {
		swap = float64(b.SwapTotal-b.SwapFree) / float64(b.SwapTotal) * 100
	}
	sec := in.Window.Seconds()
	if sec <= 0 {
		sec = 1
	}
	f := MemoryFacts{AvailablePct: avail, SwapUsedPct: swap, SwapinRate: float64(du(va.Pswpin, vb.Pswpin)) / sec, DirectReclaimRate: float64(du(va.PgscanDirect, vb.PgscanDirect)) / sec, MajorFaultRate: float64(du(va.Pgmajfault, vb.Pgmajfault)) / sec, OOMKillDelta: float64(du(va.OOMKill, vb.OOMKill)), PSIFull: p.Full.Avg10, WorkingsetRefaultRate: float64(du(va.WorkingsetRefault, vb.WorkingsetRefault)) / sec}
	return ev("ev-machine-memory", "machine.memory", contract.DimensionMemory, contract.L1Machine, f, []contract.Observation{o("mem.available_pct", avail, "percent"), o("mem.swap_used_pct", swap, "percent"), o("mem.swapin_rate", f.SwapinRate, "count_per_sec"), o("mem.direct_reclaim_rate", f.DirectReclaimRate, "count_per_sec"), o("mem.pgmajfault_rate", f.MajorFaultRate, "count_per_sec"), o("mem.oom_kill_delta", f.OOMKillDelta, "count"), o("mem.psi_full_avg10", f.PSIFull, "percent")}, "/proc/meminfo", "/proc/vmstat", "/proc/pressure/memory"), nil
}

func parseIO(in spec.ParseInput) (contract.Evidence, error) {
	a, e := procfs.ParseDiskStats(first(in.Sample.T0, "proc.diskstats"))
	if e != nil {
		return contract.Evidence{}, e
	}
	b, e := procfs.ParseDiskStats(first(in.Sample.T1, "proc.diskstats"))
	if e != nil {
		return contract.Evidence{}, e
	}
	am := map[string]procfs.DiskStat{}
	for _, d := range a {
		if !strings.HasPrefix(d.Name, "loop") && !strings.HasPrefix(d.Name, "ram") {
			am[d.Name] = d
		}
	}
	sec := in.Window.Seconds()
	if sec <= 0 {
		sec = 1
	}
	f := IOFacts{}
	for _, d := range b {
		p, ok := am[d.Name]
		if !ok {
			continue
		}
		reads := float64(du(p.ReadsCompleted, d.ReadsCompleted)) / sec
		writes := float64(du(p.WritesCompleted, d.WritesCompleted)) / sec
		bytesR := float64(du(p.SectorsRead, d.SectorsRead)) * 512 / sec
		bytesW := float64(du(p.SectorsWritten, d.SectorsWritten)) * 512 / sec
		comps := float64(du(p.ReadsCompleted+p.WritesCompleted, d.ReadsCompleted+d.WritesCompleted))
		await := 0.0
		if comps > 0 {
			await = float64(du(p.TimeInQueue, d.TimeInQueue)) / comps
		}
		util := float64(du(p.IOTicks, d.IOTicks)) / sec / 10
		f.Devices = append(f.Devices, IODeviceFacts{Name: d.Name, ReadBPS: bytesR, WriteBPS: bytesW, IOPS: reads + writes, UtilPct: util, AwaitMS: await, Inflight: d.IOsInProgress})
	}
	if x := first(in.Sample.T1, "proc.io_pressure"); len(x) > 0 {
		p, _ := procfs.ParsePressure(x)
		f.PSIFull = p.Full.Avg10
	}
	obs := []contract.Observation{o("io.psi_full_avg10", f.PSIFull, "percent")}
	for _, d := range f.Devices {
		obs = append(obs, o("io."+d.Name+".read_bps", d.ReadBPS, "bytes_per_sec"), o("io."+d.Name+".write_bps", d.WriteBPS, "bytes_per_sec"), o("io."+d.Name+".util_pct", d.UtilPct, "percent"), o("io."+d.Name+".await_ms", d.AwaitMS, "ms"), o("io."+d.Name+".inflight", float64(d.Inflight), "count"))
	}
	return ev("ev-machine-io", "machine.io", contract.DimensionIO, contract.L1Machine, f, obs, "/proc/diskstats", "/proc/pressure/io"), nil
}

func parseNetwork(in spec.ParseInput) (contract.Evidence, error) {
	a, e := procfs.ParseNetDev(first(in.Sample.T0, "proc.netdev"))
	if e != nil {
		return contract.Evidence{}, e
	}
	b, e := procfs.ParseNetDev(first(in.Sample.T1, "proc.netdev"))
	if e != nil {
		return contract.Evidence{}, e
	}
	sec := in.Window.Seconds()
	if sec <= 0 {
		sec = 1
	}
	am := map[string]procfs.NetDev{}
	for _, d := range a {
		am[d.Name] = d
	}
	f := NetworkFacts{}
	for _, d := range b {
		p := am[d.Name]
		f.RxBPS += float64(du(p.RxBytes, d.RxBytes)) / sec
		f.TxBPS += float64(du(p.TxBytes, d.TxBytes)) / sec
		f.RxDropRate += float64(du(p.RxDrop, d.RxDrop)) / sec
	}
	return ev("ev-machine-network", "machine.network", contract.DimensionNetwork, contract.L1Machine, f, []contract.Observation{o("net.rx_bps", f.RxBPS, "bytes_per_sec"), o("net.tx_bps", f.TxBPS, "bytes_per_sec"), o("net.rx_drop_rate", f.RxDropRate, "count_per_sec")}, "/proc/net/dev", "/proc/net/snmp", "/proc/net/netstat", "/proc/net/sockstat"), nil
}

func parseLimits(in spec.ParseInput) (contract.Evidence, error) {
	var f LimitsFacts
	if b := first(in.Sample.T1, "proc.file_nr"); len(b) > 0 {
		p := strings.Fields(string(b))
		if len(p) >= 3 {
			f.FDUsed, _ = strconv.ParseUint(p[0], 10, 64)
			f.FDMax, _ = strconv.ParseUint(p[2], 10, 64)
			if f.FDMax > 0 {
				f.FDUsedPct = float64(f.FDUsed) / float64(f.FDMax) * 100
			}
		}
	}
	if b := first(in.Sample.T1, "proc.pid_max"); len(b) > 0 {
		f.PIDMax, _ = strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
	}
	return ev("ev-machine-limits", "machine.limits", contract.DimensionLimits, contract.L1Machine, f, []contract.Observation{o("limits.fd_used_pct", f.FDUsedPct, "percent"), o("limits.pid_used_pct", f.PIDUsedPct, "percent")}, "/proc/sys/fs/file-nr", "/proc/sys/kernel/pid_max", "/proc/sys/net/core/somaxconn"), nil
}

func pathPID(p string) int64 {
	parts := strings.Split(filepath.ToSlash(p), "/")
	for i := 0; i+1 < len(parts); i++ {
		if parts[i] == "proc" {
			v, _ := strconv.ParseInt(parts[i+1], 10, 64)
			return v
		}
	}
	return 0
}
func parseProcesses(in spec.ParseInput) (contract.Evidence, error) {
	t0 := index(in.Sample.T0.Reads["pid.stat"])
	t1 := index(in.Sample.T1.Reads["pid.stat"])
	sts := index(in.Sample.T1.Reads["pid.status"])
	i0 := index(in.Sample.T0.Reads["pid.io"])
	i1 := index(in.Sample.T1.Reads["pid.io"])
	sec := in.Window.Seconds()
	if sec <= 0 {
		sec = 1
	}
	rows := make([]ProcessRow, 0)
	var total, dstate, zstate, threads uint64
	for path, data := range t1 {
		p, e := procfs.ParsePidStat(data)
		if e != nil {
			continue
		}
		old, oe := procfs.ParsePidStat(t0[path])
		if oe != nil {
			continue
		}
		cpu := float64(du(old.Utime+old.Stime, p.Utime+p.Stime)) / float64(100) / sec
		rss := uint64(0)
		if p.RSS > 0 {
			rss = uint64(p.RSS) * uint64(osPageSize())
		}
		ioB := 0.0
		if x, ok := i1[path]; ok {
			nn, pe := procfs.ParsePidIO(x)
			if pe == nil {
				if y, ok2 := i0[path]; ok2 {
					oo, pe2 := procfs.ParsePidIO(y)
					if pe2 == nil {
						ioB = float64(du(oo.ReadBytes+oo.WriteBytes, nn.ReadBytes+nn.WriteBytes)) / sec
					}
				}
			}
		}
		uid := uint64(0)
		if x, ok := sts[path]; ok {
			ss, pe := procfs.ParsePidStatus(x)
			if pe == nil {
				uid = ss.Uid[0]
			}
		}
		total++
		threads += uint64(maxInt64(p.NumThreads, 0))
		if p.State == 'D' {
			dstate++
		}
		if p.State == 'Z' {
			zstate++
		}
		rows = append(rows, ProcessRow{p.PID, p.PPID, string(p.State), rss, cpu, ioB, p.NumThreads, p.Starttime, uid, p.Comm})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].CPUPercent != rows[j].CPUPercent {
			return rows[i].CPUPercent > rows[j].CPUPercent
		}
		return rows[i].PID < rows[j].PID
	})
	if len(rows) > 10 {
		rows = rows[:10]
	}
	f := ProcessFacts{Total: total, DState: dstate, ZState: zstate, ThreadTotal: threads, Rows: rows}
	return ev("ev-machine-processes", "machine.processes", contract.DimensionCPU, contract.L1Machine, f, []contract.Observation{o("procs.total", float64(total), "count"), o("procs.d_state", float64(dstate), "count"), o("procs.z_state", float64(zstate), "count"), o("procs.thread_total", float64(threads), "count")}, "/proc/[0-9]*/stat", "/proc/[0-9]*/status", "/proc/[0-9]*/io"), nil
}
func index(rs []source.Raw) map[string][]byte {
	m := map[string][]byte{}
	for _, r := range rs {
		m[r.Path] = r.Data
	}
	return m
}
func maxInt64(v int64, n int64) int64 {
	if v > n {
		return v
	}
	return n
}

func osPageSize() int { return os.Getpagesize() }
