package procfs

import (
	"bytes"
	"errors"
	"strconv"
	"strings"
)

// CPUTimes contains /proc/stat CPU counters.
type CPUTimes struct{ User, Nice, System, Idle, IOWait, IRQ, SoftIRQ, Steal, Guest, GuestNice uint64 }

// Stat is parsed /proc/stat state.
type Stat struct {
	CPU                                         CPUTimes
	PerCore                                     []CPUTimes
	Ctxt, Processes, ProcsRunning, ProcsBlocked uint64
	BootTime                                    int64
}

// MemInfo contains selected /proc/meminfo counters in bytes.
type MemInfo struct{ MemTotal, MemFree, MemAvailable, Buffers, Cached, SwapTotal, SwapFree, Dirty, Writeback, Slab, SReclaimable, AnonPages, Mapped, Shmem, CommitLimit, CommittedAS uint64 }

// VMStat contains selected /proc/vmstat counters.
type VMStat struct{ PgscanKswapd, PgscanDirect, PgstealKswapd, PgstealDirect, Pswpin, Pswpout, Pgfault, Pgmajfault, OOMKill, NrFreePages, WorkingsetRefault uint64 }

// LoadAvg is parsed /proc/loadavg.
type LoadAvg struct {
	One, Five, Fifteen       float64
	Runnable, Total, LastPID int
}

// PSILine is one PSI line.
type PSILine struct {
	Avg10, Avg60, Avg300 float64
	Total                uint64
}

// Pressure contains PSI some/full lines.
type Pressure struct{ Some, Full PSILine }

// PidStat contains named /proc/<pid>/stat fields.
type PidStat struct {
	PID, PPID, PGRP, Session                       int64
	State                                          byte
	Comm                                           string
	Utime, Stime, Cutime, Cstime                   uint64
	NumThreads                                     int64
	Starttime                                      uint64
	Vsize                                          uint64
	RSS                                            int64
	Processor, RTPriority, Policy                  int64
	DelayacctBlkioTicks, GuestTime, MinFlt, MajFlt uint64
}

// PidStatus contains selected status fields.
type PidStatus struct {
	Name, Umask, State                                                         string
	Tgid, Pid, PPid, TracerPid                                                 int64
	Uid, Gid                                                                   [4]uint64
	FDSize, VmPeak, VmSize, VmRSS, RssAnon, RssFile, RssShmem, VmSwap, Threads uint64
	VoluntaryCtxSwitches, NonvoluntaryCtxSwitches                              uint64
}

// SmapsRollup contains aggregate memory mapping counters.
type SmapsRollup struct{ Rss, Pss, PssAnon, PssFile, PssShmem, SharedClean, SharedDirty, PrivateClean, PrivateDirty, Referenced, Anonymous, LazyFree, Swap, SwapPss uint64 }

// PidIO contains /proc/<pid>/io counters.
type PidIO struct{ RChar, WChar, SyscR, SyscW, ReadBytes, WriteBytes, CancelledWriteBytes uint64 }

// SchedStat contains cumulative scheduling counters.
type SchedStat struct{ RunTimeNS, RunqueueWaitNS, Timeslices uint64 }

// DiskStat is a /proc/diskstats row.
type DiskStat struct {
	Major, Minor                                                                                                                                         uint64
	Name                                                                                                                                                 string
	ReadsCompleted, ReadsMerged, SectorsRead, ReadTicks, WritesCompleted, WritesMerged, SectorsWritten, WritesTicks, IOsInProgress, IOTicks, TimeInQueue uint64
}

// NetDev is a network interface counter row.
type NetDev struct {
	Name                                                                   string
	RxBytes, RxPackets, RxErrs, RxDrop, TxBytes, TxPackets, TxErrs, TxDrop uint64
}

// Socket is a /proc/net socket row.
type Socket struct {
	LocalAddr                    string
	LocalPort, RemotePort        int
	RemoteAddr, State            string
	TxQueue, RxQueue, Inode, UID uint64
}

