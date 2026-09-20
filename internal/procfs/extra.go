package procfs

import (
	"bytes"
	"errors"
	"strconv"
	"time"
)

// MapRegion is one bounded smaps mapping.
type MapRegion struct {
	Start, End                              uint64
	Perms                                   string
	Offset                                  uint64
	Dev                                     string
	Inode                                   uint64
	Path                                    string
	Rss, Pss, PrivateDirty, Anonymous, Swap uint64
}

// ParseCmdline parses a NUL-separated command line.
func ParseCmdline(b []byte) []string {
	parts := bytes.Split(b, []byte{0})
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if len(p) > 0 {
			out = append(out, string(p))
		}
	}
	return out
}

// ParseLimits parses process soft/hard limits.
func ParseLimits(b []byte) (map[string][2]uint64, error) {
	out := map[string][2]uint64{}
	for _, l := range bytes.Split(b, []byte{'\n'}) {
		f := bytes.Fields(l)
		if len(f) < 4 {
			continue
		}
		key := string(bytes.Join(f[:len(f)-3], []byte{' '}))
		soft := limit(f[len(f)-3])
		hard := limit(f[len(f)-2])
		if soft >= 0 && hard >= 0 {
			out[key] = [2]uint64{uint64(soft), uint64(hard)}
		}
	}
	if len(out) == 0 {
		return nil, errors.New("no limits")
	}
	return out, nil
}
func limit(b []byte) int64 {
	if bytes.Equal(b, []byte("unlimited")) {
		return int64(^uint64(0) >> 1)
	}
	v, e := strconv.ParseInt(string(b), 10, 64)
	if e != nil {
		return -1
	}
	return v
}

// ParseCgroup parses /proc/<pid>/cgroup into hierarchy paths.
func ParseCgroup(b []byte) []string {
	out := []string{}
	for _, l := range bytes.Split(b, []byte{'\n'}) {
		f := bytes.SplitN(l, []byte{':'}, 3)
		if len(f) == 3 {
			out = append(out, string(f[2]))
		}
	}
	return out
}

// ParseSmaps parses bounded memory mapping regions.
func ParseSmaps(b []byte, max int) []MapRegion {
	if max <= 0 {
		max = 4096
	}
	out := make([]MapRegion, 0, max)
	var cur *MapRegion
	for _, l := range bytes.Split(b, []byte{'\n'}) {
		f := bytes.Fields(l)
		if len(f) >= 5 && bytes.Contains(f[0], []byte{'-'}) {
			if cur != nil {
				out = append(out, *cur)
				if len(out) >= max {
					break
				}
			}
			p := bytes.SplitN(f[0], []byte{'-'}, 2)
			if len(p) != 2 {
				continue
			}
			a, _ := strconv.ParseUint(string(p[0]), 16, 64)
			z, _ := strconv.ParseUint(string(p[1]), 16, 64)
			off, _ := strconv.ParseUint(string(f[2]), 16, 64)
			ino, _ := strconv.ParseUint(string(f[4]), 10, 64)
			path := ""
			if len(f) > 5 {
				path = string(bytes.Join(f[5:], []byte{' '}))
			}
			cur = &MapRegion{Start: a, End: z, Perms: string(f[1]), Offset: off, Dev: string(f[3]), Inode: ino, Path: path}
			continue
		}
		if cur == nil {
			continue
		}
		if i := bytes.IndexByte(l, ':'); i >= 0 {
			v := kvBytes(bytes.TrimSpace(l[i+1:]))
			switch string(l[:i]) {
			case "Rss":
				cur.Rss = v
			case "Pss":
				cur.Pss = v
			case "Private_Dirty":
				cur.PrivateDirty = v
			case "Anonymous":
				cur.Anonymous = v
			case "Swap":
				cur.Swap = v
			}
		}
	}
	if cur != nil && len(out) < max {
		out = append(out, *cur)
	}
	return out
}

// ClockTicks returns the Linux user-Hz assumption used by /proc accounting.
func ClockTicks() int64 { return 100 }

// TicksToDuration converts process ticks to a duration.
func TicksToDuration(t uint64) time.Duration {
	return time.Duration(t) * time.Second / time.Duration(ClockTicks())
}
