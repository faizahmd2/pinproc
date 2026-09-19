package source

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// BuildScript creates one bounded read script for SSH transport.
func BuildScript(reads []Read, phaseT1 bool, windowSeconds int) string {
	sort.SliceStable(reads, func(i, j int) bool {
		if reads[i].Key != reads[j].Key {
			return reads[i].Key < reads[j].Key
		}
		return reads[i].Path < reads[j].Path
	})
	var b strings.Builder
	b.WriteString("LC_ALL=C\n")
	b.WriteString("emit(){ printf '\\n--DGN|%s|%s|B--\\n' \"$1\" \"$2\"; cat -- \"$2\" 2>/dev/null; printf '\\n--DGN|%s|E--\\n' \"$1\"; }\n")
	b.WriteString("emit_link(){ printf '\\n--DGN|%s|%s|B--\\n' \"$1\" \"$2\"; readlink -- \"$2\" 2>/dev/null; printf '\\n--DGN|%s|E--\\n' \"$1\"; }\n")
	for _, r := range reads {
		b.WriteString(emitLine(r, "T0"))
	}
	if phaseT1 {
		if windowSeconds < 1 {
			windowSeconds = 1
		}
		b.WriteString(fmt.Sprintf("sleep %d\\n", windowSeconds))
		for _, r := range reads {
			b.WriteString(emitLine(r, "T1"))
		}
	}
	return b.String()
}

func emitLine(r Read, phase string) string {
	key := phase + ":" + r.Key
	switch r.Kind {
	case ReadGlobLinks:
		return fmt.Sprintf("for f in %s; do emit_link '%s' \"$f\"; done\\n", r.Path, key)
	case ReadGlob, ReadDirNames:
		return fmt.Sprintf("for f in %s; do emit '%s' \"$f\"; done\\n", r.Path, key)
	default:
		return fmt.Sprintf("emit '%s' '%s'\\n", key, r.Path)
	}
}

// ParseScriptOutput demultiplexes sentinel-framed output.
func ParseScriptOutput(out string, maxBytes int64) Snapshot {
	s := Snapshot{At: time.Now(), Reads: map[string][]Raw{}}
	var cur *Raw
	ended := true
	total := int64(0)
	flush := func(truncated bool) {
		if cur == nil {
			return
		}
		if truncated {
			cur.Err = errors.New("framed read truncated")
		}
		s.Reads[cur.Key] = append(s.Reads[cur.Key], *cur)
		total += int64(len(cur.Data))
		cur = nil
		ended = true
	}
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "--DGN|") && strings.HasSuffix(line, "|B--"):
			flush(!ended)
			inside := strings.TrimSuffix(strings.TrimPrefix(line, "--DGN|"), "|B--")
			parts := strings.SplitN(inside, "|", 2)
			if len(parts) == 2 {
				cur = &Raw{Key: parts[0], Path: parts[1]}
				ended = false
			}
		case strings.HasPrefix(line, "--DGN|") && strings.HasSuffix(line, "|E--"):
			flush(false)
		case cur != nil:
			add := len(line) + 1
			if maxBytes <= 0 || total+int64(add) <= maxBytes {
				cur.Data = append(cur.Data, line...)
				cur.Data = append(cur.Data, '\n')
			}
		}
	}
	if !ended {
		flush(true)
	}
	s.Bytes = total
	return s
}