func u64(b []byte) (uint64, error) { return strconv.ParseUint(string(b), 10, 64) }
func firstU64(b []byte) uint64 {
	f := bytes.Fields(b)
	if len(f) == 0 {
		return 0
	}
	v, _ := u64(f[0])
	return v
}
func kvBytes(b []byte) uint64 { return firstU64(b) * 1024 }
func fields2(b []byte) ([]byte, []byte, bool) {
	i := bytes.IndexByte(b, ' ')
	if i < 0 {
		return nil, nil, false
	}
	return bytes.TrimSpace(b[:i]), bytes.TrimSpace(b[i+1:]), true
}

// ParseStat parses /proc/stat.
func ParseStat(b []byte) (Stat, error) {
	var o Stat
	for _, l := range bytes.Split(b, []byte{'\n'}) {
		f := bytes.Fields(l)
		if len(f) == 0 {
			continue
		}
		name := f[0]
		if bytes.Equal(name, []byte("cpu")) {
			if len(f) < 5 {
				return Stat{}, errors.New("short cpu line")
			}
			var v [10]uint64
			for i := 0; i < 10 && i+1 < len(f); i++ {
				v[i] = firstU64(f[i+1])
			}
			o.CPU = CPUTimes{v[0], v[1], v[2], v[3], v[4], v[5], v[6], v[7], v[8], v[9]}
		} else if len(name) > 3 && bytes.Equal(name[:3], []byte("cpu")) && name[3] >= '0' && name[3] <= '9' {
			var v [10]uint64
			for i := 0; i < 10 && i+1 < len(f); i++ {
				v[i] = firstU64(f[i+1])
			}
			o.PerCore = append(o.PerCore, CPUTimes{v[0], v[1], v[2], v[3], v[4], v[5], v[6], v[7], v[8], v[9]})
		}
		switch string(name) {
		case "ctxt":
			if len(f) > 1 {
				o.Ctxt = firstU64(f[1])
			}
		case "processes":
			if len(f) > 1 {
				o.Processes = firstU64(f[1])
			}
		case "procs_running":
			if len(f) > 1 {
				o.ProcsRunning = firstU64(f[1])
			}
		case "procs_blocked":
			if len(f) > 1 {
				o.ProcsBlocked = firstU64(f[1])
			}
		case "btime":
			if len(f) > 1 {
				o.BootTime = int64(firstU64(f[1]))
			}
		}
	}
	if o.CPU == (CPUTimes{}) {
		return Stat{}, errors.New("cpu line missing")
	}
	return o, nil
}

// ParseMemInfo parses /proc/meminfo.
func ParseMemInfo(b []byte) (MemInfo, error) {
	var o MemInfo
	seen := false
	for _, l := range bytes.Split(b, []byte{'\n'}) {
		i := bytes.IndexByte(l, ':')
		if i < 0 {
			continue
		}
		v := kvBytes(bytes.TrimSpace(l[i+1:]))
		switch string(l[:i]) {
		case "MemTotal":
			o.MemTotal = v
			seen = true
		case "MemFree":
			o.MemFree = v
		case "MemAvailable":
			o.MemAvailable = v
		case "Buffers":
			o.Buffers = v
		case "Cached":
			o.Cached = v
		case "SwapTotal":
			o.SwapTotal = v
		case "SwapFree":
			o.SwapFree = v
		case "Dirty":
			o.Dirty = v
		case "Writeback":
			o.Writeback = v
		case "Slab":
			o.Slab = v
		case "SReclaimable":
			o.SReclaimable = v
		case "AnonPages":
			o.AnonPages = v
		case "Mapped":
			o.Mapped = v
		case "Shmem":
			o.Shmem = v
		case "CommitLimit":
			o.CommitLimit = v
		case "Committed_AS":
			o.CommittedAS = v
		}
	}
	if !seen {
		return MemInfo{}, errors.New("memtotal missing")
	}
	return o, nil
}

// ParseVMStat parses selected /proc/vmstat counters.
func ParseVMStat(b []byte) (VMStat, error) {
	var o VMStat
	seen := false
	for _, l := range bytes.Split(b, []byte{'\n'}) {
		f := bytes.Fields(l)
		if len(f) != 2 {
			continue
		}
		v, _ := u64(f[1])
		switch string(f[0]) {
		case "pgscan_kswapd":
			o.PgscanKswapd = v
			seen = true
		case "pgscan_direct":
			o.PgscanDirect = v
		case "pgsteal_kswapd":
			o.PgstealKswapd = v
		case "pgsteal_direct":
			o.PgstealDirect = v
		case "pswpin":
			o.Pswpin = v
		case "pswpout":
			o.Pswpout = v
		case "pgfault":
			o.Pgfault = v
		case "pgmajfault":
			o.Pgmajfault = v
		case "oom_kill":
			o.OOMKill = v
		case "nr_free_pages":
			o.NrFreePages = v
		case "workingset_refault":
			o.WorkingsetRefault = v
		}
	}
	if !seen {
		return VMStat{}, errors.New("vmstat fields missing")
	}
	return o, nil
}

