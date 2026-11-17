package inventory

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeHost builds a minimal /proc and /sys tree.
type fakeHost struct {
	t    *testing.T
	proc string
	sys  string
}

func newFakeHost(t *testing.T) *fakeHost {
	root := t.TempDir()
	h := &fakeHost{t: t, proc: filepath.Join(root, "proc"), sys: filepath.Join(root, "sys")}
	h.write("proc/meminfo", `MemTotal:        8000000 kB
MemFree:         2000000 kB
MemAvailable:    4000000 kB
Buffers:          100000 kB
Cached:          2100000 kB
SwapCached:            0 kB
AnonPages:       3000000 kB
Shmem:            100000 kB
SReclaimable:     300000 kB
SUnreclaim:       100000 kB
KernelStack:       20000 kB
PageTables:        40000 kB
Percpu:             5000 kB
VmallocUsed:       15000 kB
SwapTotal:       1000000 kB
SwapFree:        1000000 kB
Dirty:              1000 kB
Writeback:             0 kB
Committed_AS:    5000000 kB
CommitLimit:     5000000 kB
`)
	h.write("sys/devices/system/memory/block_size_bytes", "8000000\n") // 128 MiB
	for i := 0; i < 64; i++ {                                          // 64 x 128 MiB = 8 GiB
		h.write("sys/devices/system/memory/memory"+itoa(uint64(i))+"/state", "online\n")
	}
	h.write("proc/loadavg", "1.00 0.50 0.25 2/300 1234\n")
	h.write("proc/uptime", "3600.00 1000.00\n")
	h.write("proc/vmstat", "pswpin 0\npswpout 0\npgmajfault 10\noom_kill 1\n")
	h.write("proc/net/snmp", "Tcp: RtoAlgorithm RtoMin RtoMax MaxConn ActiveOpens PassiveOpens AttemptFails EstabResets CurrEstab InSegs OutSegs RetransSegs InErrs OutRsts\nTcp: 1 200 120000 -1 10 10 0 0 5 100 100 0 0 0\n")
	h.write("proc/net/dev", "Inter-|   Receive |  Transmit\n face |bytes packets errs drop fifo frame compressed multicast|bytes packets errs drop fifo colls carrier compressed\n  eth0: 1000 10 0 0 0 0 0 0 2000 20 0 0 0 0 0 0\n")
	h.write("sys/class/net/eth0/device/x", "")
	h.write("sys/class/net/eth0/speed", "10000\n")
	h.write("sys/class/net/eth0/operstate", "up\n")
	h.write("proc/diskstats", "   8       0 sda 100 0 800 50 200 0 1600 100 0 100 150 0 0 0 0\n")
	h.write("sys/block/sda/size", "209715200\n") // 100 GiB in 512-byte sectors
	h.write("sys/block/sda/queue/rotational", "0\n")
	h.write("sys/block/sda/sda1/partition", "1\n")
	h.write("sys/block/sda/sda1/size", "104857600\n") // 50 GiB, not mounted
	h.write("sys/block/sda/sda1/dev", "8:1\n")
	h.write("sys/block/loop0/size", "100\n")
	h.stat(100, 100)
	h.addProc("1", "systemd", 'S', 1, 0, 50, "0::/init.scope\n", 10000)
	h.addProc("2", "kthreadd", 'S', 0, 0, 0, "0::/\n", 0)
	h.addProc("50", "kworker/0:1", 'I', 2, 0, 0, "0::/\n", 0)
	h.addProc("100", "postgres", 'S', 1, 1000, 500, "0::/system.slice/postgresql@16.service\n", 900000)
	h.addProc("101", "postgres", 'S', 100, 1000, 100, "0::/system.slice/postgresql@16.service\n", 300000)
	h.addProc("200", "node", 'R', 1, 1000, 1000, "0::/system.slice/docker-0123456789abcdef0123456789abcdef.scope\n", 500000)
	h.addProc("300", "bash", 'S', 1, 1000, 10, "0::/user.slice/user-1000.slice/session-3.scope\n", 4000)
	return h
}

func (h *fakeHost) write(rel, body string) {
	p := filepath.Join(filepath.Dir(h.proc), rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		h.t.Fatal(err)
	}
}

