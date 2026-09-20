package enrich

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/faizahmd2/pinproc/internal/contract"
	"github.com/faizahmd2/pinproc/internal/source"
	"github.com/joshdurbin/drain3"
)

var severityRe = regexp.MustCompile("(?i)\\b(ERROR|FATAL|PANIC|Exception|Traceback)\\b")
var oomRe = regexp.MustCompile("Killed process ([0-9]+) \\(([^)]*)\\)")

type linePick struct {
	line     string
	count    int
	order    int
	template string
}

func Attach(ctx context.Context, src source.Source, hyps []contract.Hypothesis) []contract.Notice {
	var notices []contract.Notice
	kmsgNeeded := false
	for _, h := range hyps {
		if h.Source == "rule:mem.oom_recent" {
			kmsgNeeded = true
			break
		}
	}
	var kmsgLines string
	kmsgChecked := false
	for i := range hyps {
		if err := ctx.Err(); err != nil {
			break
		}
		svc := hyps[i].Entity.Service
		if svc == nil || len(svc.LogPaths) == 0 {
			if hyps[i].Source == "rule:mem.oom_recent" && kmsgNeeded {
				if !kmsgChecked {
					kmsgChecked = true
					if src != nil {
						s, err := src.Snapshot(ctx, []source.Read{{Key: "kmsg", Path: "/dev/kmsg", Kind: source.ReadKmsg, MaxBytes: 64 << 10, Optional: true}})
						if err == nil && len(s.Reads["kmsg"]) > 0 {
							raw := s.Reads["kmsg"][0]
							kmsgLines = string(raw.Data)
							if raw.Err != nil && len(kmsgLines) == 0 {
								notices = append(notices, contract.Notice{Capability: "logs.kernel", Message: "kernel log unavailable — " + raw.Err.Error() + ". Run as root to enable OOM evidence.", Count: 1})
							}
						}
					}
				}
				if path, line := matchOOM(kmsgLines, hyps[i].Entity.ID); path != "" {
					hyps[i].LogContext = &contract.LogContext{Path: path, Line: line}
				}
			}
			continue
		}
		if hpath, line, count := bestLine(ctx, svc.LogPaths); hpath != "" {
			hyps[i].LogContext = &contract.LogContext{Path: hpath, Line: line, Count: count}
		}
		if hyps[i].Source == "rule:mem.oom_recent" && !kmsgChecked {
			kmsgChecked = true
			if src != nil {
				s, err := src.Snapshot(ctx, []source.Read{{Key: "kmsg", Path: "/dev/kmsg", Kind: source.ReadKmsg, MaxBytes: 64 << 10, Optional: true}})
				if err == nil && len(s.Reads["kmsg"]) > 0 {
					raw := s.Reads["kmsg"][0]
					kmsgLines = string(raw.Data)
					if raw.Err != nil && len(kmsgLines) == 0 {
						notices = append(notices, contract.Notice{Capability: "logs.kernel", Message: "kernel log unavailable — " + raw.Err.Error(), Count: 1})
					}
				}
			}
			if path, line := matchOOM(kmsgLines, hyps[i].Entity.ID); path != "" {
				hyps[i].LogContext = &contract.LogContext{Path: path, Line: line}
			}
		}
	}
	return notices
}

func readTailBounded(ctx context.Context, path string, limit int64, timeout time.Duration) ([]byte, error) {
	type result struct { data []byte; err error }
	ch := make(chan result, 1)
	go func() {
		f, err := os.Open(path)
		if err != nil { ch <- result{err: err}; return }
		defer f.Close()
		info, err := f.Stat()
		if err != nil { ch <- result{err: err}; return }
		start := info.Size() - limit
		if start < 0 { start = 0 }
		if _, err = f.Seek(start, io.SeekStart); err != nil { ch <- result{err: err}; return }
		data, err := io.ReadAll(io.LimitReader(f, limit))
		ch <- result{data: data, err: err}
	}()
	timer := time.NewTimer(timeout); defer timer.Stop()
	select {
	case r := <-ch: return r.data, r.err
	case <-ctx.Done(): return nil, ctx.Err()
	case <-timer.C: return nil, fmt.Errorf("log read timed out after %s", timeout)
	}
}

func bestLine(ctx context.Context, paths []string) (string, string, int) {
	for _, path := range paths {
		if err := ctx.Err(); err != nil { return "", "", 0 }
		data, err := readTailBounded(ctx, path, 32<<10, 2*time.Second)
		if err != nil { continue }
		var miner *drain3.TemplateMiner
		miner, err = drain3.New()
		if err != nil { continue }
		picks := map[string]*linePick{}
		order := 0
		lastNonEmpty := ""
		sc := bufio.NewScanner(strings.NewReader(string(data)))
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" { continue }
			lastNonEmpty = line; order++
			res := miner.AddLogMessage(line)
			if res.Cluster == nil || !severityRe.MatchString(line) { continue }
			key := res.Cluster.GetTemplate(); p := picks[key]
			if p == nil { p = &linePick{template:key}; picks[key] = p }
			p.count = res.Cluster.Size; p.line = line; p.order = order
		}
		var best *linePick
		for _, p := range picks { if best == nil || p.count > best.count || (p.count == best.count && p.order > best.order) { best = p } }
		if best != nil { return path, truncate(best.line, 200), best.count }
		if lastNonEmpty != "" { return path, truncate(lastNonEmpty, 200), 1 }
	}
	return "", "", 0
}
func matchOOM(data, entityID string) (string, string) {
	pid := strings.TrimPrefix(entityID, "pid:")
	if _, err := strconv.Atoi(pid); err != nil {
		return "", ""
	}
	for _, line := range strings.Split(data, "\n") {
		m := oomRe.FindStringSubmatch(line)
		if len(m) > 1 && m[1] == pid {
			return "/dev/kmsg", truncate(line, 200)
		}
	}
	return "", ""
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}