// ParseLoadAvg parses /proc/loadavg.
func ParseLoadAvg(b []byte) (LoadAvg, error) {
	f := bytes.Fields(b)
	if len(f) < 4 {
		return LoadAvg{}, errors.New("short loadavg")
	}
	var o LoadAvg
	var e error
	if o.One, e = strconv.ParseFloat(string(f[0]), 64); e != nil {
		return o, e
	}
	if o.Five, e = strconv.ParseFloat(string(f[1]), 64); e != nil {
		return o, e
	}
	if o.Fifteen, e = strconv.ParseFloat(string(f[2]), 64); e != nil {
		return o, e
	}
	p := bytes.SplitN(f[3], []byte{'/'}, 2)
	if len(p) != 2 {
		return o, errors.New("bad runnable")
	}
	o.Runnable, _ = strconv.Atoi(string(p[0]))
	o.Total, _ = strconv.Atoi(string(p[1]))
	if len(f) > 4 {
		o.LastPID, _ = strconv.Atoi(string(f[4]))
	}
	return o, nil
}

// ParsePressure parses /proc/pressure/{cpu,memory,io}.
func ParsePressure(b []byte) (Pressure, error) {
	var o Pressure
	seen := false
	for _, l := range bytes.Split(b, []byte{'\n'}) {
		f := bytes.Fields(l)
		if len(f) < 2 {
			continue
		}
		var p PSILine
		for _, x := range f[1:] {
			if bytes.HasPrefix(x, []byte("avg10=")) {
				p.Avg10, _ = strconv.ParseFloat(string(x[6:]), 64)
			} else if bytes.HasPrefix(x, []byte("avg60=")) {
				p.Avg60, _ = strconv.ParseFloat(string(x[6:]), 64)
			} else if bytes.HasPrefix(x, []byte("avg300=")) {
				p.Avg300, _ = strconv.ParseFloat(string(x[7:]), 64)
			} else if bytes.HasPrefix(x, []byte("total=")) {
				p.Total = firstU64(x[6:])
			}
		}
		if bytes.Equal(f[0], []byte("some")) {
			o.Some = p
			seen = true
		} else if bytes.Equal(f[0], []byte("full")) {
			o.Full = p
		}
	}
	if !seen {
		return Pressure{}, errors.New("psi some missing")
	}
	return o, nil
}

// ParsePidStat parses /proc/<pid>/stat; comm is delimited by the LAST ')' character.
func ParsePidStat(b []byte) (PidStat, error) {
	left := bytes.IndexByte(b, '(')
	right := bytes.LastIndexByte(b, ')')
	if left < 1 || right < left+2 {
		return PidStat{}, errors.New("invalid pid stat")
	}
	sp := bytes.IndexByte(b[:left], ' ')
	if sp < 0 {
		return PidStat{}, errors.New("pid missing")
	}
	pid, e := u64(b[:sp])
	if e != nil {
		return PidStat{}, e
	}
	tail := bytes.Fields(b[right+2:])
	if len(tail) < 50 {
		return PidStat{}, errors.New("short pid stat")
	}
	o := PidStat{PID: int64(pid), Comm: string(b[left+1 : right]), State: tail[0][0], PPID: int64(firstU64(tail[1-1+1])), PGRP: int64(firstU64(tail[2-1+1])), Session: int64(firstU64(tail[3-1+1])), Utime: firstU64(tail[11]), Stime: firstU64(tail[12]), Cutime: firstU64(tail[13]), Cstime: firstU64(tail[14]), MinFlt: firstU64(tail[7]), MajFlt: firstU64(tail[9]), NumThreads: int64(firstU64(tail[17])), Starttime: firstU64(tail[19]), Vsize: firstU64(tail[20]), RSS: int64(firstU64(tail[21])), Processor: int64(firstU64(tail[36])), RTPriority: int64(firstU64(tail[37])), Policy: int64(firstU64(tail[38])), DelayacctBlkioTicks: firstU64(tail[39]), GuestTime: firstU64(tail[40])}
	return o, nil
}