func (h *fakeHost) stat(user, idle uint64) {
	h.write("proc/stat", "cpu  "+itoa(user)+" 0 "+itoa(user/2)+" "+itoa(idle)+" 0 0 0 0 0 0\ncpu0 "+itoa(user/2)+" 0 "+itoa(user/4)+" "+itoa(idle/2)+" 0 0 0 0 0 0\ncpu1 "+itoa(user/2)+" 0 "+itoa(user/4)+" "+itoa(idle/2)+" 0 0 0 0 0 0\nctxt 1000\nbtime 1700000000\n")
}

func (h *fakeHost) addProc(pid, comm string, state byte, ppid int, uid uint64, ticks uint64, cgroup string, rssKB uint64) {
	dir := "proc/" + pid + "/"
	h.write(dir+"stat", pid+" ("+comm+") "+string(state)+" "+itoa(uint64(ppid))+" 1 1 0 -1 0 0 0 0 0 "+itoa(ticks)+" 0 0 0 20 0 1 0 100 0 "+itoa(rssKB/4)+strings.Repeat(" 0", 30)+"\n")
	h.write(dir+"status", "Name:\t"+comm+"\nUid:\t"+itoa(uid)+"\t"+itoa(uid)+"\t"+itoa(uid)+"\t"+itoa(uid)+"\nVmRSS:\t"+itoa(rssKB)+" kB\nRssAnon:\t"+itoa(rssKB*3/4)+" kB\nRssFile:\t"+itoa(rssKB/4)+" kB\nVmSwap:\t0 kB\n")
	h.write(dir+"cgroup", cgroup)
	h.write(dir+"io", "rchar: 0\nwchar: 0\nsyscr: 0\nsyscw: 0\nread_bytes: 0\nwrite_bytes: "+itoa(ticks*4096)+"\ncancelled_write_bytes: 0\n")
}

func collectFake(t *testing.T, h *fakeHost, mutate func()) *Snapshot {
	t.Helper()
	c := New(Options{Proc: h.proc, Sys: h.sys, Etc: filepath.Join(filepath.Dir(h.proc), "etc"), TopProcs: 10, Enrich: true})
	c.users = map[uint64]string{0: "root", 1000: "app"}
	a := c.take(nil)
	mutate()
	b := c.take(a)
	b.at = a.at.Add(time.Second)
	c.prev = a
	return c.build(context.Background(), a, b)
}

func sumBuckets(bs []Bucket) float64 {
	var s float64
	for _, b := range bs {
		s += b.Value
	}
	return s
}

func TestMemoryIsZeroSumToPhysicalRAM(t *testing.T) {
	h := newFakeHost(t)
	s := collectFake(t, h, func() {})
	m := s.Memory
	if m.PhysicalBytes != 8<<30 {
		t.Fatalf("physical = %d, want 8 GiB", m.PhysicalBytes)
	}
	if got := sumBuckets(m.Buckets); math.Abs(got-float64(m.AccountedBytes)) > 1 {
		t.Fatalf("buckets sum %.0f != accounted %d", got, m.AccountedBytes)
	}
	want := map[string]float64{"apps": 3000000 * 1024, "shmem": 100000 * 1024, "free": 2000000 * 1024,
		"cache": (2100000 - 100000 + 100000) * 1024, "kernel": (300000 + 100000 + 20000 + 40000 + 5000 + 15000) * 1024,
		"firmware": float64(8<<30) - 8000000*1024}
	for _, b := range m.Buckets {
		if w, ok := want[b.Key]; ok && math.Abs(b.Value-w) > 1 {
			t.Errorf("bucket %s = %.0f, want %.0f", b.Key, b.Value, w)
		}
		var parts float64
		for _, p := range b.Parts {
			parts += p.Value
		}
		if parts > b.Value+1 {
			t.Errorf("bucket %s parts %.0f exceed bucket %.0f", b.Key, parts, b.Value)
		}
	}
	if m.UsedBytes != (8000000-4000000)*1024 {
		t.Errorf("used = %d (want MemTotal-MemAvailable)", m.UsedBytes)
	}
}

func TestCPUBucketsSumToCores(t *testing.T) {
	h := newFakeHost(t)
	s := collectFake(t, h, func() {
		h.stat(100+150, 100+50) // +150 user, +75 system, +50 idle
		h.addProc("100", "postgres", 'S', 1, 1000, 500+60, "0::/system.slice/postgresql@16.service\n", 900000)
	})
	if s.CPU.Cores != 2 {
		t.Fatalf("cores = %d", s.CPU.Cores)
	}
	if got := sumBuckets(s.CPU.Buckets); math.Abs(got-2) > 1e-9 {
		t.Fatalf("cpu buckets sum to %.4f cores, want 2", got)
	}
	var pg *Service
	for i := range s.Services {
		if s.Services[i].Unit == "postgresql@16.service" {
			pg = &s.Services[i]
		}
	}
	if pg == nil || math.Abs(pg.CPUCores-0.6) > 1e-9 {
		t.Fatalf("postgres service cpu = %+v, want 0.60 cores", pg)
	}
	if pg.WriteBPS != 60*4096 {
		t.Errorf("postgres write rate = %.0f", pg.WriteBPS)
	}
}

