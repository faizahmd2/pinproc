package source

import (
	"context"
	"fmt"
	"github.com/faizahmd2/diagnos/internal/contract"
	"github.com/faizahmd2/diagnos/internal/transport"
	"strings"
	"time"
)

// SSH reads target procfs through one batched transport call.
type SSH struct {
	exec     transport.Executor
	maxBytes int64
	name     string
}

// NewSSH creates a remote source over a transport.
func NewSSH(exec transport.Executor, name string, maxBytes int64) *SSH {
	if maxBytes <= 0 {
		maxBytes = 8 << 20
	}
	return &SSH{exec: exec, maxBytes: maxBytes, name: name}
}

// Name returns the remote source name.
func (s *SSH) Name() string { return "ssh:" + s.name }

// Facts maps remote facts into the V2 contract.
func (s *SSH) Facts(ctx context.Context) (contract.Facts, error) {
	f, e := s.exec.Facts(ctx)
	if e != nil {
		return contract.Facts{}, e
	}
	return contract.Facts{Kernel: f.Kernel, OSID: f.OSID, OSLike: f.OSIDLike, Has: f.Has}, nil
}

// Snapshot performs one RTT-equivalent scripted read.
func (s *SSH) Snapshot(ctx context.Context, reads []Read) (Snapshot, error) {
	res := s.exec.RunScript(ctx, BuildScript(reads, false, 0), 30*time.Second, s.maxBytes)
	if res.Err != nil {
		return Snapshot{}, res.Err
	}
	if res.Truncated {
		return Snapshot{}, fmt.Errorf("remote snapshot output truncated")
	}
	all := ParseScriptOutput(res.Stdout, s.maxBytes)
	return phase(all, "T0"), nil
}

// Sample performs T0, sleep, T1 in one remote call.
func (s *SSH) Sample(ctx context.Context, reads []Read, w time.Duration) (Sample, error) {
	sec := int(w.Seconds())
	if sec < 1 {
		sec = 1
	}
	res := s.exec.RunScript(ctx, BuildScript(reads, true, sec), w+10*time.Second, s.maxBytes)
	if res.Err != nil {
		return Sample{}, res.Err
	}
	if res.Truncated {
		return Sample{}, fmt.Errorf("remote sample output truncated")
	}
	all := ParseScriptOutput(res.Stdout, s.maxBytes)
	t0 := phase(all, "T0")
	t1 := phase(all, "T1")
	return Sample{T0: t0, T1: t1, Window: w}, nil
}

// Close is a no-op; transport owns the connection.
func (s *SSH) Close() error { return nil }

func phase(all Snapshot, prefix string) Snapshot {
	out := Snapshot{At: all.At, Reads: map[string][]Raw{}}
	for k, rs := range all.Reads {
		if strings.HasPrefix(k, prefix+":") {
			key := strings.TrimPrefix(k, prefix+":")
			for _, r := range rs {
				r.Key = key
				out.Reads[key] = append(out.Reads[key], r)
			}
		}
	}
	return out
}