// ParsePidStatus parses selected /proc/<pid>/status fields.
func ParsePidStatus(b []byte) (PidStatus, error) {
	var o PidStatus
	seen := false
	for _, l := range bytes.Split(b, []byte{'\n'}) {
		i := bytes.IndexByte(l, ':')
		if i < 0 {
			continue
		}
		k := string(l[:i])
		v := bytes.TrimSpace(l[i+1:])
		switch k {
		case "Name":
			o.Name = string(v)
			seen = true
		case "Umask":
			o.Umask = string(v)
		case "State":
			o.State = string(v)
		case "Tgid":
			o.Tgid = int64(firstU64(v))
		case "Pid":
			o.Pid = int64(firstU64(v))
		case "PPid":
			o.PPid = int64(firstU64(v))
		case "TracerPid":
			o.TracerPid = int64(firstU64(v))
		case "Uid":
			parse4(v, &o.Uid)
		case "Gid":
			parse4(v, &o.Gid)
		case "FDSize":
			o.FDSize = firstU64(v)
		case "VmPeak":
			o.VmPeak = kvBytes(v)
		case "VmSize":
			o.VmSize = kvBytes(v)
		case "VmRSS":
			o.VmRSS = kvBytes(v)
		case "RssAnon":
			o.RssAnon = kvBytes(v)
		case "RssFile":
			o.RssFile = kvBytes(v)
		case "RssShmem":
			o.RssShmem = kvBytes(v)
		case "VmSwap":
			o.VmSwap = kvBytes(v)
		case "Threads":
			o.Threads = firstU64(v)
		case "voluntary_ctxt_switches":
			o.VoluntaryCtxSwitches = firstU64(v)
		case "nonvoluntary_ctxt_switches":
			o.NonvoluntaryCtxSwitches = firstU64(v)
		}
	}
	if !seen {
		return PidStatus{}, errors.New("name missing")
	}
	return o, nil
}
func parse4(b []byte, d *[4]uint64) {
	f := bytes.Fields(b)
	for i := 0; i < 4 && i < len(f); i++ {
		d[i] = firstU64(f[i])
	}
}

// ParseSmapsRollup parses /proc/<pid>/smaps_rollup.
func ParseSmapsRollup(b []byte) (SmapsRollup, error) {
	var o SmapsRollup
	seen := false
	for _, l := range bytes.Split(b, []byte{'\n'}) {
		i := bytes.IndexByte(l, ':')
		if i < 0 {
			continue
		}
		v := kvBytes(bytes.TrimSpace(l[i+1:]))
		switch string(l[:i]) {
		case "Rss":
			o.Rss = v
			seen = true
		case "Pss":
			o.Pss = v
		case "Pss_Anon":
			o.PssAnon = v
		case "Pss_File":
			o.PssFile = v
		case "Pss_Shmem":
			o.PssShmem = v
		case "Shared_Clean":
			o.SharedClean = v
		case "Shared_Dirty":
			o.SharedDirty = v
		case "Private_Clean":
			o.PrivateClean = v
		case "Private_Dirty":
			o.PrivateDirty = v
		case "Referenced":
			o.Referenced = v
		case "Anonymous":
			o.Anonymous = v
		case "LazyFree":
			o.LazyFree = v
		case "Swap":
			o.Swap = v
		case "SwapPss":
			o.SwapPss = v
		}
	}
	if !seen {
		return SmapsRollup{}, errors.New("rss missing")
	}
	return o, nil
}

// ParsePidIO parses /proc/<pid>/io.
func ParsePidIO(b []byte) (PidIO, error) {
	var o PidIO
	seen := false
	for _, l := range bytes.Split(b, []byte{'\n'}) {
		i := bytes.IndexByte(l, ':')
		if i < 0 {
			continue
		}
		v := firstU64(l[i+1:])
		switch string(l[:i]) {
		case "rchar":
			o.RChar = v
			seen = true
		case "wchar":
			o.WChar = v
		case "syscr":
			o.SyscR = v
		case "syscw":
			o.SyscW = v
		case "read_bytes":
			o.ReadBytes = v
		case "write_bytes":
			o.WriteBytes = v
		case "cancelled_write_bytes":
			o.CancelledWriteBytes = v
		}
	}
	if !seen {
		return PidIO{}, errors.New("rchar missing")
	}
	return o, nil
}

