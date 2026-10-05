// Package pressure reads the cheap, aggregate pressure/utilization signals that
// drive pinproc's read-only "pull" trigger. The calm path reads only a handful of
// tiny kernel files (no per-process scan) into a reused buffer, so a long-lived
// sampler costs far less than a cron/timer that forks a process each tick.
package pressure

import (
	"bytes"
	"syscall"
	"time"
)

// Levels is a snapshot of the aggregate signals. Stall fields are PSI "some"
// avg10 percentages (time at least one task waited for the resource in the last
// 10s). Utilization fields are percentages of capacity.
type Levels struct {
	CPUStallPct  float64 // /proc/pressure/cpu    some avg10
	MemStallPct  float64 // /proc/pressure/memory some avg10
	IOStallPct   float64 // /proc/pressure/io     some avg10
	CPUUtilPct   float64 // from /proc/stat delta between ticks (0 on first tick)
	MemUsedPct   float64 // from MemTotal/MemAvailable
	SwapUsedPct  float64 // from SwapTotal/SwapFree
	Load1PerCore float64 // loadavg[0] / cores

	// Network signals (context + the things that actually hurt). Rates are 0 on
	// the first tick (no baseline).
	NetRxBps         float64 // sum of non-loopback interface rx bytes/s
	NetTxBps         float64 // sum of non-loopback interface tx bytes/s
	TCPRetransPerSec float64 // TCP RetransSegs/s (/proc/net/snmp)
	TCPInUse         uint64  // /proc/net/sockstat TCP inuse
	TCPTimeWait      uint64  // /proc/net/sockstat TCP tw
	TCPOrphan        uint64  // /proc/net/sockstat TCP orphan
	ConntrackPct     float64 // nf_conntrack_count / nf_conntrack_max * 100 (0 if absent)
}

// Parse keys hoisted to package level so the calm path allocates nothing.
var (
	kSome    = []byte("some")
	kAvg10   = []byte("avg10=")
	kMemTot  = []byte("MemTotal:")
	kMemAvl  = []byte("MemAvailable:")
	kSwapTot = []byte("SwapTotal:")
	kSwapFre = []byte("SwapFree:")
	kCPU     = []byte("cpu ")
)

// Reader reads aggregate pressure cheaply. It is not safe for concurrent use; a
// single sampler goroutine owns one Reader and its buffer.
type Reader struct {
	cores   int
	buf     []byte
	pCPU    string
	pMem    string
	pIO     string
	pMemnfo string
	pLoad   string
	pStat   string
	pNetDev string
	pSnmp   string
	pSock   string
	pCtCnt  string
	pCtMax  string
	// previous /proc/stat aggregate counters for utilization delta
	prevBusy, prevTotal uint64
	havePrev            bool
	// previous network counters for rate delta
	prevRx, prevTx, prevRetrans uint64
	lastNetAt                   time.Time
	haveNet                     bool
}

// NewReader returns a Reader. procRoot defaults to /proc; cores is used to
// normalize load average per core. Paths are precomputed so the calm tick does no
// string building.
func NewReader(procRoot string, cores int) *Reader {
	if procRoot == "" {
		procRoot = "/proc"
	}
	if cores < 1 {
		cores = 1
	}
	return &Reader{
		cores:   cores,
		buf:     make([]byte, 8192),
		pCPU:    procRoot + "/pressure/cpu",
		pMem:    procRoot + "/pressure/memory",
		pIO:     procRoot + "/pressure/io",
		pMemnfo: procRoot + "/meminfo",
		pLoad:   procRoot + "/loadavg",
		pStat:   procRoot + "/stat",
		pNetDev: procRoot + "/net/dev",
		pSnmp:   procRoot + "/net/snmp",
		pSock:   procRoot + "/net/sockstat",
		pCtCnt:  procRoot + "/sys/net/netfilter/nf_conntrack_count",
		pCtMax:  procRoot + "/sys/net/netfilter/nf_conntrack_max",
	}
}

// Calm reads only the aggregate signals — no per-process scan. Missing or
// unreadable files degrade to zero for that field rather than erroring, so the
// sampler keeps running on hosts that lack PSI; doctor reports availability.
func (r *Reader) Calm() Levels {
	var l Levels
	l.CPUStallPct = r.psiSome(r.pCPU)
	l.MemStallPct = r.psiSome(r.pMem)
	l.IOStallPct = r.psiSome(r.pIO)
	r.readMeminfo(&l)
	l.Load1PerCore = r.load1() / float64(r.cores)
	l.CPUUtilPct = r.cpuUtil()
	r.readNet(&l)
	return l
}

