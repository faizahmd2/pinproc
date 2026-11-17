package inventory

import (
	"context"
	"encoding/hex"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/faizahmd2/pinproc/internal/procfs"
)

// This file gives each workload its live shape — the things an engineer would
// otherwise piece together from ss -tnp, ls /proc/PID/fd, top -H and pstree:
// who it accepts connections from, whom it depends on, what it holds open, and
// which of its threads are burning CPU.

// sockEntry is one row of /proc/net/{tcp,tcp6,udp,udp6}.
type sockEntry struct {
	proto string
	procfs.Socket
}

type sockTable struct {
	listen []sockEntry // TCP LISTEN and bound UDP sockets
	estab  []sockEntry // TCP ESTABLISHED
}

func readSockets(proc string) *sockTable {
	t := &sockTable{}
	lowEphemeral := 32768
	if f := strings.Fields(readString(filepath.Join(proc, "sys/net/ipv4/ip_local_port_range"))); len(f) == 2 {
		if v, err := strconv.Atoi(f[0]); err == nil && v > 0 {
			lowEphemeral = v
		}
	}
	for _, proto := range []string{"tcp", "tcp6", "udp", "udp6"} {
		rows, _ := procfs.ParseSocketTable(readFile(filepath.Join(proc, "net", proto)))
		for _, r := range rows {
			e := sockEntry{proto: proto, Socket: r}
			switch {
			case strings.HasPrefix(proto, "tcp") && r.State == "0A":
				t.listen = append(t.listen, e)
			case strings.HasPrefix(proto, "tcp") && r.State == "01":
				t.estab = append(t.estab, e)
			case strings.HasPrefix(proto, "udp") && r.State == "07" && r.LocalPort > 0 && r.LocalPort < lowEphemeral && zeroAddr(r.RemoteAddr):
				t.listen = append(t.listen, e)
			}
		}
	}
	return t
}

func zeroAddr(h string) bool { return strings.Trim(h, "0") == "" }

// hexAddr decodes the kernel's /proc/net address encoding (each 32-bit word in
// host byte order, which is little-endian on every platform pinproc targets).
func hexAddr(h string) string {
	b, err := hex.DecodeString(h)
	if err != nil || (len(b) != 4 && len(b) != 16) {
		return h
	}
	for i := 0; i+4 <= len(b); i += 4 {
		b[i], b[i+1], b[i+2], b[i+3] = b[i+3], b[i+2], b[i+1], b[i]
	}
	if len(b) == 4 {
		return netip.AddrFrom4([4]byte(b)).String()
	}
	a := netip.AddrFrom16([16]byte(b))
	if a.Is4In6() {
		return a.Unmap().String()
	}
	return a.String()
}

func loopback(addr string) bool {
	a, err := netip.ParseAddr(addr)
	return err == nil && a.IsLoopback()
}

// fdWalk counts open descriptors of pids and maps socket inode -> pid. budget
// bounds the number of readlink calls so a host with huge fd tables stays fast.
func (c *Collector) fdWalk(pids []int, owners map[uint64]int, budget *int) int {
	total := 0
	for _, pid := range pids {
		dir := filepath.Join(c.opt.Proc, strconv.Itoa(pid), "fd")
		ents, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		total += len(ents)
		if owners == nil {
			continue
		}
		for _, e := range ents {
			if *budget <= 0 {
				break
			}
			*budget--
			l, err := os.Readlink(filepath.Join(dir, e.Name()))
			if err != nil || !strings.HasPrefix(l, "socket:[") {
				continue
			}
			ino, _ := strconv.ParseUint(strings.TrimSuffix(strings.TrimPrefix(l, "socket:["), "]"), 10, 64)
			if ino != 0 {
				if _, seen := owners[ino]; !seen {
					owners[ino] = pid
				}
			}
		}
	}
	return total
}

