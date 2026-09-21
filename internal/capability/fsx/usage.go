package fsx

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/faizahmd2/pinproc/internal/capability/spec"
	"github.com/faizahmd2/pinproc/internal/contract"
	"github.com/faizahmd2/pinproc/internal/procfs"
	"github.com/faizahmd2/pinproc/internal/source"
)

type MountFacts struct {
	Path, FSType, Device  string
	Major, Minor          uint64
	UsedPct, InodeUsedPct float64
	FreeBytes, TotalBytes uint64
}

type FilesystemFacts struct {
	Mounts []MountFacts
}

type UsageFacts struct {
	Mount            string
	TopDirectories   []string
	TopFiles         []string
	DeletedOpenBytes uint64
}

func Filesystem() spec.Capability {
	return spec.Capability{
		ID: "machine.filesystem", Dimension: contract.DimensionFilesystem, Level: contract.L1Machine,
		Kind: contract.KindSnapshot, Accepts: contract.EntityMachine, Cost: contract.CostLow,
		Summary: "per-mount capacity and inode usage", LeadsTo: []string{"fs.usage"},
		Reads: func(contract.Entity, contract.Facts) []source.Read {
			return []source.Read{{Key: "mountinfo", Path: "/proc/self/mountinfo", Kind: source.ReadFile}}
		},
		Parse: parseFilesystem,
	}
}

func Usage() spec.Capability {
	return spec.Capability{
		ID: "fs.usage", Dimension: contract.DimensionFilesystem, Level: contract.L2Owner,
		Kind: contract.KindSnapshot, Accepts: contract.EntityMount, Cost: contract.CostHigh,
		Summary: "bounded tree walk for largest directories and files",
		Reads:   func(contract.Entity, contract.Facts) []source.Read { return nil },
		Parse:   parseUsage,
	}
}