func TestServicesGroupByOwner(t *testing.T) {
	h := newFakeHost(t)
	s := collectFake(t, h, func() {})
	byKey := map[string]Service{}
	for _, sv := range s.Services {
		byKey[sv.Key] = sv
	}
	pg, ok := byKey["unit:postgresql@16.service"]
	if !ok || pg.Procs != 2 || pg.MainPID != 100 || pg.Kind != "systemd" {
		t.Fatalf("postgres = %+v", pg)
	}
	if pg.CgroupPath != "/system.slice/postgresql@16.service" {
		t.Errorf("cgroup path = %q", pg.CgroupPath)
	}
	if c, ok := byKey["container:0123456789abcdef0123456789abcdef"]; !ok || c.Kind != "container" {
		t.Errorf("container not grouped: %+v", byKey)
	}
	if k, ok := byKey["kernel"]; !ok || k.Procs != 2 {
		t.Errorf("kernel threads = %+v", k)
	}
	if b, ok := byKey["prog:bash"]; !ok || b.User != "" && b.User != "app" {
		t.Errorf("session program = %+v", b)
	}
	if s.Services[0].Key != "unit:postgresql@16.service" {
		t.Errorf("services not sorted by memory: first = %s", s.Services[0].Key)
	}
	if s.Tasks.Total != 7 || s.Tasks.Running != 1 {
		t.Errorf("tasks = %+v", s.Tasks)
	}
}

func TestDiskIsZeroSumToDeviceSize(t *testing.T) {
	h := newFakeHost(t)
	s := collectFake(t, h, func() {})
	if len(s.Disks) != 1 || s.Disks[0].Name != "sda" {
		t.Fatalf("disks = %+v (loop devices must be skipped)", s.Disks)
	}
	d := s.Disks[0]
	if d.SizeBytes != 100<<30 || d.Kind != "SSD" {
		t.Fatalf("disk = %+v", d)
	}
	if got := sumBuckets(d.Buckets); math.Abs(got-float64(d.SizeBytes)) > 1 {
		t.Fatalf("disk buckets sum %.0f != size %d", got, d.SizeBytes)
	}
	labels := []string{}
	for _, b := range d.Buckets {
		labels = append(labels, b.Key)
	}
	if strings.Join(labels, ",") != "unmounted,unpartitioned" {
		t.Errorf("buckets = %v", labels)
	}
}

func TestTopSharesNeverExceedTotal(t *testing.T) {
	parts := topShares([]Share{{"a", 60}, {"b", 50}, {"c", 10}}, 2, 100, "other")
	var sum float64
	for _, p := range parts {
		sum += p.Value
	}
	if sum > 100+1e-9 {
		t.Fatalf("parts %v exceed total", parts)
	}
	parts = topShares([]Share{{"a", 30}, {"b", 20}}, 5, 100, "other")
	if len(parts) != 3 || parts[2].Name != "other" || parts[2].Value != 50 {
		t.Fatalf("remainder missing: %v", parts)
	}
}

func TestContainerAndCgroupParsing(t *testing.T) {
	if p := cgroupPath([]byte("12:pids:/x\n1:name=systemd:/system.slice/a.service\n")); p != "/system.slice/a.service" {
		t.Errorf("v1 path = %q", p)
	}
	if p := cgroupPath([]byte("0::/system.slice/b.service\n")); p != "/system.slice/b.service" {
		t.Errorf("v2 path = %q", p)
	}
	rt, id, cg := containerOf("/kubepods.slice/kubepods-pod1.slice/cri-containerd-abcdef0123456789.scope")
	if rt != "containerd" || id != "abcdef0123456789" || !strings.HasSuffix(cg, ".scope") {
		t.Errorf("k8s container = %q %q %q", rt, id, cg)
	}
	if _, id, _ := containerOf("/docker/0123456789abcdef0123"); id != "0123456789abcdef0123" {
		t.Errorf("cgroupfs docker id = %q", id)
	}
}
