package inventory

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/faizahmd2/pinproc/internal/procfs"
)

// disks accounts each block device to the byte, the way lsblk + df would if they
// were read together: every partition (and the LVM volumes inside it) becomes
// "used on <mount>", free, reserved/metadata, swap or "not mounted"; whatever no
// partition covers is "unpartitioned". The buckets of a disk sum to its size.
func (c *Collector) disks(a, b *sample, secs float64, notes *[]string) []Disk {
	ents, err := os.ReadDir(filepath.Join(c.opt.Sys, "block"))
	if err != nil {
		return nil
	}
	mounts := c.mountsByDev()
	swaps := c.swapDevices()
	var deadline time.Time
	if c.opt.ScanDirs {
		deadline = time.Now().Add(c.opt.ScanBudget)
	}
	var out []Disk
	for _, e := range ents {
		name := e.Name()
		if skipBlock(name) {
			continue
		}
		dir := filepath.Join(c.opt.Sys, "block", name)
		size := readUint(filepath.Join(dir, "size")) * 512
		if size == 0 {
			continue
		}
		d := Disk{Name: name, SizeBytes: size, SectorBytes: readUint(filepath.Join(dir, "queue/logical_block_size"))}
		d.Model = strings.Join(strings.Fields(readString(filepath.Join(dir, "device/model"))), " ")
		d.Kind = diskKind(name, readString(filepath.Join(dir, "queue/rotational")))
		if x, ok := a.disks[name]; ok {
			if y, ok := b.disks[name]; ok && secs > 0 {
				ms := secs * 1000
				d.ReadBPS = rate(y.SectorsRead, x.SectorsRead, secs) * 512
				d.WriteBPS = rate(y.SectorsWritten, x.SectorsWritten, secs) * 512
				d.ReadIOPS = rate(y.ReadsCompleted, x.ReadsCompleted, secs)
				d.WriteIOPS = rate(y.WritesCompleted, x.WritesCompleted, secs)
				d.UtilPct = min(100, float64(sub(y.IOTicks, x.IOTicks))/ms*100)
				if ios := sub(y.ReadsCompleted, x.ReadsCompleted) + sub(y.WritesCompleted, x.WritesCompleted); ios > 0 {
					d.AwaitMS = float64(sub(y.ReadTicks, x.ReadTicks)+sub(y.WritesTicks, x.WritesTicks)) / float64(ios)
				}
				d.QueueDepth = float64(sub(y.TimeInQueue, x.TimeInQueue)) / ms
			}
		}

		// Partitions, or the whole disk when it carries a filesystem directly.
		type vol struct{ name, dir string }
		var parts []vol
		for _, pe := range readDirNames(dir) {
			if strings.HasPrefix(pe, name) {
				if _, err := os.Stat(filepath.Join(dir, pe, "partition")); err == nil {
					parts = append(parts, vol{pe, filepath.Join(dir, pe)})
				}
			}
		}
		if len(parts) == 0 {
			parts = []vol{{name, dir}}
		}
		acc := &diskAcc{}
		var covered uint64
		for _, p := range parts {
			psize := readUint(filepath.Join(p.dir, "size")) * 512
			if p.dir != dir {
				covered += psize
			}
			c.accountVolume(acc, &d, p.name, p.dir, psize, mounts, swaps, deadline)
		}
		if len(parts) == 1 && parts[0].dir == dir {
			covered = size
		}
		gap := sub(size, covered)
		if gap*1000 < size { // GPT tables and 1 MiB alignment: metadata, not usable space
			acc.add("reserved", "Reserved & filesystem metadata", "root-reserved blocks + journal/inode tables + partition table", "partition table & alignment", gap)
		} else {
			acc.add("unpartitioned", "Unpartitioned", "disk size − partitions", "", gap)
		}
		d.Buckets = acc.buckets()
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SizeBytes > out[j].SizeBytes })
	for _, d := range out {
		for _, m := range d.Mounts {
			if m.ScanNote != "" {
				*notes = append(*notes, "directory sizes on "+m.Path+": "+m.ScanNote)
			}
		}
	}
	return out
}

