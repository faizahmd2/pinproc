// Package journal cuts the few log lines that matter out of the system journal
// and the kernel ring buffer for an incident window. It reads; it never writes,
// rotates or configures logging. Unit logs come from `journalctl -o json` (the
// journal's own reader, bounded by a timeout); kernel messages are read straight
// from /dev/kmsg so they are available even when journald is struggling.
package journal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Line is one (possibly collapsed) log line.
type Line struct {
	At       time.Time `json:"at"`
	Unit     string    `json:"unit,omitempty"`
	Ident    string    `json:"ident,omitempty"` // SYSLOG_IDENTIFIER / comm
	PID      int       `json:"pid,omitempty"`
	Priority int       `json:"priority"` // syslog level: 0 emerg … 3 err, 4 warning … 7 debug
	Message  string    `json:"message"`
	Count    int       `json:"count,omitempty"` // >1 when identical lines were collapsed
}

// Section is the excerpt from one source.
type Section struct {
	Title string `json:"title"`
	Unit  string `json:"unit,omitempty"`
	Cmd   string `json:"cmd"`             // the command that shows the full log
	Lines []Line `json:"lines,omitempty"` // oldest first
	Note  string `json:"note,omitempty"`
}

// Excerpt is everything cut for one window.
type Excerpt struct {
	Since    time.Time `json:"since"`
	Until    time.Time `json:"until"`
	Sections []Section `json:"sections,omitempty"`
	Notes    []string  `json:"notes,omitempty"`
}

// Query describes the window and the units of interest.
type Query struct {
	Units    []string // systemd units, most relevant first (at most 3 are read)
	Since    time.Time
	Until    time.Time
	Boot     time.Time // for /dev/kmsg timestamps
	PerUnit  int       // lines kept per unit (default 10)
	Timeout  time.Duration
	Kmsg     string // default /dev/kmsg
	NoOthers bool   // skip the host-wide error query
}

// Empty reports whether the excerpt holds no lines at all.
func (e *Excerpt) Empty() bool {
	if e == nil {
		return true
	}
	for _, s := range e.Sections {
		if len(s.Lines) > 0 {
			return false
		}
	}
	return true
}

// Collect runs the unit, kernel and host-wide reads concurrently and returns
// whatever finished inside the timeout.
func Collect(ctx context.Context, q Query) *Excerpt {
	if q.PerUnit <= 0 {
		q.PerUnit = 10
	}
	if q.Timeout <= 0 {
		q.Timeout = 4 * time.Second
	}
	if q.Kmsg == "" {
		q.Kmsg = "/dev/kmsg"
	}
	if q.Until.IsZero() {
		q.Until = time.Now()
	}
	ctx, cancel := context.WithTimeout(ctx, q.Timeout)
	defer cancel()
	ex := &Excerpt{Since: q.Since.UTC(), Until: q.Until.UTC()}
	units := uniq(q.Units)
	if len(units) > 3 {
		units = units[:3]
	}
	_, lookErr := exec.LookPath("journalctl")
	sections := make([]*Section, len(units)+2)
	var wg sync.WaitGroup
	if lookErr == nil {
		for i, u := range units {
			wg.Add(1)
			go func(i int, u string) {
				defer wg.Done()
				sections[i] = unitSection(ctx, u, q, i == 0)
			}(i, u)
		}
		if !q.NoOthers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				sections[len(units)+1] = othersSection(ctx, units, q)
			}()
		}
	} else if len(units) > 0 {
		ex.Notes = append(ex.Notes, "journalctl not found — unit logs unavailable on this host")
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		sections[len(units)] = kernelSection(q)
	}()
	wg.Wait()
	if o := sections[len(units)+1]; o != nil {
		shown := map[string]bool{}
		for _, s := range sections[:len(units)] {
			if s != nil {
				for _, l := range s.Lines {
					shown[numRe.ReplaceAllString(l.Message, "#")] = true
				}
			}
		}
		var keep []Line
		for _, l := range o.Lines {
			if !shown[numRe.ReplaceAllString(l.Message, "#")] {
				keep = append(keep, l)
			}
		}
		o.Lines = keep
	}
	for _, s := range sections {
		if s != nil && (len(s.Lines) > 0 || s.Note != "") {
			ex.Sections = append(ex.Sections, *s)
		}
	}
	return ex
}