var wellKnown = map[int]string{
	22: "ssh", 25: "smtp", 53: "dns", 80: "http", 443: "https", 465: "smtps", 587: "smtp",
	1433: "mssql", 1521: "oracle", 2049: "nfs", 2181: "zookeeper", 2379: "etcd", 3306: "mysql",
	4222: "nats", 5432: "postgres", 5672: "amqp", 6379: "redis", 6443: "kube-apiserver",
	8086: "influxdb", 9042: "cassandra", 9092: "kafka", 9200: "elasticsearch", 11211: "memcached",
	27017: "mongodb", 3000: "http-app", 8080: "http-alt", 8443: "https-alt",
}

// workloadSockets attributes listening and established sockets to services and
// fills Service.{Listen,Peers,Ports,FDs} plus the machine-wide listener list.
// all=true walks every process (report); otherwise only the enriched services.
func (c *Collector) workloadSockets(services []Service, enriched int, all bool, procs map[int]*procSample) []Listener {
	t := readSockets(c.opt.Proc)
	owners := map[uint64]int{}
	budget := 60000
	svcOfPID := map[int]int{} // pid -> index in services
	for i := range services {
		for _, pid := range services[i].pids {
			svcOfPID[pid] = i
		}
	}
	for i := 0; i < len(services) && i < enriched; i++ {
		s := &services[i]
		if s.Kind == "kernel" {
			continue
		}
		pids := s.pids
		if len(pids) > 64 {
			pids = pids[:64]
		}
		s.FDs = c.fdWalk(pids, owners, &budget)
	}
	if all {
		var rest []int
		for i := enriched; i < len(services); i++ {
			if services[i].Kind != "kernel" {
				rest = append(rest, services[i].pids...)
			}
		}
		sort.Ints(rest)
		c.fdWalk(rest, owners, &budget)
	}

	listenPorts := map[int]bool{}
	for _, l := range t.listen {
		if strings.HasPrefix(l.proto, "tcp") {
			listenPorts[l.LocalPort] = true
		}
	}
	clients := map[int]int{}
	type peerKey struct {
		addr string
		port int
	}
	peers := map[int]map[peerKey]int{} // service index -> peer -> conns
	hostAddrs := map[string]bool{}
	for _, l := range t.listen {
		hostAddrs[hexAddr(l.LocalAddr)] = true
	}
	for _, e := range t.estab {
		if listenPorts[e.LocalPort] {
			clients[e.LocalPort]++
			continue
		}
		pid, ok := owners[e.Inode]
		if !ok {
			continue
		}
		si, ok := svcOfPID[pid]
		if !ok {
			continue
		}
		if peers[si] == nil {
			peers[si] = map[peerKey]int{}
		}
		peers[si][peerKey{hexAddr(e.RemoteAddr), e.RemotePort}]++
	}

	// Who listens on which port, so a local peer can be named by its service.
	portOwner := map[int]string{}
	var all_ []Listener
	seen := map[string]bool{}
	for _, l := range t.listen {
		addr := hexAddr(l.LocalAddr)
		key := l.proto + "|" + addr + "|" + strconv.Itoa(l.LocalPort)
		if seen[key] {
			continue
		}
		seen[key] = true
		ln := Listener{Proto: l.proto, Addr: addr, Port: l.LocalPort, Public: !loopback(addr)}
		if strings.HasPrefix(l.proto, "tcp") {
			ln.Clients = clients[l.LocalPort]
		}
		if pid, ok := owners[l.Inode]; ok {
			ln.PID = pid
			if si, ok := svcOfPID[pid]; ok {
				ln.Service = services[si].Name
				s := &services[si]
				s.Listen = append(s.Listen, ln)
				if strings.HasPrefix(l.proto, "tcp") {
					portOwner[l.LocalPort] = s.Name
				}
			} else if p := procs[pid]; p != nil {
				ln.Service = p.comm
			}
		}
		all_ = append(all_, ln)
	}
	sort.Slice(all_, func(i, j int) bool {
		if all_[i].Port != all_[j].Port {
			return all_[i].Port < all_[j].Port
		}
		return all_[i].Proto < all_[j].Proto
	})

	for i := range services {
		s := &services[i]
		sort.Slice(s.Listen, func(a, b int) bool { return s.Listen[a].Port < s.Listen[b].Port })
		s.Ports = nil
		pseen := map[int]bool{}
		for _, l := range s.Listen {
			if strings.HasPrefix(l.Proto, "tcp") && !pseen[l.Port] {
				pseen[l.Port] = true
				s.Ports = append(s.Ports, l.Port)
			}
		}
		for k, n := range peers[i] {
			p := Peer{Addr: k.addr, Port: k.port, Conns: n}
			if loopback(k.addr) || hostAddrs[k.addr] {
				p.Name = portOwner[k.port]
			}
			if p.Name == "" {
				p.Name = wellKnown[k.port]
			}
			s.Peers = append(s.Peers, p)
		}
		sort.Slice(s.Peers, func(a, b int) bool {
			if s.Peers[a].Conns != s.Peers[b].Conns {
				return s.Peers[a].Conns > s.Peers[b].Conns
			}
			return s.Peers[a].Port < s.Peers[b].Port
		})
		if len(s.Peers) > 6 {
			s.Peers = s.Peers[:6]
		}
	}
	return all_
}

