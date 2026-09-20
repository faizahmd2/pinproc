package process

import (
	"os"
	"github.com/faizahmd2/vm-native-diagnos/internal/capability/spec"
	"github.com/faizahmd2/vm-native-diagnos/internal/contract"
	"github.com/faizahmd2/vm-native-diagnos/internal/procfs"
	"github.com/faizahmd2/vm-native-diagnos/internal/source"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// MemoryFacts is process memory evidence.
type MemoryFacts struct {
	PID                                             int64
	RSS, PSS, Anon, File, Shmem, Swap, PrivateDirty uint64
	VMSize, VmSwap, Threads                         uint64
}

// I/O facts for one process.
type IOFacts struct {
	PID                                                         int64
	ReadBytesPerSec, WriteBytesPerSec, RCharPerSec, WCharPerSec float64
	SysReadPerSec, SysWritePerSec                               float64
}

// FileFacts summarizes process file descriptors.
type FileFacts struct {
	PID                                              int64
	Total, Regular, Sockets, Pipes, Devices, Deleted uint64
	DeletedBytes uint64
	Samples                                          []FDEntry
}

// FDEntry is one bounded descriptor target.
type FDEntry struct {
	FD     int
	Target string
	Kind   string
}

// SocketFacts summarizes process socket inodes discovered from fd links.
type SocketFacts struct {
	PID    int64
	Count  uint64
	Inodes []uint64
}

// Limits returns process descriptor/process-count limits.
func Limits() spec.Capability {
	return spec.Capability{
		ID: "process.limits", Dimension: contract.DimensionLimits, Level: contract.L2Owner,
		Kind: contract.KindSnapshot, Accepts: contract.EntityProcess, Cost: contract.CostLow,
		Summary: "process file-descriptor and process-count limits", LeadsTo: nil,
		Reads: func(e contract.Entity, _ contract.Facts) []source.Read {
			p := depthEntityPID(e)
			return []source.Read{
				{Key: "proc.limits", Path: "/proc/" + p + "/limits", Kind: source.ReadFile, Optional: true},
				{Key: "proc.fd", Path: "/proc/" + p + "/fd/*", Kind: source.ReadDirNames, Optional: true},
			}
		},
		Parse: parseLimits,
	}
}

func parseLimits(in spec.ParseInput) (contract.Evidence, error) {
	pid := depthEntityPID(in.Scope)
	limits, err := procfs.ParseLimits(depthRaw(in.Sample.T1, "proc.limits"))
	if err != nil {
		return contract.Evidence{}, err
	}
	maxFD := limits["Max open files"]
	maxProc := limits["Max processes"]
	fdOpen := len(in.Sample.T1.Reads["proc.fd"])
	fdPct := 0.0
	if maxFD[1] > 0 && maxFD[1] < ^uint64(0)>>1 {
		fdPct = float64(fdOpen) / float64(maxFD[1]) * 100
	}
	return contract.Evidence{
		ID: "ev-process-" + pid + "-limits", Capability: "process.limits",
		Entity:    contract.Entity{Kind: contract.EntityProcess, ID: "pid:" + pid},
		Dimension: contract.DimensionLimits, Level: contract.L2Owner, CollectedAt: in.Sample.T1.At,
		Observations: []contract.Observation{
			{Key: "proc.fd_open", Value: float64(fdOpen), Unit: "count"},
			{Key: "proc.fd_limit", Value: float64(maxFD[1]), Unit: "count"},
			{Key: "proc.fd_used_pct", Value: fdPct, Unit: "percent"},
			{Key: "proc.pid_limit", Value: float64(maxProc[1]), Unit: "count"},
		},
		Facts: limits, Sources: []string{"/proc/" + pid + "/limits", "/proc/" + pid + "/fd"},
		Verify: []string{"cat /proc/" + pid + "/limits", "ls -1 /proc/" + pid + "/fd | wc -l"},
	}, nil
}

// Memory returns process-level memory evidence.
func Memory() spec.Capability {
	return spec.Capability{
		ID: "process.memory", Dimension: contract.DimensionMemory, Level: contract.L2Owner, Kind: contract.KindSnapshot,
		Accepts: contract.EntityProcess, Cost: contract.CostLow, Summary: "process RSS/PSS and anonymous/file/shared/swap memory",
		LeadsTo: []string{"process.memory_maps"},
		Reads: func(e contract.Entity, _ contract.Facts) []source.Read {
			p := depthEntityPID(e)
			return []source.Read{
				{Key: "proc.status", Path: "/proc/" + p + "/status", Kind: source.ReadFile},
				{Key: "proc.smaps_rollup", Path: "/proc/" + p + "/smaps_rollup", Kind: source.ReadFile, Optional: true},
				{Key: "proc.limits", Path: "/proc/" + p + "/limits", Kind: source.ReadFile, Optional: true},
			}
		}, Parse: parseMemory}
}

// IO returns process-level I/O evidence.
func IO() spec.Capability {
	return spec.Capability{
		ID: "process.io", Dimension: contract.DimensionIO, Level: contract.L2Owner, Kind: specKindSampled(),
		Accepts: contract.EntityProcess, Cost: contract.CostLow, Summary: "process read/write throughput and syscall activity",
		LeadsTo: []string{"process.files"}, Reads: func(e contract.Entity, _ contract.Facts) []source.Read {
			p := depthEntityPID(e)
			return []source.Read{{Key: "proc.io", Path: "/proc/" + p + "/io", Kind: source.ReadFile}}
		}, Parse: parseIO}
}

// Files returns bounded descriptor attribution.
func Files() spec.Capability {
	return spec.Capability{
		ID: "process.files", Dimension: contract.DimensionFilesystem, Level: contract.L3Execution, Kind: contract.KindSnapshot,
		Accepts: contract.EntityProcess, Cost: contract.CostMedium, Summary: "open-file and descriptor inventory with socket/pipe/device classification",
		LeadsTo: []string{"process.sockets"}, Reads: func(e contract.Entity, _ contract.Facts) []source.Read {
			p := depthEntityPID(e)
			return []source.Read{{Key: "proc.fd", Path: "/proc/" + p + "/fd/*", Kind: source.ReadGlobLinks, MaxBytes: 4096, Optional: true}}
		}, Parse: parseFiles}
}

// Sockets returns process-owned socket inodes.
func Sockets() spec.Capability {
	return spec.Capability{
		ID: "process.sockets", Dimension: contract.DimensionNetwork, Level: contract.L4Mechanism, Kind: specKindSnapshot(),
		Accepts: contract.EntityProcess, Cost: contract.CostMedium, Summary: "process socket descriptor ownership and inode inventory",
		LeadsTo: nil, Reads: func(e contract.Entity, _ contract.Facts) []source.Read {
			p := depthEntityPID(e)
			return []source.Read{{Key: "proc.fd", Path: "/proc/" + p + "/fd/*", Kind: source.ReadGlob, MaxBytes: 4096, Optional: true}}
		}, Parse: parseSockets}
}

// MemoryMaps is deep process memory mapping evidence.
func MemoryMaps() spec.Capability {
	return spec.Capability{
		ID: "process.memory_maps", Dimension: contract.DimensionMemory, Level: contract.L4Mechanism, Kind: specKindSnapshot(),
		Accepts: contract.EntityProcess, Cost: contract.CostHigh, Summary: "bounded /proc/<pid>/smaps region inventory", Requires: []string{"root_or_ptrace"}, LeadsTo: nil,
		Reads: func(e contract.Entity, _ contract.Facts) []source.Read {
			p := depthEntityPID(e)
			return []source.Read{{Key: "proc.smaps", Path: "/proc/" + p + "/smaps", Kind: source.ReadFile, MaxBytes: 2 << 20, Optional: true}}
		}, Parse: parseMaps}
}

func specKindSampled() contract.CapabilityKind  { return contract.KindSampled }
func specKindSnapshot() contract.CapabilityKind { return contract.KindSnapshot }

func parseMemory(in spec.ParseInput) (contract.Evidence, error) {
	pid := depthEntityPID(in.Scope)
	status, _ := procfs.ParsePidStatus(depthRaw(in.Sample.T1, "proc.status"))
	roll, _ := procfs.ParseSmapsRollup(depthRaw(in.Sample.T1, "proc.smaps_rollup"))
	f := MemoryFacts{PID: status.Pid, RSS: roll.Rss, PSS: roll.Pss, Anon: roll.PssAnon, File: roll.PssFile, Shmem: roll.PssShmem, Swap: roll.Swap, PrivateDirty: roll.PrivateDirty, VMSize: status.VmSize, VmSwap: status.VmSwap, Threads: status.Threads}
	if f.PID == 0 {
		f.PID, _ = strconv.ParseInt(pid, 10, 64)
	}
	obs := []contract.Observation{{Key: "proc.rss", Value: float64(f.RSS), Unit: "bytes"}, {Key: "proc.pss", Value: float64(f.PSS), Unit: "bytes"}, {Key: "proc.anon", Value: float64(f.Anon), Unit: "bytes"}, {Key: "proc.file", Value: float64(f.File), Unit: "bytes"}, {Key: "proc.shmem", Value: float64(f.Shmem), Unit: "bytes"}, {Key: "proc.swap", Value: float64(f.Swap), Unit: "bytes"}, {Key: "proc.private_dirty", Value: float64(f.PrivateDirty), Unit: "bytes"}, {Key: "proc.vm_size", Value: float64(f.VMSize), Unit: "bytes"}, {Key: "proc.vm_swap", Value: float64(f.VmSwap), Unit: "bytes"}}
	return contract.Evidence{ID: "ev-process-" + pid + "-memory", Capability: "process.memory", Entity: contract.Entity{Kind: contract.EntityProcess, ID: "pid:" + pid}, Dimension: contract.DimensionMemory, Level: contract.L2Owner, CollectedAt: in.Sample.T1.At, Observations: obs, Facts: f, Sources: []string{"/proc/" + pid + "/status", "/proc/" + pid + "/smaps_rollup", "/proc/" + pid + "/limits"}, Verify: []string{"cat /proc/" + pid + "/status", "cat /proc/" + pid + "/smaps_rollup"}}, nil
}
func parseIO(in spec.ParseInput) (contract.Evidence, error) {
	pid := depthEntityPID(in.Scope)
	a, e := procfs.ParsePidIO(depthRaw(in.Sample.T0, "proc.io"))
	if e != nil {
		return contract.Evidence{}, e
	}
	b, e := procfs.ParsePidIO(depthRaw(in.Sample.T1, "proc.io"))
	if e != nil {
		return contract.Evidence{}, e
	}
	sec := in.Window.Seconds()
	if sec <= 0 {
		sec = 1
	}
	f := IOFacts{PID: parsePID(pid), ReadBytesPerSec: float64(depthDU(a.ReadBytes, b.ReadBytes)) / sec, WriteBytesPerSec: float64(depthDU(a.WriteBytes, b.WriteBytes)) / sec, RCharPerSec: float64(depthDU(a.RChar, b.RChar)) / sec, WCharPerSec: float64(depthDU(a.WChar, b.WChar)) / sec, SysReadPerSec: float64(depthDU(a.SyscR, b.SyscR)) / sec, SysWritePerSec: float64(depthDU(a.SyscW, b.SyscW)) / sec}
	return contract.Evidence{ID: "ev-process-" + pid + "-io", Capability: "process.io", Entity: contract.Entity{Kind: contract.EntityProcess, ID: "pid:" + pid}, Dimension: contract.DimensionIO, Level: contract.L2Owner, CollectedAt: in.Sample.T1.At, Window: in.Window, Observations: []contract.Observation{{Key: "proc.read_bps", Value: f.ReadBytesPerSec, Unit: "bytes_per_sec"}, {Key: "proc.write_bps", Value: f.WriteBytesPerSec, Unit: "bytes_per_sec"}, {Key: "proc.rchar_per_sec", Value: f.RCharPerSec, Unit: "bytes_per_sec"}, {Key: "proc.wchar_per_sec", Value: f.WCharPerSec, Unit: "bytes_per_sec"}, {Key: "proc.syscr_per_sec", Value: f.SysReadPerSec, Unit: "count_per_sec"}, {Key: "proc.syscw_per_sec", Value: f.SysWritePerSec, Unit: "count_per_sec"}}, Facts: f, Sources: []string{"/proc/" + pid + "/io"}, Verify: []string{"cat /proc/" + pid + "/io"}}, nil
}
func parseFiles(in spec.ParseInput) (contract.Evidence, error) {
	pid := depthEntityPID(in.Scope)
	rows := []FDEntry{}
	var reg, socks, pipes, dev, deleted, deletedBytes uint64
	for path, data := range depthIndex(in.Sample.T1.Reads["proc.fd"]) {
		fd := fdNumber(path)
		target := string(data)
		kind := "other"
		if strings.HasSuffix(target, " (deleted)") {
			deleted++
			if st, err := os.Stat("/proc/" + pid + "/fd/" + strconv.Itoa(fd)); err == nil && st.Size() > 0 {
				deletedBytes += uint64(st.Size())
			}
		}
		switch {
		case strings.HasPrefix(target, "socket:"):
			kind = "socket"
			socks++
		case strings.HasPrefix(target, "pipe:"):
			kind = "pipe"
			pipes++
		case strings.HasPrefix(target, "/dev/"):
			kind = "device"
			dev++
		case strings.HasPrefix(target, "/"):
			kind = "regular"
			reg++
		}
		if len(rows) < 64 {
			rows = append(rows, FDEntry{FD: fd, Target: target, Kind: kind})
		}
	}
	f := FileFacts{PID: parsePID(pid), Total: reg + socks + pipes + dev, Samples: rows, Regular: reg, Sockets: socks, Pipes: pipes, Devices: dev, Deleted: deleted, DeletedBytes: deletedBytes}
	return contract.Evidence{ID: "ev-process-" + pid + "-files", Capability: "process.files", Entity: contract.Entity{Kind: contract.EntityProcess, ID: "pid:" + pid}, Dimension: contract.DimensionFilesystem, Level: contract.L3Execution, CollectedAt: in.Sample.T1.At, Facts: f, Observations: []contract.Observation{{Key: "proc.fd_total", Value: float64(f.Total), Unit: "count"}, {Key: "proc.fd_sockets", Value: float64(socks), Unit: "count"}, {Key: "proc.fd_pipes", Value: float64(pipes), Unit: "count"}, {Key: "proc.fd_regular", Value: float64(reg), Unit: "count"}, {Key: "proc.fd_deleted", Value: float64(deleted), Unit: "count"}, {Key: "proc.fd_deleted_bytes", Value: float64(deletedBytes), Unit: "bytes"}}, Sources: []string{"/proc/" + pid + "/fd/*"}, Verify: []string{"ls -l /proc/" + pid + "/fd"}}, nil
}
func parseSockets(in spec.ParseInput) (contract.Evidence, error) {
	pid := depthEntityPID(in.Scope)
	f := SocketFacts{PID: parsePID(pid)}
	for _, r := range in.Sample.T1.Reads["proc.fd"] {
		t := string(r.Data)
		if !strings.HasPrefix(t, "socket:[") {
			continue
		}
		x := strings.TrimSuffix(strings.TrimPrefix(t, "socket:["), "]")
		v, e := strconv.ParseUint(x, 10, 64)
		if e == nil {
			f.Inodes = append(f.Inodes, v)
			f.Count++
		}
	}
	sort.Slice(f.Inodes, func(i, j int) bool { return f.Inodes[i] < f.Inodes[j] })
	return contract.Evidence{ID: "ev-process-" + pid + "-sockets", Capability: "process.sockets", Entity: contract.Entity{Kind: contract.EntityProcess, ID: "pid:" + pid}, Dimension: contract.DimensionNetwork, Level: contract.L4Mechanism, CollectedAt: in.Sample.T1.At, Facts: f, Observations: []contract.Observation{{Key: "proc.socket_count", Value: float64(f.Count), Unit: "count"}}, Sources: []string{"/proc/" + pid + "/fd/*"}, Verify: []string{"ls -l /proc/" + pid + "/fd"}}, nil
}
func parseMaps(in spec.ParseInput) (contract.Evidence, error) {
	pid := depthEntityPID(in.Scope)
	regions := procfs.ParseSmaps(depthRaw(in.Sample.T1, "proc.smaps"), 256)
	return contract.Evidence{ID: "ev-process-" + pid + "-maps", Capability: "process.memory_maps", Entity: contract.Entity{Kind: contract.EntityProcess, ID: "pid:" + pid}, Dimension: contract.DimensionMemory, Level: contract.L4Mechanism, CollectedAt: in.Sample.T1.At, Facts: regions, Observations: []contract.Observation{{Key: "proc.memory_map_regions", Value: float64(len(regions)), Unit: "count"}}, Sources: []string{"/proc/" + pid + "/smaps"}, Verify: []string{"sed -n '1,200p' /proc/" + pid + "/smaps"}}, nil
}
func depthRaw(s source.Snapshot, k string) []byte {
	r := s.Reads[k]
	if len(r) == 0 {
		return nil
	}
	return r[0].Data
}
func depthIndex(rs []source.Raw) map[string][]byte {
	m := map[string][]byte{}
	for _, r := range rs {
		m[r.Path] = r.Data
	}
	return m
}
func fdNumber(p string) int   { b := filepath.Base(p); v, _ := strconv.Atoi(b); return v }
func parsePID(s string) int64 { v, _ := strconv.ParseInt(s, 10, 64); return v }
func depthEntityPID(e contract.Entity) string {
	v := strings.TrimPrefix(e.ID, "pid:")
	if _, err := strconv.Atoi(v); err != nil {
		return "0"
	}
	return v
}
func depthDU(a, b uint64) uint64 {
	if b >= a {
		return b - a
	}
	return 0
}
