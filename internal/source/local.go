package source

import (
	"context"
	"errors"
	"fmt"
	"github.com/faizahmd2/diagnos/internal/contract"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Local reads Linux procfs/sysfs directly without fork/exec.
type Local struct {
	procRoot, sysRoot string
	bufPool           sync.Pool
	maxBytes          int
}

// NewLocal returns a local source with configurable roots.
func NewLocal(procRoot, sysRoot string, maxBytes int) *Local {
	if procRoot == "" {
		procRoot = "/proc"
	}
	if sysRoot == "" {
		sysRoot = "/sys"
	}
	if maxBytes <= 0 {
		maxBytes = 1 << 20
	}
	s := &Local{procRoot: procRoot, sysRoot: sysRoot, maxBytes: maxBytes}
	s.bufPool.New = func() any { return make([]byte, 32*1024) }
	return s
}

// Name returns the source name.
func (s *Local) Name() string { return "local" }

// Facts reads cheap host capability information.
func (s *Local) Facts(ctx context.Context) (contract.Facts, error) {
	b := readSmall(filepath.Join(s.procRoot, "version"), 64<<10)
	r := readSmall("/etc/os-release", 64<<10)
	f := contract.Facts{Kernel: strings.TrimSpace(string(b)), Has: map[string]bool{"proc": true}}
	for _, l := range strings.Split(string(r), "\n") {
		if strings.HasPrefix(l, "ID=") {
			f.OSID = strings.Trim(strings.TrimPrefix(l, "ID="), "\"")
		}
		if strings.HasPrefix(l, "ID_LIKE=") {
			f.OSLike = strings.Fields(strings.Trim(strings.TrimPrefix(l, "ID_LIKE="), "\""))
		}
	}
	if _, e := os.Stat(filepath.Join(s.sysRoot, "fs/cgroup/cgroup.controllers")); e == nil {
		f.CgroupV2 = true
	}
	if _, e := os.Stat(filepath.Join(s.procRoot, "pressure")); e == nil {
		f.PSI = true
	}
	f.Has["cgroup_v2"] = f.CgroupV2
	f.Has["psi"] = f.PSI
	select {
	case <-ctx.Done():
		return f, ctx.Err()
	default:
		return f, nil
	}
}

// Snapshot collects bounded reads.
func (s *Local) Snapshot(ctx context.Context, reads []Read) (Snapshot, error) {
	out := Snapshot{At: time.Now(), Reads: map[string][]Raw{}}
	buf := s.bufPool.Get().([]byte)
	defer s.bufPool.Put(buf[:0])
	for _, r := range reads {
		select {
		case <-ctx.Done():
			return out, ctx.Err()
		default:
		}
		items, e := s.expandRead(r)
		if e != nil {
			if r.Optional {
				continue
			}
			return out, e
		}
		for _, p := range items {
			x := s.readOne(r, p, buf)
			out.Reads[r.Key] = append(out.Reads[r.Key], x)
			out.Bytes += int64(len(x.Data))
		}
	}
	return out, nil
}

// Sample collects two snapshots using a timer.
func (s *Local) Sample(ctx context.Context, reads []Read, w time.Duration) (Sample, error) {
	if w <= 0 {
		w = time.Second
	}
	t0, e := s.Snapshot(ctx, reads)
	if e != nil {
		return Sample{}, e
	}
	timer := time.NewTimer(w)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return Sample{}, ctx.Err()
	case <-timer.C:
	}
	t1, e := s.Snapshot(ctx, reads)
	if e != nil {
		return Sample{}, e
	}
	return Sample{T0: t0, T1: t1, Window: t1.At.Sub(t0.At)}, nil
}

// Close releases resources.
func (s *Local) Close() error { return nil }

func (s *Local) readOne(r Read, p string, buf []byte) Raw {
	raw := Raw{Key: r.Key, Path: p}
	limit := r.MaxBytes
	if limit <= 0 {
		limit = s.maxBytes
	}
	if r.Kind == ReadLink {
		v, e := os.Readlink(p)
		if e != nil {
			raw.Err = e
		} else {
			raw.Data = []byte(v)
		}
		return raw
	}
	f, e := os.Open(p)
	if e != nil {
		raw.Err = e
		return raw
	}
	defer f.Close()
	nread := 0
	for nread < limit {
		n, e := f.Read(buf)
		if n > 0 {
			if nread+n > limit {
				n = limit - nread
			}
			raw.Data = append(raw.Data, buf[:n]...)
			nread += n
		}
		if e != nil {
			if e != io.EOF {
				raw.Err = e
			}
			break
		}
	}
	if nread >= limit {
		raw.Err = fmt.Errorf("read truncated at %d bytes", limit)
	}
	return raw
}

func (s *Local) expandRead(r Read) ([]string, error) {
	switch r.Kind {
	case ReadFile, ReadLink, ReadGlobLinks:
		return []string{s.translate(r.Path)}, nil
	case ReadDirNames:
		dir := s.translate(r.Path)
		es, e := os.ReadDir(dir)
		if e != nil {
			return nil, e
		}
		o := make([]string, 0, len(es))
		for _, x := range es {
			o = append(o, filepath.Join(dir, x.Name()))
		}
		return o, nil
	case ReadGlob:
		return s.expandGlob(r.Path)
	default:
		return nil, errors.New("unknown read kind")
	}
}
func (s *Local) expandGlob(pattern string) ([]string, error) {
	pattern = s.translate(pattern)
	parts := strings.Split(filepath.Clean(pattern), string(os.PathSeparator))
	cur := []string{string(os.PathSeparator)}
	for i := 1; i < len(parts); i++ {
		next := make([]string, 0)
		for _, base := range cur {
			part := parts[i]
			if strings.ContainsAny(part, "*?[") {
				es, e := os.ReadDir(base)
				if e != nil {
					continue
				}
				for _, x := range es {
					if match(part, x.Name()) {
						next = append(next, filepath.Join(base, x.Name()))
					}
				}
			} else {
				next = append(next, filepath.Join(base, part))
			}
		}
		cur = next
		if len(cur) == 0 {
			break
		}
	}
	return cur, nil
}
func match(p, n string) bool {
	if p == "*" {
		return true
	}
	if p == "[0-9]*" {
		return len(n) > 0 && n[0] >= '0' && n[0] <= '9'
	}
	ok, e := filepath.Match(p, n)
	return e == nil && ok
}
func (s *Local) translate(p string) string {
	if p == "/proc" || strings.HasPrefix(p, "/proc/") {
		return filepath.Join(s.procRoot, strings.TrimPrefix(p, "/proc"))
	}
	if p == "/sys" || strings.HasPrefix(p, "/sys/") {
		return filepath.Join(s.sysRoot, strings.TrimPrefix(p, "/sys"))
	}
	return p
}
func readSmall(p string, limit int) []byte {
	f, e := os.Open(p)
	if e != nil {
		return nil
	}
	defer f.Close()
	b, _ := io.ReadAll(io.LimitReader(f, int64(limit)))
	return b
}

// ParseNumericPID returns a positive PID represented by a directory name.
func ParseNumericPID(name string) (int, bool) {
	v, e := strconv.Atoi(name)
	return v, e == nil && v > 0
}