// readNet fills the network signals. Rates use the delta since the previous tick.
func (r *Reader) readNet(l *Levels) {
	now := time.Now()
	dt := now.Sub(r.lastNetAt).Seconds()
	var rx, tx uint64
	if data := r.read(r.pNetDev); data != nil {
		rx, tx = sumNetDev(data)
	}
	var retrans uint64
	if data := r.read(r.pSnmp); data != nil {
		retrans = snmpTCPRetrans(data)
	}
	if r.haveNet && dt > 0 {
		if rx >= r.prevRx {
			l.NetRxBps = float64(rx-r.prevRx) / dt
		}
		if tx >= r.prevTx {
			l.NetTxBps = float64(tx-r.prevTx) / dt
		}
		if retrans >= r.prevRetrans {
			l.TCPRetransPerSec = float64(retrans-r.prevRetrans) / dt
		}
	}
	r.prevRx, r.prevTx, r.prevRetrans, r.lastNetAt, r.haveNet = rx, tx, retrans, now, true

	if data := r.read(r.pSock); data != nil {
		l.TCPInUse, l.TCPOrphan, l.TCPTimeWait = sockstatTCP(data)
	}
	cnt := parseUint(r.read(r.pCtCnt))
	max := parseUint(r.read(r.pCtMax))
	if max > 0 {
		l.ConntrackPct = float64(cnt) / float64(max) * 100
	}
}

// cpuUtil computes CPU utilization since the previous Calm tick from the /proc/stat
// aggregate line. Returns 0 on the first tick (no baseline yet).
func (r *Reader) cpuUtil() float64 {
	data := r.read(r.pStat)
	if data == nil {
		return 0
	}
	line := data
	if nl := bytes.IndexByte(data, '\n'); nl >= 0 {
		line = data[:nl]
	}
	if !bytes.HasPrefix(line, kCPU) {
		return 0
	}
	// Fields after "cpu ": user nice system idle iowait irq softirq steal ...
	fields := line[len(kCPU):]
	var vals [10]uint64
	n := 0
	i := 0
	for n < len(vals) && i < len(fields) {
		for i < len(fields) && fields[i] == ' ' {
			i++
		}
		start := i
		var v uint64
		for i < len(fields) && fields[i] >= '0' && fields[i] <= '9' {
			v = v*10 + uint64(fields[i]-'0')
			i++
		}
		if i == start {
			break
		}
		vals[n] = v
		n++
	}
	if n < 5 {
		return 0
	}
	var total uint64
	for j := 0; j < n; j++ {
		total += vals[j]
	}
	idle := vals[3] + vals[4] // idle + iowait
	busy := total - idle
	defer func() { r.prevBusy, r.prevTotal, r.havePrev = busy, total, true }()
	if !r.havePrev || total <= r.prevTotal {
		return 0
	}
	dt := total - r.prevTotal
	db := busy - r.prevBusy
	if dt == 0 {
		return 0
	}
	return float64(db) / float64(dt) * 100
}

// read reads a small file into the reused buffer using raw syscalls, so a calm
// tick allocates nothing (no *os.File on the heap). Returns the filled slice.
func (r *Reader) read(path string) []byte {
	fd, err := syscall.Open(path, syscall.O_RDONLY, 0)
	if err != nil {
		return nil
	}
	n, err := syscall.Read(fd, r.buf)
	_ = syscall.Close(fd)
	if err != nil || n <= 0 {
		return nil
	}
	return r.buf[:n]
}

// psiSome parses the "some avg10=" value from /proc/pressure/<res>.
func (r *Reader) psiSome(path string) float64 {
	data := r.read(path)
	if data == nil {
		return 0
	}
	// The "some" line is first; take avg10 from it.
	line := data
	if nl := bytes.IndexByte(data, '\n'); nl >= 0 {
		line = data[:nl]
	}
	if !bytes.HasPrefix(line, kSome) {
		return 0
	}
	return floatAfter(line, kAvg10)
}

// readMeminfo fills memory/swap used percentages from /proc/meminfo.
func (r *Reader) readMeminfo(l *Levels) {
	data := r.read(r.pMemnfo)
	if data == nil {
		return
	}
	memTotal := kbValue(data, kMemTot)
	memAvail := kbValue(data, kMemAvl)
	if memTotal > 0 && memAvail <= memTotal {
		l.MemUsedPct = float64(memTotal-memAvail) / float64(memTotal) * 100
	}
	swapTotal := kbValue(data, kSwapTot)
	swapFree := kbValue(data, kSwapFre)
	if swapTotal > 0 && swapFree <= swapTotal {
		l.SwapUsedPct = float64(swapTotal-swapFree) / float64(swapTotal) * 100
	}
}

// load1 parses the 1-minute load average (first field of /proc/loadavg).
func (r *Reader) load1() float64 {
	data := r.read(r.pLoad)
	if data == nil {
		return 0
	}
	if sp := bytes.IndexByte(data, ' '); sp > 0 {
		return parseFloat(data[:sp])
	}
	return 0
}