// parents returns the launch chain of pid, outermost first, e.g.
// ["systemd (1)", "containerd-shim (812)"].
func parents(procs map[int]*procSample, pid int) []string {
	var chain []string
	p := procs[pid]
	for i := 0; p != nil && p.ppid > 0 && i < 6; i++ {
		q := procs[p.ppid]
		if q == nil {
			break
		}
		chain = append([]string{q.comm + " (" + strconv.Itoa(q.pid) + ")"}, chain...)
		p = q
	}
	return chain
}

func (c *Collector) readTasks(pid int) (map[int]uint64, map[int]string, map[int]string) {
	dir := filepath.Join(c.opt.Proc, strconv.Itoa(pid), "task")
	ents, err := os.ReadDir(dir)
	if err != nil || len(ents) > 4000 {
		return nil, nil, nil
	}
	ticks := make(map[int]uint64, len(ents))
	names := make(map[int]string, len(ents))
	states := make(map[int]string, len(ents))
	for _, e := range ents {
		tid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		st, err := procfs.ParsePidStat(readFile(filepath.Join(dir, e.Name(), "stat")))
		if err != nil {
			continue
		}
		ticks[tid] = st.Utime + st.Stime
		names[tid] = st.Comm
		states[tid] = string(st.State)
	}
	return ticks, names, states
}

// hotThreads measures per-thread CPU of the busiest multi-threaded processes over
// a short extra window (what top -H shows). Only runs when a process is actually
// busy, so an idle machine pays nothing.
func (c *Collector) hotThreads(ctx context.Context, services []Service, procs []Process) {
	type cand struct {
		pid int
		svc int
	}
	var cands []cand
	svcOf := map[string]int{}
	for i := range services {
		svcOf[services[i].Name] = i
	}
	sorted := append([]Process(nil), procs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].CPUCores > sorted[j].CPUCores })
	taken := map[int]bool{}
	for _, p := range sorted {
		if len(cands) >= 3 || p.CPUCores < 0.10 {
			break
		}
		si, ok := svcOf[p.Service]
		if !ok || taken[si] || p.Threads < 2 {
			continue
		}
		taken[si] = true
		cands = append(cands, cand{p.PID, si})
	}
	if len(cands) == 0 {
		return
	}
	before := make([]map[int]uint64, len(cands))
	for i, cd := range cands {
		before[i], _, _ = c.readTasks(cd.pid)
	}
	const win = 300 * time.Millisecond
	t := time.NewTimer(win)
	select {
	case <-ctx.Done():
		t.Stop()
		return
	case <-t.C:
	}
	for i, cd := range cands {
		after, names, states := c.readTasks(cd.pid)
		var ths []Thread
		for tid, v := range after {
			u, ok := before[i][tid]
			if !ok {
				continue
			}
			cores := float64(sub(v, u)) / clockTicks / win.Seconds()
			if cores >= 0.01 {
				ths = append(ths, Thread{TID: tid, Name: names[tid], Cores: cores, State: states[tid]})
			}
		}
		sort.Slice(ths, func(a, b int) bool { return ths[a].Cores > ths[b].Cores })
		if len(ths) > 5 {
			ths = ths[:5]
		}
		s := &services[cd.svc]
		s.HotThreads, s.HotThreadsPID = ths, cd.pid
	}
}
