package source

import (
	"context"
	"errors"
	"fmt"
	"github.com/faizahmd2/pinproc/internal/contract"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Local reads Linux procfs/sysfs directly without fork/exec.
type Local struct {
	procRoot, sysRoot string
	bufPool           sync.Pool
	maxBytes          int
	readTimeout       time.Duration
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
	s := &Local{procRoot: procRoot, sysRoot: sysRoot, maxBytes: maxBytes, readTimeout: 2 * time.Second}
	s.bufPool.New = func() any { return make([]byte, 32*1024) }
	return s
}

// NewLocalWithTimeout returns a local source with a configured risky-read timeout.
func NewLocalWithTimeout(procRoot, sysRoot string, maxBytes int, timeout time.Duration) *Local {
	s := NewLocal(procRoot, sysRoot, maxBytes)
	if timeout > 0 {
		s.readTimeout = timeout
	}
	return s
}

// Name returns the source name.
func (s *Local) Name() string { return "local" }

// StartupCheck validates the minimum host contract before an investigation begins.
func (s *Local) StartupCheck() error {
	f, err := os.Open(filepath.Join(s.procRoot, "stat"))
	if err != nil {
		return fmt.Errorf("proc filesystem unavailable: %w", err)
	}
	defer f.Close()
	var one [1]byte
	if _, err := f.Read(one[:]); err != nil {
		return fmt.Errorf("proc filesystem unreadable: %w", err)
	}
	return nil
}

// Facts reads cheap host capability information.
func (s *Local) Facts(ctx context.Context) (contract.Facts, error) {
	b := s.readSmallBounded(ctx, filepath.Join(s.procRoot, "version"), 64<<10)
	r := s.readSmallBounded(ctx, "/etc/os-release", 64<<10)
	f := contract.Facts{Kernel: strings.TrimSpace(string(b)), Root: os.Geteuid() == 0, Has: map[string]bool{"proc": true}}
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
	snapshotTimeout := 10 * time.Second
	if s.readTimeout > 0 && 3*s.readTimeout > snapshotTimeout {
		snapshotTimeout = 3 * s.readTimeout
	}
	snapshotCtx, cancel := context.WithTimeout(ctx, snapshotTimeout)
	defer cancel()
	buf := s.bufPool.Get().([]byte)
	defer s.bufPool.Put(buf[:0])
	for _, r := range reads {
		select {
		case <-snapshotCtx.Done():
			return out, snapshotCtx.Err()
		default:
		}
		items, e := s.expandReadBounded(snapshotCtx, r)
		if e != nil {
			if r.Optional {
				continue
			}
			return out, e
		}
		for _, p := range items {
			select {
			case <-snapshotCtx.Done():
				return out, snapshotCtx.Err()
			default:
			}
			x := s.readOneBounded(snapshotCtx, r, p)
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

func (s *Local) expandReadBounded(ctx context.Context, r Read) ([]string, error) {
	type result struct {
		items []string
		err   error
	}
	ch := make(chan result, 1)
	go func() { items, err := s.expandRead(r); ch <- result{items, err} }()
	select {
	case res := <-ch:
		return res.items, res.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *Local) readOneBounded(ctx context.Context, r Read, p string) Raw {
	ch := make(chan Raw, 1)
	go func() { ch <- s.readOne(r, p, make([]byte, 32*1024)) }()
	timeout := s.readTimeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case raw := <-ch:
		return raw
	case <-ctx.Done():
		return Raw{Key: r.Key, Path: p, Err: ctx.Err()}
	case <-timer.C:
		return Raw{Key: r.Key, Path: p, Err: fmt.Errorf("read timed out after %s (path may be on a hung filesystem)", timeout)}
	}
}

func (s *Local) readOne(r Read, p string, buf []byte) Raw {
	if r.Kind == ReadKmsg {
		return s.readKmsg(p, r.MaxBytes)
	}
	if isRisky(r.Kind, r.Key, p) {
		return s.readWithDeadline(r, p, s.readTimeout)
	}
	raw := Raw{Key: r.Key, Path: p}
	limit := r.MaxBytes
	if limit <= 0 {
		limit = s.maxBytes
	}
	if r.Kind == ReadLink || r.Kind == ReadGlobLinks {
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

func isRisky(kind ReadKind, key, path string) bool {
	if kind == ReadLink || kind == ReadGlobLinks {
		return true
	}
	return strings.Contains(key, "smaps") || strings.Contains(path, "/maps") || strings.Contains(path, "/smaps")
}

func (s *Local) readWithDeadline(r Read, p string, timeout time.Duration) Raw {
	type result struct {
		data []byte
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		if r.Kind == ReadLink || r.Kind == ReadGlobLinks {
			v, e := os.Readlink(p)
			ch <- result{data: []byte(v), err: e}
			return
		}
		f, e := os.Open(p)
		if e != nil {
			ch <- result{err: e}
			return
		}
		defer f.Close()
		data, e := io.ReadAll(io.LimitReader(f, int64(maxReadBytes(r, s.maxBytes))))
		ch <- result{data: data, err: e}
	}()
	select {
	case res := <-ch:
		return Raw{Key: r.Key, Path: p, Data: res.data, Err: res.err}
	case <-time.After(timeout):
		return Raw{Key: r.Key, Path: p, Err: fmt.Errorf("read timed out after %s (path may be on a hung filesystem)", timeout)}
	}
}

func maxReadBytes(r Read, fallback int) int {
	if r.MaxBytes > 0 {
		return r.MaxBytes
	}
	return fallback
}

func (s *Local) readKmsg(p string, maxBytes int) Raw {
	raw := Raw{Key: "kmsg", Path: p}
	limit := maxReadBytes(Read{MaxBytes: maxBytes}, s.maxBytes)
	f, err := os.OpenFile(p, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		raw.Err = err
		return raw
	}
	defer f.Close()
	deadline := time.Now().Add(500 * time.Millisecond)
	lines := 0
	buf := make([]byte, 8192)
	for len(raw.Data) < limit && lines < 500 && time.Now().Before(deadline) {
		n, e := f.Read(buf)
		if n > 0 {
			remain := limit - len(raw.Data)
			if n > remain {
				n = remain
			}
			raw.Data = append(raw.Data, buf[:n]...)
			lines += bytesCountNewlines(buf[:n])
		}
		if e != nil {
			if errors.Is(e, syscall.EAGAIN) || errors.Is(e, syscall.EWOULDBLOCK) {
				break
			}
			raw.Err = e
			break
		}
	}
	return raw
}
func bytesCountNewlines(b []byte) int {
	n := 0
	for _, x := range b {
		if x == '\n' {
			n++
		}
	}
	return n
}

func (s *Local) expandRead(r Read) ([]string, error) {
	switch r.Kind {
	case ReadFile, ReadLink:
		return []string{s.translate(r.Path)}, nil
	case ReadKmsg:
		return []string{r.Path}, nil
	case ReadGlobLinks:
		return s.expandGlob(r.Path)
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
	maxItems := 8192
	if strings.Contains(pattern, "/fd/") {
		maxItems = 256
	}
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
						if len(next) >= maxItems {
							break
						}
					}
				}
			} else {
				next = append(next, filepath.Join(base, part))
			}
			if len(next) >= maxItems {
				break
			}
		}
		cur = next
		if len(cur) == 0 || len(cur) >= maxItems {
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
func (s *Local) readSmallBounded(ctx context.Context, path string, limit int) []byte {
	ch := make(chan []byte, 1)
	go func() { ch <- readSmall(path, limit) }()
	t := time.NewTimer(2 * time.Second)
	defer t.Stop()
	select {
	case b := <-ch:
		return b
	case <-ctx.Done():
		return nil
	case <-t.C:
		return nil
	}
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