// floatAfter finds key in line and parses the float immediately following it.
func floatAfter(line, key []byte) float64 {
	i := bytes.Index(line, key)
	if i < 0 {
		return 0
	}
	rest := line[i+len(key):]
	end := 0
	for end < len(rest) && rest[end] != ' ' && rest[end] != '\n' {
		end++
	}
	return parseFloat(rest[:end])
}

// kbValue finds a "Key:   <n> kB" line and returns n (kB).
func kbValue(data, key []byte) uint64 {
	i := bytes.Index(data, key)
	if i < 0 {
		return 0
	}
	rest := data[i+len(key):]
	// skip spaces
	j := 0
	for j < len(rest) && rest[j] == ' ' {
		j++
	}
	start := j
	for j < len(rest) && rest[j] >= '0' && rest[j] <= '9' {
		j++
	}
	if j == start {
		return 0
	}
	return parseUint(rest[start:j])
}

// parseUint parses a non-negative integer from bytes with no allocation.
func parseUint(b []byte) uint64 {
	var v uint64
	for _, c := range b {
		if c < '0' || c > '9' {
			break
		}
		v = v*10 + uint64(c-'0')
	}
	return v
}

// parseFloat parses a non-negative decimal (e.g. "12.34") from bytes with no
// allocation. The kernel values we read are simple fixed-point, no sign/exponent.
// sumNetDev sums rx (col 1) and tx (col 9) bytes across non-loopback interfaces.
func sumNetDev(data []byte) (rx, tx uint64) {
	for len(data) > 0 {
		var line []byte
		if nl := bytes.IndexByte(data, '\n'); nl >= 0 {
			line, data = data[:nl], data[nl+1:]
		} else {
			line, data = data, nil
		}
		colon := bytes.IndexByte(line, ':')
		if colon < 0 {
			continue // header lines have no colon in the iface position
		}
		iface := bytes.TrimSpace(line[:colon])
		if len(iface) == 0 || bytes.Equal(iface, []byte("lo")) {
			continue
		}
		fields := bytes.Fields(line[colon+1:])
		if len(fields) >= 9 {
			rx += parseUint(fields[0])
			tx += parseUint(fields[8])
		}
	}
	return rx, tx
}

// snmpTCPRetrans extracts Tcp RetransSegs from /proc/net/snmp by matching the
// header column to the value column (robust to column order across kernels).
func snmpTCPRetrans(data []byte) uint64 {
	var header, values []byte
	for len(data) > 0 {
		var line []byte
		if nl := bytes.IndexByte(data, '\n'); nl >= 0 {
			line, data = data[:nl], data[nl+1:]
		} else {
			line, data = data, nil
		}
		if !bytes.HasPrefix(line, []byte("Tcp:")) {
			continue
		}
		if header == nil {
			header = line
		} else {
			values = line
			break
		}
	}
	if header == nil || values == nil {
		return 0
	}
	hf := bytes.Fields(header)
	vf := bytes.Fields(values)
	for i, h := range hf {
		if bytes.Equal(h, []byte("RetransSegs")) && i < len(vf) {
			return parseUint(vf[i])
		}
	}
	return 0
}

// sockstatTCP parses the "TCP: inuse N orphan N tw N ..." line of /proc/net/sockstat.
func sockstatTCP(data []byte) (inuse, orphan, tw uint64) {
	for len(data) > 0 {
		var line []byte
		if nl := bytes.IndexByte(data, '\n'); nl >= 0 {
			line, data = data[:nl], data[nl+1:]
		} else {
			line, data = data, nil
		}
		if !bytes.HasPrefix(line, []byte("TCP:")) {
			continue
		}
		f := bytes.Fields(line)
		// f[0] is "TCP:"; key/value pairs start at index 1.
		for i := 1; i+1 < len(f); i += 2 {
			switch string(f[i]) {
			case "inuse":
				inuse = parseUint(f[i+1])
			case "orphan":
				orphan = parseUint(f[i+1])
			case "tw":
				tw = parseUint(f[i+1])
			}
		}
		return inuse, orphan, tw
	}
	return 0, 0, 0
}

func parseFloat(b []byte) float64 {
	b = bytes.TrimSpace(b)
	var intPart uint64
	i := 0
	for i < len(b) && b[i] >= '0' && b[i] <= '9' {
		intPart = intPart*10 + uint64(b[i]-'0')
		i++
	}
	v := float64(intPart)
	if i < len(b) && b[i] == '.' {
		i++
		frac, scale := 0.0, 0.1
		for i < len(b) && b[i] >= '0' && b[i] <= '9' {
			frac += float64(b[i]-'0') * scale
			scale *= 0.1
			i++
		}
		v += frac
	}
	return v
}