func uniq(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// windowArgs renders a window the way a human would type it, in UTC.
func windowArgs(q Query) string {
	f := "2006-01-02 15:04:05"
	return fmt.Sprintf("--since \"%s UTC\" --until \"%s UTC\"", q.Since.UTC().Format(f), q.Until.UTC().Format(f))
}

func journalctl(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "journalctl", append([]string{"--no-pager", "-q", "-o", "json"}, args...)...)
	cmd.Env = []string{"LANG=C", "SYSTEMD_PAGER=cat"}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	b, _ := io.ReadAll(io.LimitReader(out, 4<<20))
	_ = cmd.Wait()
	return b, ctx.Err()
}

func since(q Query) []string {
	return []string{"--since", "@" + strconv.FormatInt(q.Since.Unix(), 10), "--until", "@" + strconv.FormatInt(q.Until.Unix()+1, 10)}
}

// unitSection cuts one unit's lines. Only the first (the cause) falls back to
// its last lines when it logged nothing alarming; for the others silence is
// simply left out.
func unitSection(ctx context.Context, unit string, q Query, primary bool) *Section {
	s := &Section{Title: unit, Unit: unit, Cmd: "journalctl -u " + unit + " " + windowArgs(q)}
	b, err := journalctl(ctx, append([]string{"-u", unit, "-n", "400"}, since(q)...)...)
	lines := parseJSON(b)
	if err != nil && len(lines) == 0 {
		s.Note = "journal read timed out"
		return s
	}
	if len(lines) == 0 {
		if !primary {
			return nil
		}
		s.Note = "no log lines in this window"
		return s
	}
	picked := collapse(filter(lines, interesting), q.PerUnit)
	if len(picked) == 0 && !primary {
		return nil
	}
	if len(picked) == 0 {
		s.Note = "no warnings or errors — last lines shown"
		picked = collapse(lines, 4)
	}
	s.Lines = picked
	return s
}

func othersSection(ctx context.Context, skip []string, q Query) *Section {
	s := &Section{Title: "Other errors on this host", Cmd: "journalctl -p err " + windowArgs(q)}
	b, _ := journalctl(ctx, append([]string{"-p", "err", "-n", "300"}, since(q)...)...)
	skipSet := map[string]bool{}
	for _, u := range skip {
		skipSet[u] = true
	}
	var keep []Line
	for _, l := range parseJSON(b) {
		if skipSet[l.Unit] || l.Ident == "kernel" {
			continue
		}
		keep = append(keep, l)
	}
	s.Lines = collapse(keep, 8)
	return s
}

type entry struct {
	Realtime string          `json:"__REALTIME_TIMESTAMP"`
	Priority string          `json:"PRIORITY"`
	Message  json.RawMessage `json:"MESSAGE"`
	Unit     string          `json:"_SYSTEMD_UNIT"`
	ObjUnit  string          `json:"UNIT"`
	Ident    string          `json:"SYSLOG_IDENTIFIER"`
	Comm     string          `json:"_COMM"`
	PID      string          `json:"_PID"`
}

func parseJSON(b []byte) []Line {
	var out []Line
	for _, raw := range bytes.Split(b, []byte{'\n'}) {
		if len(bytes.TrimSpace(raw)) == 0 {
			continue
		}
		var e entry
		if json.Unmarshal(raw, &e) != nil {
			continue
		}
		l := Line{Message: message(e.Message), Unit: e.Unit, Ident: e.Ident}
		if l.Unit == "" {
			l.Unit = e.ObjUnit
		}
		if l.Ident == "" {
			l.Ident = e.Comm
		}
		l.PID, _ = strconv.Atoi(e.PID)
		l.Priority = 6
		if p, err := strconv.Atoi(e.Priority); err == nil {
			l.Priority = p
		}
		if us, err := strconv.ParseInt(e.Realtime, 10, 64); err == nil {
			l.At = time.UnixMicro(us).UTC()
		}
		if l.Message != "" {
			out = append(out, l)
		}
	}
	return out
}

// message decodes MESSAGE, which journald emits as a byte array when the text is
// not valid UTF-8.
func message(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return clean(s)
	}
	var bs []byte
	var ints []int
	if json.Unmarshal(raw, &ints) == nil {
		for _, i := range ints {
			bs = append(bs, byte(i))
		}
		return clean(strings.ToValidUTF8(string(bs), "?"))
	}
	return ""
}

