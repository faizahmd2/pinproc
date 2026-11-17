package inventory

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// readFile reads a small kernel file. Errors are swallowed: every caller treats a
// missing or unreadable file as "not available", never as fatal.
func readFile(path string) []byte {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return b
}

func readString(path string) string { return strings.TrimSpace(string(readFile(path))) }

func readUint(path string) uint64 {
	v, _ := strconv.ParseUint(readString(path), 10, 64)
	return v
}

func readInt(path string) int {
	v, _ := strconv.Atoi(readString(path))
	return v
}

// readGuarded reads a /proc file that takes the target's mmap lock (cmdline,
// smaps_rollup). A process stuck in the kernel can hold that lock indefinitely, so
// the read runs aside and is abandoned after d — the same hazard htop and ps hit.
func readGuarded(path string, d time.Duration) []byte {
	ch := make(chan []byte, 1)
	go func() { ch <- readFile(path) }()
	select {
	case b := <-ch:
		return b
	case <-time.After(d):
		return nil
	}
}

// kvBytes parses "Key:   123 kB" files (meminfo, status, smaps_rollup) into bytes.
// Values without a unit (counts such as HugePages_Total) are kept as-is.
func kvBytes(b []byte) map[string]uint64 {
	out := make(map[string]uint64, 64)
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		line := sc.Text()
		i := strings.IndexByte(line, ':')
		if i <= 0 {
			continue
		}
		f := strings.Fields(line[i+1:])
		if len(f) == 0 {
			continue
		}
		v, err := strconv.ParseUint(f[0], 10, 64)
		if err != nil {
			continue
		}
		if len(f) > 1 && f[1] == "kB" {
			v *= 1024
		}
		out[line[:i]] = v
	}
	return out
}

// kvFields parses "key value" lines (vmstat, cpu.stat, memory.events).
func kvFields(b []byte) map[string]uint64 {
	out := map[string]uint64{}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) != 2 {
			continue
		}
		if v, err := strconv.ParseUint(f[1], 10, 64); err == nil {
			out[f[0]] = v
		}
	}
	return out
}

func sub(a, b uint64) uint64 {
	if a > b {
		return a - b
	}
	return 0
}

func rate(cur, prev uint64, secs float64) float64 {
	if secs <= 0 || cur < prev {
		return 0
	}
	return float64(cur-prev) / secs
}

func pct(part, whole float64) float64 {
	if whole <= 0 {
		return 0
	}
	return part / whole * 100
}

func osRelease(etc string) string {
	data := string(readFile(filepath.Join(etc, "os-release")))
	if data == "" {
		data = string(readFile("/usr/lib/os-release"))
	}
	var id string
	for _, line := range strings.Split(data, "\n") {
		if v, ok := strings.CutPrefix(line, "PRETTY_NAME="); ok {
			return strings.Trim(v, `"`)
		}
		if v, ok := strings.CutPrefix(line, "ID="); ok {
			id = strings.Trim(v, `"`)
		}
	}
	return id
}

// passwd maps uid -> user name from /etc/passwd.
func passwd(path string) map[uint64]string {
	out := map[uint64]string{}
	for _, line := range strings.Split(string(readFile(path)), "\n") {
		f := strings.Split(line, ":")
		if len(f) < 3 {
			continue
		}
		if uid, err := strconv.ParseUint(f[2], 10, 64); err == nil {
			out[uid] = f[0]
		}
	}
	return out
}

func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	out := in[:0]
	for _, s := range in {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
