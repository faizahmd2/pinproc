package inventory

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// memory accounts RAM into disjoint buckets that sum exactly to physical RAM (or
// MemTotal when the kernel does not expose memory blocks). The split follows
// proc(5)/meminfo semantics and smem's "-w" view:
//
//	Applications    AnonPages                      process heap/stack (private)
//	Shared & tmpfs  Shmem                          /dev/shm, tmpfs, shared anon
//	File cache      Cached-Shmem+Buffers+SwapCached reclaimable on demand
//	Kernel          Slab+KernelStack+PageTables+Percpu+VmallocUsed
//	Huge pages      Hugetlb
//	Free            MemFree
//	Kernel other    MemTotal - everything above     drivers, unaccounted pages
//	Firmware        physical - MemTotal             BIOS, kernel image, crashkernel
func (c *Collector) memory(a, b *sample, secs float64, services []Service) Memory {
	mi := kvBytes(readFile(filepath.Join(c.opt.Proc, "meminfo")))
	total := mi["MemTotal"]
	m := Memory{
		TotalBytes: total, AvailableBytes: mi["MemAvailable"], FreeBytes: mi["MemFree"],
		SwapTotal: mi["SwapTotal"], SwapUsed: sub(sub(mi["SwapTotal"], mi["SwapFree"]), mi["SwapCached"]),
		DirtyBytes: mi["Dirty"], WritebackBytes: mi["Writeback"],
		CommittedBytes: mi["Committed_AS"], CommitLimit: mi["CommitLimit"],
		PSI: readPSI(filepath.Join(c.opt.Proc, "pressure/memory")), PSSAccurate: c.pssSeen,
	}
	if m.AvailableBytes == 0 {
		m.AvailableBytes = m.FreeBytes
	}
	m.UsedBytes = sub(total, m.AvailableBytes)
	m.UsedPct = pct(float64(m.UsedBytes), float64(total))
	page := float64(os.Getpagesize())
	m.SwapInBPS = rate(b.vm["pswpin"], a.vm["pswpin"], secs) * page
	m.SwapOutBPS = rate(b.vm["pswpout"], a.vm["pswpout"], secs) * page
	m.MajorFaultsPS = rate(b.vm["pgmajfault"], a.vm["pgmajfault"], secs)
	m.OOMKills = b.vm["oom_kill"]

	anon := mi["AnonPages"]
	shmem := mi["Shmem"]
	cache := sub(mi["Cached"], shmem) + mi["Buffers"] + mi["SwapCached"]
	kparts := []Share{
		{Name: "slab, reclaimable (dentry/inode caches)", Value: float64(mi["SReclaimable"])},
		{Name: "slab, unreclaimable", Value: float64(mi["SUnreclaim"])},
		{Name: "page tables", Value: float64(mi["PageTables"] + mi["SecPageTables"])},
		{Name: "kernel stacks", Value: float64(mi["KernelStack"])},
		{Name: "per-CPU", Value: float64(mi["Percpu"])},
		{Name: "vmalloc", Value: float64(mi["VmallocUsed"])},
	}
	var kernel uint64
	for _, p := range kparts {
		kernel += uint64(p.Value)
	}
	huge := mi["Hugetlb"]
	if huge == 0 {
		huge = mi["HugePages_Total"] * mi["Hugepagesize"]
	}
	free := mi["MemFree"]
	sum := anon + shmem + cache + kernel + huge + free
	var other uint64
	if total >= sum {
		other = total - sum
	} else {
		// Counters overlap slightly on some kernels; trim the reclaimable cache so
		// the breakdown still adds up to MemTotal exactly.
		cache = sub(cache, sum-total)
	}

	m.Buckets = []Bucket{
		{Key: "apps", Label: "Applications", Source: "AnonPages", Value: float64(anon), Parts: appParts(services, anon)},
		{Key: "shmem", Label: "Shared memory & tmpfs", Source: "Shmem", Value: float64(shmem), Parts: c.tmpfsParts(shmem)},
		{Key: "cache", Label: "File cache (reclaimable)", Source: "Cached − Shmem + Buffers + SwapCached", Value: float64(cache),
			Parts: topShares([]Share{{Name: "page cache", Value: float64(sub(cache, mi["Buffers"]+mi["SwapCached"]))}, {Name: "buffers", Value: float64(mi["Buffers"])}, {Name: "swap cache", Value: float64(mi["SwapCached"])}}, 3, float64(cache), "")},
		{Key: "kernel", Label: "Kernel", Source: "Slab + PageTables + KernelStack + Percpu + VmallocUsed", Value: float64(kernel), Parts: topShares(kparts, 6, float64(kernel), "")},
	}
	if huge > 0 {
		m.Buckets = append(m.Buckets, Bucket{Key: "huge", Label: "Huge pages (reserved)", Source: "Hugetlb", Value: float64(huge)})
	}
	if other > 0 {
		m.Buckets = append(m.Buckets, Bucket{Key: "other", Label: "Kernel other (drivers, unaccounted)", Source: "MemTotal − all of the above", Value: float64(other)})
	}
	m.Buckets = append(m.Buckets, Bucket{Key: "free", Label: "Free", Source: "MemFree", Value: float64(free)})
	m.AccountedBytes = total
	if phys := physicalMemory(c.opt.Sys); phys > total {
		m.PhysicalBytes = phys
		m.AccountedBytes = phys
		m.Buckets = append(m.Buckets, Bucket{Key: "firmware", Label: "Firmware & kernel image", Source: "physical RAM − MemTotal", Value: float64(phys - total)})
	}
	for i := range m.Buckets {
		m.Buckets[i].Parts = dropZero(m.Buckets[i].Parts)
	}
	return m
}

func appParts(services []Service, anon uint64) []Share {
	parts := make([]Share, 0, len(services))
	for _, s := range services {
		if s.AnonBytes > 0 {
			parts = append(parts, Share{Name: s.Name, Value: float64(s.AnonBytes)})
		}
	}
	return topShares(parts, 5, float64(anon), "other processes")
}

// tmpfsParts names where shared memory lives: each tmpfs mount's used space.
func (c *Collector) tmpfsParts(shmem uint64) []Share {
	var parts []Share
	for _, line := range strings.Split(string(readFile(filepath.Join(c.opt.Proc, "self/mounts"))), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 || f[2] != "tmpfs" {
			continue
		}
		var st syscall.Statfs_t
		if syscall.Statfs(f[1], &st) != nil {
			continue
		}
		used := (uint64(st.Blocks) - uint64(st.Bfree)) * uint64(st.Bsize)
		if used > 0 {
			parts = append(parts, Share{Name: f[1], Value: float64(used)})
		}
	}
	return topShares(parts, 4, float64(shmem), "shared anonymous / SysV")
}

// physicalMemory sums online memory blocks, as lsmem does. It is the RAM the VM
// was given, before firmware and the kernel image take their share.
func physicalMemory(sys string) uint64 {
	dir := filepath.Join(sys, "devices/system/memory")
	bs, err := strconv.ParseUint(strings.TrimPrefix(readString(filepath.Join(dir, "block_size_bytes")), "0x"), 16, 64)
	if err != nil || bs == 0 {
		return 0
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	var n uint64
	for _, e := range ents {
		if !strings.HasPrefix(e.Name(), "memory") {
			continue
		}
		if readString(filepath.Join(dir, e.Name(), "state")) == "online" {
			n++
		}
	}
	return n * bs
}

func dropZero(in []Share) []Share {
	out := in[:0]
	for _, p := range in {
		if p.Value > 0 {
			out = append(out, p)
		}
	}
	return out
}
