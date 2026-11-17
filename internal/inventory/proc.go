package inventory

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/faizahmd2/pinproc/internal/identity"
	"github.com/faizahmd2/pinproc/internal/procfs"
)

const clockTicks = 100 // USER_HZ on every mainstream Linux build

type procSample struct {
	pid, ppid            int
	comm                 string
	state                byte
	utime, stime         uint64
	start                uint64
	threads              int
	readB, writeB        uint64
	ioOK                 bool
	cgroup               string
	uid                  uint64
	rss, anon, file, shm uint64
	swap                 uint64
}

func (c *Collector) scanProcs(prev *sample) map[int]*procSample {
	ents, err := os.ReadDir(c.opt.Proc)
	if err != nil {
		return nil
	}
	out := make(map[int]*procSample, len(ents))
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		dir := filepath.Join(c.opt.Proc, e.Name())
		st, err := procfs.ParsePidStat(readFile(filepath.Join(dir, "stat")))
		if err != nil {
			continue
		}
		p := &procSample{pid: pid, ppid: int(st.PPID), comm: st.Comm, state: st.State,
			utime: st.Utime, stime: st.Stime, start: st.Starttime, threads: int(st.NumThreads)}
		if io, err := procfs.ParsePidIO(readFile(filepath.Join(dir, "io"))); err == nil {
			p.readB, p.writeB, p.ioOK = io.ReadBytes, io.WriteBytes, true
		}
		if prev != nil {
			if q, ok := prev.procs[pid]; ok && q.start == p.start {
				p.cgroup = q.cgroup
			}
		}
		if p.cgroup == "" {
			p.cgroup = cgroupPath(readFile(filepath.Join(dir, "cgroup")))
		}
		kv := kvBytes(readFile(filepath.Join(dir, "status")))
		p.uid = kv["Uid"]
		p.rss, p.anon, p.file, p.shm, p.swap = kv["VmRSS"], kv["RssAnon"], kv["RssFile"], kv["RssShmem"], kv["VmSwap"]
		out[pid] = p
	}
	return out
}

// cgroupPath returns the unified (v2) path, or the systemd hierarchy on v1.
func cgroupPath(b []byte) string {
	var first, named string
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.SplitN(line, ":", 3)
		if len(f) != 3 {
			continue
		}
		if f[0] == "0" && f[1] == "" {
			return f[2]
		}
		if f[1] == "name=systemd" {
			named = f[2]
		}
		if first == "" {
			first = f[2]
		}
	}
	if named != "" {
		return named
	}
	return first
}

// owner classifies a process by its cgroup, the way systemd-cgtop and docker
// stats see it: a container, a systemd service, or (for login sessions and ad-hoc
// scopes) the program itself.
type owner struct {
	key, name, kind, unit, cgroup string
}