// accountVolume splits one partition (or a whole-disk volume) between its
// filesystem, LVM volumes, swap, or "not mounted".
func (c *Collector) accountVolume(acc *diskAcc, d *Disk, name, dir string, size uint64, mounts map[string]mountRow, swaps map[string]bool, deadline time.Time) {
	if swaps[name] {
		acc.add("swap", "Swap", "/proc/swaps", name, size)
		return
	}
	dev := readString(filepath.Join(dir, "dev"))
	if m, ok := mounts[dev]; ok {
		c.accountFS(acc, d, name, size, m, deadline)
		return
	}
	// LVM / dm-crypt / md: the space belongs to the volumes stacked on top.
	holders := readDirNames(filepath.Join(dir, "holders"))
	if len(holders) > 0 {
		var used uint64
		for _, h := range holders {
			hdir := filepath.Join(c.opt.Sys, "block", h)
			hsize := min(readUint(filepath.Join(hdir, "size"))*512, sub(size, used))
			used += hsize
			hname := readString(filepath.Join(hdir, "dm/name"))
			if hname == "" {
				hname = h
			}
			if swaps[h] {
				acc.add("swap", "Swap", "/proc/swaps", hname, hsize)
				continue
			}
			if m, ok := mounts[readString(filepath.Join(hdir, "dev"))]; ok {
				c.accountFS(acc, d, hname, hsize, m, deadline)
				continue
			}
			acc.add("unmounted", "Not mounted", "partition without a mounted filesystem", hname, hsize)
		}
		acc.add("unmounted", "Not mounted", "partition without a mounted filesystem", name+" (volume group free)", sub(size, used))
		return
	}
	acc.add("unmounted", "Not mounted", "partition without a mounted filesystem", name, size)
}

func (c *Collector) accountFS(acc *diskAcc, d *Disk, device string, size uint64, m mountRow, deadline time.Time) {
	var st syscall.Statfs_t
	if syscall.Statfs(m.path, &st) != nil || st.Blocks == 0 {
		acc.add("unmounted", "Not mounted", "statfs failed", device, size)
		return
	}
	bs := uint64(st.Bsize)
	fsTotal := uint64(st.Blocks) * bs
	used := (uint64(st.Blocks) - uint64(st.Bfree)) * bs
	avail := uint64(st.Bavail) * bs
	reserved := sub(uint64(st.Bfree), uint64(st.Bavail)) * bs
	meta := sub(size, fsTotal)
	if fsTotal > size && size > 0 { // thin/virtual volumes: trust the partition size
		used = min(used, size)
		avail = sub(size, used+reserved)
		meta = 0
	}
	mu := MountUse{Path: m.path, Device: device, FSType: m.fstype, SizeBytes: size, TotalBytes: fsTotal,
		UsedBytes: used, AvailBytes: avail, Reserved: reserved}
	if used+avail > 0 {
		mu.UsedPct = float64(used) / float64(used+avail) * 100 // df's Use%
	}
	if st.Files > 0 {
		mu.InodePct = float64(uint64(st.Files)-uint64(st.Ffree)) / float64(st.Files) * 100
	}
	if !deadline.IsZero() {
		mu.TopDirs, mu.ScanNote = scanDirs(m.path, deadline, used)
	}
	d.Mounts = append(d.Mounts, mu)
	acc.addUsed(m.path, used, mu.TopDirs)
	acc.add("free", "Free", "statfs f_bavail", m.path, avail)
	acc.add("reserved", "Reserved & filesystem metadata", "root-reserved blocks + journal/inode tables", m.path, reserved+meta)
}

// diskAcc collects buckets in a stable display order.
type diskAcc struct {
	used  []Bucket
	other map[string]*Bucket
	order []string
}

func (a *diskAcc) addUsed(path string, v uint64, dirs []Share) {
	if v == 0 {
		return
	}
	a.used = append(a.used, Bucket{Key: "used:" + path, Label: "Used · " + path, Source: "statfs f_blocks − f_bfree", Value: float64(v), Parts: dirs})
}

func (a *diskAcc) add(key, label, source, part string, v uint64) {
	if v == 0 {
		return
	}
	if a.other == nil {
		a.other = map[string]*Bucket{}
	}
	b := a.other[key]
	if b == nil {
		b = &Bucket{Key: key, Label: label, Source: source}
		a.other[key] = b
		a.order = append(a.order, key)
	}
	b.Value += float64(v)
	if part != "" {
		b.Parts = append(b.Parts, Share{Name: part, Value: float64(v)})
	}
}

func (a *diskAcc) buckets() []Bucket {
	out := append([]Bucket(nil), a.used...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Value > out[j].Value })
	for _, k := range []string{"swap", "reserved", "unmounted", "unpartitioned", "free"} {
		if b, ok := a.other[k]; ok {
			if len(b.Parts) == 1 && k != "unmounted" {
				b.Parts = nil // a single part just repeats the bucket
			}
			out = append(out, *b)
		}
	}
	return out
}