// ParseSchedStat parses /proc/<pid>/schedstat.
func ParseSchedStat(b []byte) (SchedStat, error) {
	f := bytes.Fields(b)
	if len(f) < 3 {
		return SchedStat{}, errors.New("short schedstat")
	}
	return SchedStat{firstU64(f[0]), firstU64(f[1]), firstU64(f[2])}, nil
}

// ParseDiskStats parses supported /proc/diskstats widths.
func ParseDiskStats(b []byte) ([]DiskStat, error) {
	var o []DiskStat
	for _, l := range bytes.Split(b, []byte{'\n'}) {
		f := bytes.Fields(l)
		if len(f) != 14 && len(f) != 18 && len(f) != 20 {
			continue
		}
		d := DiskStat{Major: firstU64(f[0]), Minor: firstU64(f[1]), Name: string(f[2]), ReadsCompleted: firstU64(f[3]), ReadsMerged: firstU64(f[4]), SectorsRead: firstU64(f[5]), ReadTicks: firstU64(f[6]), WritesCompleted: firstU64(f[7]), WritesMerged: firstU64(f[8]), SectorsWritten: firstU64(f[9]), WritesTicks: firstU64(f[10]), IOsInProgress: firstU64(f[11]), IOTicks: firstU64(f[12]), TimeInQueue: firstU64(f[13])}
		o = append(o, d)
	}
	if len(o) == 0 {
		return nil, errors.New("no disk records")
	}
	return o, nil
}

// ParseNetDev parses /proc/net/dev.
func ParseNetDev(b []byte) ([]NetDev, error) {
	var o []NetDev
	for _, l := range bytes.Split(b, []byte{'\n'}) {
		i := bytes.IndexByte(l, ':')
		if i < 0 {
			continue
		}
		f := bytes.Fields(l[i+1:])
		if len(f) < 12 {
			continue
		}
		o = append(o, NetDev{Name: strings.TrimSpace(string(l[:i])), RxBytes: firstU64(f[0]), RxPackets: firstU64(f[1]), RxErrs: firstU64(f[2]), RxDrop: firstU64(f[3]), TxBytes: firstU64(f[8]), TxPackets: firstU64(f[9]), TxErrs: firstU64(f[10]), TxDrop: firstU64(f[11])})
	}
	if len(o) == 0 {
		return nil, errors.New("no net devices")
	}
	return o, nil
}

// ParseSocketTable parses /proc/net/{tcp,tcp6,udp,udp6} style rows.
func ParseSocketTable(b []byte) ([]Socket, error) {
	var o []Socket
	for _, l := range bytes.Split(b, []byte{'\n'}) {
		f := bytes.Fields(l)
		if len(f) < 10 {
			continue
		}
		local := bytes.SplitN(f[1], []byte{':'}, 2)
		remote := bytes.SplitN(f[2], []byte{':'}, 2)
		if len(local) != 2 || len(remote) != 2 {
			continue
		}
		q := bytes.SplitN(f[4], []byte{':'}, 2)
		if len(q) != 2 {
			continue
		}
		lp, _ := strconv.ParseUint(string(local[1]), 16, 16)
		rp, _ := strconv.ParseUint(string(remote[1]), 16, 16)
		tx, _ := strconv.ParseUint(string(q[0]), 16, 64)
		rx, _ := strconv.ParseUint(string(q[1]), 16, 64)
		ino, _ := strconv.ParseUint(string(f[9]), 10, 64)
		uid, _ := strconv.ParseUint(string(f[7]), 10, 64)
		o = append(o, Socket{LocalAddr: string(local[0]), LocalPort: int(lp), RemoteAddr: string(remote[0]), RemotePort: int(rp), State: string(f[3]), TxQueue: tx, RxQueue: rx, Inode: ino, UID: uid})
	}
	if len(o) == 0 {
		return nil, errors.New("no socket rows")
	}
	return o, nil
}