func classify(p *procSample) owner {
	if p.pid == 2 || p.ppid == 2 {
		return owner{key: "kernel", name: "kernel threads", kind: "kernel"}
	}
	path := p.cgroup
	if rt, id, cg := containerOf(path); id != "" {
		return owner{key: "container:" + id, name: rt + " " + short(id), kind: "container", cgroup: cg}
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	for i := len(parts) - 1; i >= 0; i-- {
		if strings.HasSuffix(parts[i], ".service") && !strings.HasPrefix(parts[i], "user@") {
			unit := parts[i]
			return owner{key: "unit:" + unit, name: strings.TrimSuffix(unit, ".service"), kind: "systemd", unit: unit,
				cgroup: "/" + strings.Join(parts[:i+1], "/")}
		}
	}
	if path == "/init.scope" || p.pid == 1 {
		return owner{key: "unit:init.scope", name: "systemd", kind: "systemd", unit: "init.scope", cgroup: "/init.scope"}
	}
	return owner{key: "prog:" + p.comm, name: p.comm, kind: "program"}
}

func containerOf(path string) (runtime, id, cgroup string) {
	for _, pre := range []struct{ prefix, rt string }{{"docker-", "docker"}, {"cri-containerd-", "containerd"}, {"crio-", "cri-o"}, {"libpod-", "podman"}} {
		i := strings.LastIndex(path, pre.prefix)
		if i < 0 {
			continue
		}
		rest := path[i+len(pre.prefix):]
		end := strings.Index(rest, ".scope")
		if end < 0 {
			continue
		}
		if end >= 12 {
			return pre.rt, rest[:end], path[:i+len(pre.prefix)+end+len(".scope")]
		}
	}
	// cgroupfs driver: /docker/<64 hex>
	if i := strings.Index(path, "/docker/"); i >= 0 {
		rest := path[i+len("/docker/"):]
		if j := strings.IndexByte(rest, '/'); j >= 0 {
			rest = rest[:j]
		}
		if len(rest) >= 12 {
			return "docker", rest, path[:i+len("/docker/")+len(rest)]
		}
	}
	return "", "", ""
}

func short(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func (c *Collector) readThrottle(procs map[int]*procSample) map[string][2]uint64 {
	root := filepath.Join(c.opt.Sys, "fs/cgroup")
	if _, err := os.Stat(filepath.Join(root, "cgroup.controllers")); err != nil {
		return nil
	}
	out := map[string][2]uint64{}
	for _, p := range procs {
		o := classify(p)
		if o.cgroup == "" {
			continue
		}
		if _, done := out[o.cgroup]; done {
			continue
		}
		kv := kvFields(readFile(filepath.Join(root, o.cgroup, "cpu.stat")))
		out[o.cgroup] = [2]uint64{kv["nr_periods"], kv["nr_throttled"]}
	}
	return out
}

type procRow struct {
	s      *procSample
	o      owner
	cores  float64
	usr    float64
	sys    float64
	rd, wr float64
	pss    uint64
	pssOK  bool
}

func (c *Collector) processes(ctx context.Context, a, b *sample, secs float64) ([]Process, []Service, Tasks) {
	var tasks Tasks
	rows := make([]*procRow, 0, len(b.procs))
	for _, p := range b.procs {
		tasks.Total++
		tasks.Threads += p.threads
		switch p.state {
		case 'R':
			tasks.Running++
		case 'D':
			tasks.Blocked++
		case 'Z':
			tasks.Zombie++
		}
		r := &procRow{s: p, o: classify(p)}
		if q, ok := a.procs[p.pid]; ok && q.start == p.start && secs > 0 {
			r.usr = float64(sub(p.utime, q.utime)) / clockTicks / secs
			r.sys = float64(sub(p.stime, q.stime)) / clockTicks / secs
			r.cores = r.usr + r.sys
			if p.ioOK && q.ioOK {
				r.rd = rate(p.readB, q.readB, secs)
				r.wr = rate(p.writeB, q.writeB, secs)
			}
		}
		rows = append(rows, r)
	}

	// Group into services.
	byKey := map[string]*Service{}
	mainStart := map[string]uint64{}
	for _, r := range rows {
		s := byKey[r.o.key]
		if s == nil {
			s = &Service{Key: r.o.key, Name: r.o.name, Kind: r.o.kind, Unit: r.o.unit, CgroupPath: r.o.cgroup}
			byKey[r.o.key] = s
		}
		s.Procs++
		s.pids = append(s.pids, r.s.pid)
		s.CPUCores += r.cores
		s.userCores += r.usr
		s.sysCores += r.sys
		s.ReadBPS += r.rd
		s.WriteBPS += r.wr
		s.SwapBytes += r.s.swap
		if st, ok := mainStart[r.o.key]; !ok || r.s.start < st || (r.s.start == st && r.s.pid < s.MainPID) {
			mainStart[r.o.key] = r.s.start
			s.MainPID = r.s.pid
		}
	}
	rowByPID := make(map[int]*procRow, len(rows))
	for _, r := range rows {
		rowByPID[r.s.pid] = r
	}

	services := make([]Service, 0, len(byKey))
	for _, s := range byKey {
		services = append(services, *s)
	}
	rssOf := func(s *Service) uint64 {
		var t uint64
		for _, pid := range s.pids {
			t += rowByPID[pid].s.rss
		}
		return t
	}
	sort.Slice(services, func(i, j int) bool { return rssOf(&services[i]) > rssOf(&services[j]) })

	// PSS for the heaviest services (exact shares of shared pages, as smem does).
	pssOK := false
	if c.opt.ReadSmaps {
		budget := 256
		read := func(r *procRow) {
			if r.pssOK || budget <= 0 || r.o.kind == "kernel" {
				return
			}
			budget--
			kv := kvBytes(readGuarded(filepath.Join(c.opt.Proc, strconv.Itoa(r.s.pid), "smaps_rollup"), 300*time.Millisecond))
			if pss, ok := kv["Pss"]; ok {
				r.pss, r.pssOK = pss, true
				r.s.anon = kv["Pss_Anon"]
				if _, has := kv["Pss_Anon"]; !has {
					r.s.anon = kv["Anonymous"]
				}
				pssOK = true
			}
		}
		for i := 0; i < len(services) && i < 8; i++ {
			for _, pid := range services[i].pids {
				read(rowByPID[pid])
			}
		}
		for _, r := range topRows(rows, 15) {
			read(r)
		}
	}
	for i := range services {
		s := &services[i]
		s.MemBytes, s.AnonBytes = 0, 0
		for _, pid := range s.pids {
			r := rowByPID[pid]
			if r.pssOK {
				s.MemBytes += r.pss
			} else {
				s.MemBytes += r.s.rss
			}
			s.AnonBytes += r.s.anon
		}
	}
	sort.SliceStable(services, func(i, j int) bool { return services[i].MemBytes > services[j].MemBytes })

	// Top processes.
	var top []*procRow
	if c.opt.TopProcs < 0 {
		top = rows
	} else {
		top = topRows(rows, c.opt.TopProcs)
	}
	procs := make([]Process, 0, len(top))
	for _, r := range top {
		procs = append(procs, c.process(r, byKey[r.o.key].Name))
	}
	sort.SliceStable(procs, func(i, j int) bool { return procs[i].CPUCores > procs[j].CPUCores })

	if c.opt.Enrich {
		if c.opt.TopProcs >= 0 {
			for i := range procs {
				c.enrich(&procs[i])
			}
		}
		var listen map[uint64]int
		if c.opt.Ports {
			listen = listeningInodes(c.opt.Proc)
		}
		cgRoot := filepath.Join(c.opt.Sys, "fs/cgroup")
		_, v2err := os.Stat(filepath.Join(cgRoot, "cgroup.controllers"))
		for i := 0; i < len(services) && i < 12; i++ {
			s := &services[i]
			if s.Kind == "kernel" {
				continue
			}
			mp := c.process(rowByPID[s.MainPID], s.Name)
			c.enrich(&mp)
			s.Exe, s.Cmdline, s.User, s.OOMScore, s.OOMAdj = mp.Exe, mp.Cmdline, mp.User, mp.OOMScore, mp.OOMAdj
			if s.Kind == "container" {
				if n := containerName(strings.TrimPrefix(s.Key, "container:")); n != "" {
					s.Name = n
				}
			}
			s.Ports = c.ports(s.pids, listen)
			if v2err == nil && s.CgroupPath != "" {
				s.Cgroup = c.cgroupStats(cgRoot, s.CgroupPath, a.cgThrot[s.CgroupPath], b.cgThrot[s.CgroupPath])
			}
		}
		// Keep process rows consistent with any renamed container.
		names := map[string]string{}
		for _, s := range services {
			names[s.Key] = s.Name
		}
		for i := range procs {
			if r := rowByPID[procs[i].PID]; r != nil {
				procs[i].Service = names[r.o.key]
			}
		}
	}
	_ = ctx
	c.pssSeen = pssOK
	return procs, services, tasks
}

// topRows picks up to n rows, alternating between the heaviest CPU, memory and
// disk I/O consumers so each resource's leaders are represented.
func topRows(rows []*procRow, n int) []*procRow {
	byCPU := append([]*procRow(nil), rows...)
	sort.Slice(byCPU, func(i, j int) bool { return byCPU[i].cores > byCPU[j].cores })
	byMem := append([]*procRow(nil), rows...)
	sort.Slice(byMem, func(i, j int) bool { return byMem[i].s.rss > byMem[j].s.rss })
	byIO := append([]*procRow(nil), rows...)
	sort.Slice(byIO, func(i, j int) bool { return byIO[i].rd+byIO[i].wr > byIO[j].rd+byIO[j].wr })
	seen := map[int]bool{}
	var out []*procRow
	for i := 0; len(out) < n && i < len(rows); i++ {
		for k, list := range [][]*procRow{byMem, byCPU, byIO} {
			r := list[i]
			if seen[r.s.pid] || len(out) >= n {
				continue
			}
			if (k == 1 && r.cores < 0.005) || (k == 2 && r.rd+r.wr == 0) {
				continue // nothing to show for this resource
			}
			seen[r.s.pid] = true
			out = append(out, r)
		}
	}
	return out
}

func (c *Collector) process(r *procRow, service string) Process {
	if r == nil {
		return Process{}
	}
	p := Process{
		PID: r.s.pid, PPID: r.s.ppid, Comm: r.s.comm, State: string(r.s.state), Service: service,
		CPUCores: r.cores, UserCores: r.usr, SysCores: r.sys,
		RSSBytes: r.s.rss, AnonBytes: r.s.anon, SwapBytes: r.s.swap,
		ReadBPS: r.rd, WriteBPS: r.wr, Threads: r.s.threads,
	}
	if r.pssOK {
		p.PSSBytes = r.pss
	}
	if c.boot > 0 {
		p.StartedAt = time.Unix(c.boot, 0).Add(time.Duration(r.s.start) * time.Second / clockTicks).UTC()
	}
	p.User = c.users[r.s.uid]
	if p.User == "" {
		p.User = strconv.FormatUint(r.s.uid, 10)
	}
	return p
}

// enrich adds the details htop shows for one process: full program path, the
// command line (secrets masked), open files vs limit and the OOM killer's view.
func (c *Collector) enrich(p *Process) {
	if p.PID == 0 {
		return
	}
	dir := filepath.Join(c.opt.Proc, strconv.Itoa(p.PID))
	if exe, err := os.Readlink(filepath.Join(dir, "exe")); err == nil {
		if trimmed, gone := strings.CutSuffix(exe, " (deleted)"); gone {
			exe, p.ExeGone = trimmed, true
		}
		p.Exe = exe
	}
	if b := readGuarded(filepath.Join(dir, "cmdline"), 300*time.Millisecond); len(b) > 0 {
		p.Cmdline = strings.Join(identity.RedactCmdline(procfs.ParseCmdline(b)), " ")
	}
	if ents, err := os.ReadDir(filepath.Join(dir, "fd")); err == nil {
		p.FDs = len(ents)
	}
	if lim, err := procfs.ParseLimits(readFile(filepath.Join(dir, "limits"))); err == nil {
		p.FDLimit = lim["Max open files"][0]
	}
	p.OOMScore = readInt(filepath.Join(dir, "oom_score"))
	p.OOMAdj = readInt(filepath.Join(dir, "oom_score_adj"))
}

// listeningInodes maps socket inode -> port for listening TCP sockets.
func listeningInodes(proc string) map[uint64]int {
	out := map[uint64]int{}
	for _, f := range []string{"net/tcp", "net/tcp6"} {
		rows, _ := procfs.ParseSocketTable(readFile(filepath.Join(proc, f)))
		for _, r := range rows {
			if r.State == "0A" && r.Inode != 0 {
				out[r.Inode] = r.LocalPort
			}
		}
	}
	return out
}

func (c *Collector) ports(pids []int, listen map[uint64]int) []int {
	if len(listen) == 0 {
		return nil
	}
	seen := map[int]bool{}
	var out []int
	for i, pid := range pids {
		if i >= 32 {
			break
		}
		dir := filepath.Join(c.opt.Proc, strconv.Itoa(pid), "fd")
		ents, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range ents {
			l, err := os.Readlink(filepath.Join(dir, e.Name()))
			if err != nil || !strings.HasPrefix(l, "socket:[") {
				continue
			}
			ino, _ := strconv.ParseUint(strings.TrimSuffix(strings.TrimPrefix(l, "socket:["), "]"), 10, 64)
			if port, ok := listen[ino]; ok && !seen[port] {
				seen[port] = true
				out = append(out, port)
			}
		}
	}
	sort.Ints(out)
	return out
}

func (c *Collector) cgroupStats(root, path string, before, after [2]uint64) *CgroupStats {
	dir := filepath.Join(root, path)
	if _, err := os.Stat(dir); err != nil {
		return nil
	}
	cs := &CgroupStats{MemCurrent: readUint(filepath.Join(dir, "memory.current"))}
	if v := readString(filepath.Join(dir, "memory.max")); v != "max" {
		cs.MemMax, _ = strconv.ParseUint(v, 10, 64)
	}
	if m := readPSI(filepath.Join(dir, "memory.pressure")); m.Available {
		cs.PSIAvailable = true
		cs.MemPSISome10, cs.MemPSIFull10 = m.SomeAvg10, m.FullAvg10
		cs.CPUPSISome10 = readPSI(filepath.Join(dir, "cpu.pressure")).SomeAvg10
		cs.IOPSISome10 = readPSI(filepath.Join(dir, "io.pressure")).SomeAvg10
	}
	cs.OOMKills = kvFields(readFile(filepath.Join(dir, "memory.events")))["oom_kill"]
	if periods := sub(after[0], before[0]); periods > 0 {
		cs.ThrottledPct = pct(float64(sub(after[1], before[1])), float64(periods))
	}
	if f := strings.Fields(readString(filepath.Join(dir, "cpu.max"))); len(f) == 2 && f[0] != "max" {
		quota, _ := strconv.ParseFloat(f[0], 64)
		period, _ := strconv.ParseFloat(f[1], 64)
		if period > 0 {
			cs.CPUMaxCores = quota / period
		}
	}
	return cs
}

// containerName reads a Docker container's name from its config on disk (no
// socket, no API call). Best-effort: needs read access to /var/lib/docker.
func containerName(id string) string {
	b := readFile(filepath.Join("/var/lib/docker/containers", id, "config.v2.json"))
	if len(b) == 0 {
		return ""
	}
	var v struct{ Name string }
	if json.Unmarshal(b, &v) != nil {
		return ""
	}
	return strings.TrimPrefix(v.Name, "/")
}
