// Package netmap builds a machine-wide socket→process map from /proc, so a network
// incident can be attributed to the service that owns the connections (and the
// listening ports). It is used only at capture time, not on the calm path, because
// scanning every process's fds is comparatively heavy.
package netmap

import (
	"bytes"
	"os"
	"strconv"
)

// stLISTEN is the hex TCP state for a listening socket in /proc/net/tcp.
const stLISTEN = "0A"

// Conn is one TCP socket from /proc/net/tcp(6).
type Conn struct {
	Inode     uint64
	LocalPort int
	Listen    bool
}

// maxFDScan bounds how many /proc/<pid>/fd entries we inspect in total.
const maxFDScan = 200000

// Connections parses /proc/net/tcp and tcp6 under procRoot.
func Connections(procRoot string) []Conn {
	var out []Conn
	for _, name := range []string{"/net/tcp", "/net/tcp6"} {
		out = append(out, parseTCP(readFile(procRoot+name))...)
	}
	return out
}

func parseTCP(data []byte) []Conn {
	if data == nil {
		return nil
	}
	var out []Conn
	first := true
	for len(data) > 0 {
		var line []byte
		if nl := bytes.IndexByte(data, '\n'); nl >= 0 {
			line, data = data[:nl], data[nl+1:]
		} else {
			line, data = data, nil
		}
		if first { // header
			first = false
			continue
		}
		f := bytes.Fields(line)
		if len(f) < 10 {
			continue
		}
		// f[1]=local_address "HEXIP:HEXPORT", f[3]=st, f[9]=inode
		c := Conn{Listen: string(f[3]) == stLISTEN}
		if colon := bytes.LastIndexByte(f[1], ':'); colon >= 0 {
			if p, err := strconv.ParseUint(string(f[1][colon+1:]), 16, 32); err == nil {
				c.LocalPort = int(p)
			}
		}
		if ino, err := strconv.ParseUint(string(f[9]), 10, 64); err == nil {
			c.Inode = ino
		}
		if c.Inode != 0 {
			out = append(out, c)
		}
	}
	return out
}

// SocketOwners maps socket inode → owning pid by scanning /proc/<pid>/fd links.
func SocketOwners(procRoot string) map[uint64]int {
	owners := map[uint64]int{}
	pf, err := os.Open(procRoot)
	if err != nil {
		return owners
	}
	names, _ := pf.Readdirnames(-1)
	_ = pf.Close()
	scanned := 0
	for _, nm := range names {
		pid, ok := numericPID(nm)
		if !ok {
			continue
		}
		fdDir := procRoot + "/" + nm + "/fd"
		df, err := os.Open(fdDir)
		if err != nil {
			continue
		}
		fds, _ := df.Readdirnames(-1)
		_ = df.Close()
		for _, fd := range fds {
			scanned++
			if scanned > maxFDScan {
				return owners
			}
			link, err := os.Readlink(fdDir + "/" + fd)
			if err != nil || !hasPrefix(link, "socket:[") {
				continue
			}
			ino := parseInode(link)
			if ino != 0 {
				if _, exists := owners[ino]; !exists {
					owners[ino] = pid
				}
			}
		}
	}
	return owners
}

func parseInode(link string) uint64 {
	// link = "socket:[12345]"
	start := len("socket:[")
	end := len(link) - 1
	if end <= start || link[end] != ']' {
		return 0
	}
	v, _ := strconv.ParseUint(link[start:end], 10, 64)
	return v
}

func readFile(path string) []byte {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return b
}

func hasPrefix(s, p string) bool { return len(s) >= len(p) && s[:len(p)] == p }

func numericPID(name string) (int, bool) {
	if name == "" {
		return 0, false
	}
	v := 0
	for i := 0; i < len(name); i++ {
		if name[i] < '0' || name[i] > '9' {
			return 0, false
		}
		v = v*10 + int(name[i]-'0')
	}
	return v, v > 0
}