func skipBlock(name string) bool {
	for _, p := range []string{"loop", "ram", "zram", "sr", "fd", "dm-", "md", "nbd", "mtdblock"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

func diskKind(name, rotational string) string {
	switch {
	case strings.HasPrefix(name, "nvme"):
		return "NVMe"
	case strings.HasPrefix(name, "vd"):
		return "Virtual (virtio)"
	case strings.HasPrefix(name, "xvd"):
		return "Virtual (Xen)"
	case rotational == "1":
		return "HDD"
	}
	return "SSD"
}

type mountRow struct {
	path, fstype string
	whole        bool
}

// mountsByDev maps "major:minor" to the shortest real mount of that device (bind
// mounts and btrfs subvolumes of the same device collapse onto it).
func (c *Collector) mountsByDev() map[string]mountRow {
	out := map[string]mountRow{}
	mis, err := procfs.ParseMountInfo(readFile(filepath.Join(c.opt.Proc, "self/mountinfo")))
	if err != nil {
		return out
	}
	for _, m := range mis {
		if m.Major == 0 { // pseudo filesystems
			continue
		}
		key := itoa(m.Major) + ":" + itoa(m.Minor)
		row := mountRow{path: m.MountPoint, fstype: m.FSType, whole: m.Root == "/"}
		cur, ok := out[key]
		// Prefer the mount of the filesystem's own root over bind mounts of a
		// subdirectory, then the shortest path.
		if !ok || (row.whole && !cur.whole) || (row.whole == cur.whole && len(row.path) < len(cur.path)) {
			out[key] = row
		}
	}
	return out
}

func (c *Collector) swapDevices() map[string]bool {
	out := map[string]bool{}
	for i, line := range strings.Split(string(readFile(filepath.Join(c.opt.Proc, "swaps"))), "\n") {
		f := strings.Fields(line)
		if i == 0 || len(f) < 2 || f[1] != "partition" {
			continue
		}
		dev := f[0]
		if r, err := filepath.EvalSymlinks(dev); err == nil {
			dev = r
		}
		out[filepath.Base(dev)] = true
	}
	return out
}

func readDirNames(dir string) []string {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(ents))
	for _, e := range ents {
		out = append(out, e.Name())
	}
	return out
}

func itoa(v uint64) string {
	if v == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}

// scanDirs finds where a filesystem's space went, like `ncdu` would, inside a
// shared time budget: it sums allocated blocks per directory (never crossing into
// another mount), then drills into the largest directory while one child holds
// most of it, so "/var 27 GiB" becomes "/var/lib/postgresql/16 21 GiB".
func scanDirs(root string, deadline time.Time, used uint64) ([]Share, string) {
	var rootStat syscall.Stat_t
	if syscall.Lstat(root, &rootStat) != nil {
		return nil, "unreadable"
	}
	dev := uint64(rootStat.Dev)
	const maxDepth = 4
	sizes := map[string]uint64{}
	entries, truncated, denied := 0, false, false
	rel := func(p string) []string {
		r := strings.TrimPrefix(strings.TrimPrefix(p, root), "/")
		if r == "" {
			return nil
		}
		return strings.Split(r, "/")
	}
	_ = filepath.WalkDir(root, func(path string, de fs.DirEntry, err error) error {
		if err != nil {
			if os.IsPermission(err) {
				denied = true
			}
			if de != nil && de.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		entries++
		if entries&1023 == 0 && time.Now().After(deadline) {
			truncated = true
			return fs.SkipAll
		}
		info, err := de.Info()
		if err != nil {
			return nil
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return nil
		}
		if de.IsDir() && uint64(st.Dev) != dev {
			return fs.SkipDir // another filesystem is mounted here
		}
		blocks := uint64(st.Blocks) * 512
		parts := rel(path)
		if de.IsDir() {
			parts = parts[:max(0, len(parts))]
		} else if len(parts) > 0 {
			parts = parts[:len(parts)-1] // a file counts toward its directories
		}
		for i := 1; i <= len(parts) && i <= maxDepth; i++ {
			sizes[strings.Join(parts[:i], "/")] += blocks
		}
		return nil
	})

	children := map[string][]string{}
	for p := range sizes {
		parent := ""
		if i := strings.LastIndexByte(p, '/'); i >= 0 {
			parent = p[:i]
		}
		children[parent] = append(children[parent], p)
	}
	frontier := append([]string(nil), children[""]...)
	for range 8 {
		sort.Slice(frontier, func(i, j int) bool { return sizes[frontier[i]] > sizes[frontier[j]] })
		if len(frontier) == 0 {
			break
		}
		big := frontier[0]
		kids := children[big]
		if len(kids) == 0 {
			break
		}
		var best uint64
		for _, k := range kids {
			best = max(best, sizes[k])
		}
		if best*10 < sizes[big]*6 { // no single child holds 60%: stop drilling
			break
		}
		frontier = append(frontier[1:], kids...)
	}
	shares := make([]Share, 0, len(frontier))
	for _, p := range frontier {
		shares = append(shares, Share{Name: filepath.Join(root, p), Value: float64(sizes[p])})
	}
	shares = topShares(shares, 6, float64(used), "everything else")
	note := ""
	switch {
	case truncated:
		note = "partial (time budget reached) — sizes are lower bounds"
	case denied:
		note = "some directories unreadable — run as root for complete sizes"
	}
	return shares, note
}