func parseFilesystem(in spec.ParseInput) (contract.Evidence, error) {
	mounts, err := procfs.ParseMountInfo(raw(in.Sample.T1, "mountinfo"))
	if err != nil {
		return contract.Evidence{}, err
	}
	rows := make([]MountFacts, 0, len(mounts))
	seen := map[string]bool{}
	for _, m := range mounts {
		if excluded(m.FSType) {
			continue
		}
		device := fmt.Sprintf("%d:%d", m.Major, m.Minor)
		key := device
		if m.FSType == "tmpfs" {
			key = "tmpfs:" + device
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		var st syscall.Statfs_t
		if err := syscall.Statfs(m.MountPoint, &st); err != nil {
			continue
		}
		if st.Blocks == 0 {
			continue
		}
		used := float64(st.Blocks-st.Bfree) / float64(st.Blocks) * 100
		inode := 0.0
		if st.Files > 0 {
			inode = float64(st.Files-st.Ffree) / float64(st.Files) * 100
		}
		rows = append(rows, MountFacts{
			Path: m.MountPoint, FSType: m.FSType, Device: m.Source, Major: m.Major, Minor: m.Minor,
			UsedPct: used, InodeUsedPct: inode,
			FreeBytes:  uint64(st.Bavail) * uint64(st.Bsize),
			TotalBytes: uint64(st.Blocks) * uint64(st.Bsize),
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Path < rows[j].Path })
	obs := make([]contract.Observation, 0, len(rows)*4)
	for _, r := range rows {
		obs = append(obs,
			contract.Observation{Key: "fs.used_pct:" + r.Path, Value: r.UsedPct, Unit: "percent"},
			contract.Observation{Key: "fs.inode_used_pct:" + r.Path, Value: r.InodeUsedPct, Unit: "percent"},
			contract.Observation{Key: "fs.free_bytes:" + r.Path, Value: float64(r.FreeBytes), Unit: "bytes"},
			contract.Observation{Key: "fs.total_bytes:" + r.Path, Value: float64(r.TotalBytes), Unit: "bytes"},
		)
	}
	return contract.Evidence{
		ID: "ev-machine-filesystem", Capability: "machine.filesystem",
		Entity:    contract.Entity{Kind: contract.EntityMachine, ID: "machine"},
		Dimension: contract.DimensionFilesystem, Level: contract.L1Machine, CollectedAt: in.Sample.T1.At,
		Facts: FilesystemFacts{Mounts: rows}, Observations: obs,
		Sources: []string{"/proc/self/mountinfo"}, Verify: []string{"cat /proc/self/mountinfo"},
	}, nil
}

func parseUsage(in spec.ParseInput) (contract.Evidence, error) {
	mount := strings.TrimPrefix(in.Scope.ID, "mount:")
	if mount == "" {
		return contract.Evidence{}, fmt.Errorf("mount scope is empty")
	}

	deadline := time.Now().Add(3 * time.Second)
	entries := 0
	dirSizes := map[string]uint64{}
	type fileRow struct {
		Path string
		Size uint64
	}
	var files []fileRow

	depth := func(p string) int {
		rel := strings.TrimPrefix(strings.TrimPrefix(p, mount), "/")
		if rel == "" {
			return 0
		}
		return len(strings.Split(rel, "/"))
	}

	err := filepath.WalkDir(mount, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		entries++
		if entries > 20000 {
			return filepath.SkipAll
		}
		if entries%128 == 0 && time.Now().After(deadline) {
			return filepath.SkipAll
		}
		if depth(path) > 6 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, ie := d.Info()
		if ie != nil {
			return nil
		}
		size := uint64(0)
		if info.Mode().IsRegular() {
			size = uint64(info.Size())
			files = append(files, fileRow{Path: path, Size: size})
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(path, mount), "/")
		var parts []string
		if rel != "" {
			parts = strings.Split(rel, "/")
		}
		dirSizes[mount] += size
		for i := 1; i < len(parts); i++ {
			dirSizes[filepath.Join(mount, filepath.Join(parts[:i]...))] += size
		}
		return nil
	})
	if err != nil && err != filepath.SkipAll {
		return contract.Evidence{}, err
	}

	sort.Slice(files, func(i, j int) bool { return files[i].Size > files[j].Size })
	if len(files) > 10 {
		files = files[:10]
	}
	type dirRow struct {
		Path string
		Size uint64
	}
	dirs := make([]dirRow, 0, len(dirSizes))
	for p, size := range dirSizes {
		dirs = append(dirs, dirRow{Path: p, Size: size})
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].Size > dirs[j].Size })
	if len(dirs) > 10 {
		dirs = dirs[:10]
	}

	f := UsageFacts{Mount: mount}
	if deleted := deletedOpenOnMount(in, mount); deleted > 0 {
		f.DeletedOpenBytes = deleted
		f.TopDirectories = append(f.TopDirectories, fmt.Sprintf("deleted-but-open files: %d bytes held under %s", deleted, mount))
	}
	for _, d := range dirs {
		f.TopDirectories = append(f.TopDirectories, fmt.Sprintf("%s (%d bytes)", d.Path, d.Size))
	}
	for _, x := range files {
		f.TopFiles = append(f.TopFiles, fmt.Sprintf("%s (%d bytes)", x.Path, x.Size))
	}
	return contract.Evidence{
		ID: "ev-mount-" + sanitize(mount) + "-usage", Capability: "fs.usage", Entity: in.Scope,
		Dimension: contract.DimensionFilesystem, Level: contract.L2Owner, CollectedAt: in.Sample.T1.At,
		Facts: f, Derived: append(append([]string{}, f.TopDirectories...), f.TopFiles...),
		Sources: []string{mount}, Verify: []string{"inspect top files under " + mount},
	}, nil
}

func excluded(fsName string) bool {
	switch fsName {
	case "proc", "sysfs", "cgroup", "cgroup2", "devpts", "securityfs", "debugfs", "tracefs", "mqueue", "pstore", "bpf", "autofs":
		return true
	}
	return false
}

func sanitize(s string) string {
	return strings.NewReplacer("/", "-", " ", "_").Replace(strings.Trim(s, "/"))
}

func raw(s source.Snapshot, k string) []byte {
	r := s.Reads[k]
	if len(r) == 0 {
		return nil
	}
	return r[0].Data
}

func deletedOpenOnMount(in spec.ParseInput, mount string) uint64 {
	var total uint64
	for _, e := range in.Prior {
		if e.Capability != "process.files" {
			continue
		}
		var f struct {
			Samples []struct {
				Target string `json:"Target"`
			} `json:"Samples"`
		}
		b, _ := json.Marshal(e.Facts)
		if json.Unmarshal(b, &f) != nil {
			continue
		}
		for _, s := range f.Samples {
			if !strings.HasSuffix(s.Target, " (deleted)") {
				continue
			}
			p := strings.TrimSuffix(s.Target, " (deleted)")
			if p == mount || strings.HasPrefix(p, mount+string(filepath.Separator)) {
				for _, o := range e.Observations {
					if o.Key == "proc.fd_deleted_bytes" {
						total += uint64(o.Value)
						break
					}
				}
				break
			}
		}
	}
	return total
}
