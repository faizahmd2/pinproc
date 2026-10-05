// Package contenders samples the top processes for a single resource dimension.
// It runs only while the monitor has armed a dimension (never on the calm path),
// and reuses the existing procfs parsers. CPU and I/O need deltas between ticks,
// so the sampler holds the previous counters; Reset drops that state on disarm.
package contenders

import (
	"os"
	"sort"
	"time"

	"github.com/faizahmd2/pinproc/internal/contract"
	"github.com/faizahmd2/pinproc/internal/dots"
	"github.com/faizahmd2/pinproc/internal/procfs"
)

// maxPIDs bounds how many processes we parse in one armed tick.
const maxPIDs = 8192

// Sampler produces top-N contenders for a dimension. Not safe for concurrent use.
type Sampler struct {
	procRoot string
	pageSize int64
	buf      []byte

	prevCPU   map[int]uint64
	prevIO    map[int]uint64
	lastCPUAt time.Time
	lastIOAt  time.Time
}

// NewSampler returns a sampler reading under procRoot (default /proc).
func NewSampler(procRoot string) *Sampler {
	if procRoot == "" {
		procRoot = "/proc"
	}
	return &Sampler{
		procRoot: procRoot,
		pageSize: int64(os.Getpagesize()),
		buf:      make([]byte, 32*1024),
	}
}

// Reset drops delta state. Call on disarm/discard so a new watch starts clean.
func (s *Sampler) Reset() {
	s.prevCPU = nil
	s.prevIO = nil
	s.lastCPUAt = time.Time{}
	s.lastIOAt = time.Time{}
}

// Top returns up to n contenders for the dimension, highest first. The first CPU
// or I/O tick after arming returns no values (no baseline yet) but records one.
func (s *Sampler) Top(dim contract.Dimension, n int, now time.Time) []dots.Contender {
	switch dim {
	case contract.DimensionCPU, contract.DimensionScheduling:
		return s.topCPU(n, now)
	case contract.DimensionMemory:
		return s.topMem(n)
	case contract.DimensionIO:
		return s.topIO(n, now)
	default:
		return nil
	}
}

func (s *Sampler) listPIDs() []int {
	f, err := os.Open(s.procRoot)
	if err != nil {
		return nil
	}
	names, _ := f.Readdirnames(-1)
	_ = f.Close()
	out := make([]int, 0, len(names))
	for _, nm := range names {
		if pid, ok := numericPID(nm); ok {
			out = append(out, pid)
			if len(out) >= maxPIDs {
				break
			}
		}
	}
	return out
}

// numericPID parses an all-digits directory name into a positive pid.
func numericPID(name string) (int, bool) {
	if name == "" {
		return 0, false
	}
	v := 0
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		v = v*10 + int(c-'0')
	}
	if v <= 0 {
		return 0, false
	}
	return v, true
}

func (s *Sampler) readStat(pid int) (procfs.PidStat, bool) {
	data := readInto(s.procRoot+"/"+itoa(pid)+"/stat", s.buf)
	if data == nil {
		return procfs.PidStat{}, false
	}
	p, err := procfs.ParsePidStat(data)
	if err != nil {
		return procfs.PidStat{}, false
	}
	return p, true
}

func (s *Sampler) topCPU(n int, now time.Time) []dots.Contender {
	dt := now.Sub(s.lastCPUAt).Seconds()
	havePrev := s.prevCPU != nil
	s.lastCPUAt = now
	cur := make(map[int]uint64, len(s.prevCPU))
	var rows []dots.Contender
	for _, pid := range s.listPIDs() {
		p, ok := s.readStat(pid)
		if !ok {
			continue
		}
		ticks := p.Utime + p.Stime
		cur[pid] = ticks
		if !havePrev || dt <= 0 {
			continue
		}
		prev, ok := s.prevCPU[pid]
		if !ok || ticks < prev {
			continue
		}
		pct := float64(ticks-prev) / float64(procfs.ClockTicks()) / dt * 100
		if pct > 0 {
			rows = append(rows, dots.Contender{PID: pid, Comm: p.Comm, Value: pct, Unit: "percent"})
		}
	}
	s.prevCPU = cur
	return topN(rows, n)
}

func (s *Sampler) topMem(n int) []dots.Contender {
	var rows []dots.Contender
	for _, pid := range s.listPIDs() {
		p, ok := s.readStat(pid)
		if !ok || p.RSS <= 0 {
			continue
		}
		rows = append(rows, dots.Contender{PID: pid, Comm: p.Comm, Value: float64(p.RSS * s.pageSize), Unit: "bytes"})
	}
	return topN(rows, n)
}

func (s *Sampler) topIO(n int, now time.Time) []dots.Contender {
	dt := now.Sub(s.lastIOAt).Seconds()
	havePrev := s.prevIO != nil
	s.lastIOAt = now
	cur := make(map[int]uint64, len(s.prevIO))
	var rows []dots.Contender
	for _, pid := range s.listPIDs() {
		data := readInto(s.procRoot+"/"+itoa(pid)+"/io", s.buf)
		if data == nil {
			continue
		}
		io, err := procfs.ParsePidIO(data)
		if err != nil {
			continue
		}
		bytes := io.ReadBytes + io.WriteBytes
		cur[pid] = bytes
		if !havePrev || dt <= 0 {
			continue
		}
		prev, ok := s.prevIO[pid]
		if !ok || bytes < prev {
			continue
		}
		bps := float64(bytes-prev) / dt
		if bps > 0 {
			p, _ := s.readStat(pid)
			rows = append(rows, dots.Contender{PID: pid, Comm: p.Comm, Value: bps, Unit: "bytes_per_sec"})
		}
	}
	s.prevIO = cur
	return topN(rows, n)
}

func topN(rows []dots.Contender, n int) []dots.Contender {
	sort.Slice(rows, func(i, j int) bool { return rows[i].Value > rows[j].Value })
	if n > 0 && len(rows) > n {
		rows = rows[:n]
	}
	return rows
}

func readInto(path string, buf []byte) []byte {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	n, _ := f.Read(buf)
	_ = f.Close()
	if n <= 0 {
		return nil
	}
	return buf[:n]
}

// itoa avoids strconv import churn for small positive ints.
func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}