func clean(s string) string {
	s = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return ' '
		}
		if r < 0x20 {
			return -1
		}
		return r
	}, s))
	if len(s) > 300 {
		s = s[:299] + "…"
	}
	return s
}

var badWords = regexp.MustCompile(`(?i)\b(error|errors|fatal|panic|exception|traceback|fail|failed|failure|timeout|timed out|refused|denied|killed|oom|out of memory|unavailable|cannot|could not|too many|exhausted|deadlock|unreachable|abort|aborted|crash|crashed|segfault|core dumped|reset by peer|broken pipe)\b`)

func interesting(l Line) bool { return l.Priority <= 4 || badWords.MatchString(l.Message) }

func filter(in []Line, keep func(Line) bool) []Line {
	var out []Line
	for _, l := range in {
		if keep(l) {
			out = append(out, l)
		}
	}
	return out
}

var numRe = regexp.MustCompile(`0x[0-9a-fA-F]+|[0-9a-fA-F]{8,}|\d+`)

// collapse merges lines that differ only in numbers/ids (keeping the latest one
// with a count) and returns the newest n groups, oldest first.
func collapse(in []Line, n int) []Line {
	type group struct {
		line  Line
		count int
		last  int
	}
	groups := map[string]*group{}
	var order []string
	for i, l := range in {
		key := l.Unit + "|" + numRe.ReplaceAllString(l.Message, "#")
		g := groups[key]
		if g == nil {
			g = &group{}
			groups[key] = g
			order = append(order, key)
		}
		g.line, g.last = l, i
		g.count++
	}
	out := make([]*group, 0, len(order))
	for _, k := range order {
		out = append(out, groups[k])
	}
	sort.Slice(out, func(i, j int) bool { return out[i].last < out[j].last })
	if len(out) > n {
		out = out[len(out)-n:]
	}
	res := make([]Line, 0, len(out))
	for _, g := range out {
		l := g.line
		if g.count > 1 {
			l.Count = g.count
		}
		res = append(res, l)
	}
	return res
}

var kernelWords = regexp.MustCompile(`(?i)(out of memory|oom|killed process|hung_task|blocked for more than|soft lockup|hard lockup|rcu.*stall|i/o error|blk_update_request|buffer i/o error|ext4-fs (error|warning)|xfs.*(error|corrupt)|btrfs.*error|nvme.*(timeout|reset|abort)|segfault|general protection|call trace|throttl|link is down|nf_conntrack: table full|syn flooding|tcp: out of memory|memory cgroup out of memory|page allocation failure)`)

func kernelSection(q Query) *Section {
	s := &Section{Title: "Kernel", Cmd: "journalctl -k " + windowArgs(q)}
	recs, err := readKmsg(q.Kmsg)
	if err != nil {
		s.Note = "kernel log unreadable (" + err.Error() + ") — needs root or CAP_SYSLOG"
		return s
	}
	var keep []Line
	for _, r := range recs {
		at := q.Boot.Add(time.Duration(r.usec) * time.Microsecond)
		if !q.Boot.IsZero() && (at.Before(q.Since.Add(-time.Second)) || at.After(q.Until.Add(time.Second))) {
			continue
		}
		l := Line{At: at.UTC(), Ident: "kernel", Priority: r.level, Message: clean(r.msg)}
		if l.Priority <= 3 || kernelWords.MatchString(l.Message) {
			keep = append(keep, l)
		}
	}
	s.Lines = collapse(keep, 10)
	return s
}

type kmsgRecord struct {
	level int
	usec  int64
	msg   string
}

// parseKmsg parses one /dev/kmsg record: "pri,seq,usec,flags;message".
func parseKmsg(rec []byte) (kmsgRecord, bool) {
	head, body, ok := bytes.Cut(rec, []byte{';'})
	if !ok {
		return kmsgRecord{}, false
	}
	f := strings.Split(string(head), ",")
	if len(f) < 3 {
		return kmsgRecord{}, false
	}
	pri, err1 := strconv.Atoi(f[0])
	usec, err2 := strconv.ParseInt(f[2], 10, 64)
	if err1 != nil || err2 != nil {
		return kmsgRecord{}, false
	}
	if i := bytes.IndexByte(body, '\n'); i >= 0 {
		body = body[:i] // drop the " KEY=value" dictionary continuation lines
	}
	return kmsgRecord{level: pri & 7, usec: usec, msg: string(body)}, true
}
