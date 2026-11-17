// Package inventory takes one read-only, two-point snapshot of a Linux host and
// accounts for every resource in human terms: where each byte of RAM, each core of
// CPU and each byte of every disk is going. The result is a plain, JSON-encodable
// Snapshot; every renderer (terminal, HTML, email, text) reads only this struct, so
// collection and presentation never depend on each other.
//
// Sources follow the kernel's own documentation and the established tools that use
// them: /proc/meminfo and /proc/<pid>/{stat,status,io,smaps_rollup} (proc(5),
// htop, smem), /proc/stat and /proc/pressure/* (mpstat, PSI), /proc/diskstats and
// /sys/block (iostat, lsblk), statfs(2) (df), /sys/class/net (ip, ethtool) and the
// cgroup v2 interface files (systemd-cgtop).
package inventory

import "time"

// Schema is bumped whenever a field changes meaning.
const Schema = 1

// Snapshot is the complete, renderer-independent picture of the host.
type Snapshot struct {
	Schema      int           `json:"schema"`
	CollectedAt time.Time     `json:"collected_at"`
	Window      time.Duration `json:"window_ns"` // time between the two samples
	Host        Host          `json:"host"`
	CPU         CPU           `json:"cpu"`
	Memory      Memory        `json:"memory"`
	Disks       []Disk        `json:"disks,omitempty"`
	IOPSI       PSI           `json:"io_psi"` // /proc/pressure/io
	NICs        []NIC         `json:"nics,omitempty"`
	Net         NetSummary    `json:"net"`
	Tasks       Tasks         `json:"tasks"`
	Services    []Service     `json:"services,omitempty"`  // grouped owners, heaviest first
	Processes   []Process     `json:"processes,omitempty"` // top processes (bounded)
	Listeners   []Listener    `json:"listeners,omitempty"` // every listening TCP/UDP socket (ss -tulpn)
	Checks      []Check       `json:"checks,omitempty"`    // host runtime checks (report)
	Notes       []string      `json:"notes,omitempty"`     // what could not be read, and why
}

// Host is the fixed identity of the machine.
type Host struct {
	Hostname  string        `json:"hostname"`
	PrimaryIP string        `json:"primary_ip,omitempty"`
	PublicIP  string        `json:"public_ip,omitempty"`
	OS        string        `json:"os,omitempty"`
	Kernel    string        `json:"kernel,omitempty"` // release, e.g. 6.8.0-45-generic
	Arch      string        `json:"arch,omitempty"`
	CPUModel  string        `json:"cpu_model,omitempty"`
	CPUMHz    float64       `json:"cpu_mhz,omitempty"`
	Vendor    string        `json:"vendor,omitempty"`  // DMI sys_vendor, e.g. "Amazon EC2"
	Product   string        `json:"product,omitempty"` // DMI product_name, e.g. "t3.large"
	BootTime  time.Time     `json:"boot_time"`
	Uptime    time.Duration `json:"uptime_ns"`
}

// PSI is one /proc/pressure/<resource> file (percent of wall time stalled).
type PSI struct {
	SomeAvg10  float64 `json:"some_avg10"`
	SomeAvg60  float64 `json:"some_avg60"`
	SomeAvg300 float64 `json:"some_avg300"`
	FullAvg10  float64 `json:"full_avg10"`
	FullAvg60  float64 `json:"full_avg60"`
	Available  bool    `json:"available"`
}

// Share is one named consumer inside a bucket.
type Share struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"` // bytes for memory/disk, cores for CPU
}

// Bucket is one slice of a zero-sum breakdown. The buckets of a breakdown always
// add up to its total, so a reader can account for every byte or core.
type Bucket struct {
	Key    string  `json:"key"`
	Label  string  `json:"label"`
	Source string  `json:"source"` // the kernel counters it is made of
	Value  float64 `json:"value"`  // bytes or cores
	Parts  []Share `json:"parts,omitempty"`
}

// CPU is the machine's compute capacity and where it went during the window.
type CPU struct {
	Cores       int       `json:"cores"`
	BusyPct     float64   `json:"busy_pct"`
	UserPct     float64   `json:"user_pct"` // user + nice
	SystemPct   float64   `json:"system_pct"`
	IRQPct      float64   `json:"irq_pct"` // irq + softirq
	IOWaitPct   float64   `json:"iowait_pct"`
	StealPct    float64   `json:"steal_pct"`
	IdlePct     float64   `json:"idle_pct"`
	Load1       float64   `json:"load1"`
	Load5       float64   `json:"load5"`
	Load15      float64   `json:"load15"`
	CtxPerSec   float64   `json:"ctx_switches_per_sec"`
	PSI         PSI       `json:"psi"`
	Buckets     []Bucket  `json:"buckets"` // in cores; sums to Cores
	PerCoreBusy []float64 `json:"per_core_busy_pct,omitempty"`
}

// Memory is RAM accounted to the byte. Buckets sum to Accounted (physical RAM when
// the kernel exposes it, otherwise MemTotal).
type Memory struct {
	PhysicalBytes  uint64   `json:"physical_bytes,omitempty"` // online memory blocks (lsmem)
	TotalBytes     uint64   `json:"total_bytes"`              // MemTotal
	AccountedBytes uint64   `json:"accounted_bytes"`
	AvailableBytes uint64   `json:"available_bytes"` // MemAvailable — what apps can still get
	FreeBytes      uint64   `json:"free_bytes"`
	UsedBytes      uint64   `json:"used_bytes"` // Total - Available (free(1) "used")
	UsedPct        float64  `json:"used_pct"`
	SwapTotal      uint64   `json:"swap_total"`
	SwapUsed       uint64   `json:"swap_used"`
	SwapInBPS      float64  `json:"swap_in_bps"`
	SwapOutBPS     float64  `json:"swap_out_bps"`
	DirtyBytes     uint64   `json:"dirty_bytes"`
	WritebackBytes uint64   `json:"writeback_bytes"`
	CommittedBytes uint64   `json:"committed_bytes"`
	CommitLimit    uint64   `json:"commit_limit"`
	MajorFaultsPS  float64  `json:"major_faults_per_sec"`
	OOMKills       uint64   `json:"oom_kills"` // since boot (vmstat oom_kill)
	PSI            PSI      `json:"psi"`
	Buckets        []Bucket `json:"buckets"`
	PSSAccurate    bool     `json:"pss_accurate"` // true when smaps_rollup was readable
}

// Disk is one physical (or virtual) block device and the space on it.
type Disk struct {
	Name        string  `json:"name"` // nvme0n1, sda, vda
	Model       string  `json:"model,omitempty"`
	Kind        string  `json:"kind"` // NVMe, SSD, HDD, Virtual
	SizeBytes   uint64  `json:"size_bytes"`
	SectorBytes uint64  `json:"sector_bytes,omitempty"`
	ReadBPS     float64 `json:"read_bps"`
	WriteBPS    float64 `json:"write_bps"`
	ReadIOPS    float64 `json:"read_iops"`
	WriteIOPS   float64 `json:"write_iops"`
	UtilPct     float64 `json:"util_pct"`
	AwaitMS     float64 `json:"await_ms"`
	QueueDepth  float64 `json:"queue_depth"` // aqu-sz
	// Buckets partition SizeBytes exactly: used per mount, free, reserved/metadata,
	// swap, unmounted and unpartitioned space.
	Buckets []Bucket   `json:"buckets"`
	Mounts  []MountUse `json:"mounts,omitempty"`
}

// MountUse is one mounted filesystem living on a disk.
type MountUse struct {
	Path       string  `json:"path"`
	Device     string  `json:"device"` // nvme0n1p1, or the LVM/dm name
	FSType     string  `json:"fstype"`
	SizeBytes  uint64  `json:"size_bytes"` // the partition/volume
	TotalBytes uint64  `json:"total_bytes"`
	UsedBytes  uint64  `json:"used_bytes"`
	AvailBytes uint64  `json:"avail_bytes"`
	Reserved   uint64  `json:"reserved_bytes"` // root-reserved blocks (Bfree - Bavail)
	UsedPct    float64 `json:"used_pct"`       // df "Use%"
	InodePct   float64 `json:"inode_used_pct"`
	TopDirs    []Share `json:"top_dirs,omitempty"`
	ScanNote   string  `json:"scan_note,omitempty"`
}

// NIC is one network interface.
type NIC struct {
	Name      string   `json:"name"`
	Addrs     []string `json:"addrs,omitempty"`
	SpeedMbps int      `json:"speed_mbps,omitempty"` // 0 when the driver does not report it
	Duplex    string   `json:"duplex,omitempty"`
	State     string   `json:"state,omitempty"`
	MTU       int      `json:"mtu,omitempty"`
	RxBPS     float64  `json:"rx_bps"`
	TxBPS     float64  `json:"tx_bps"`
	RxPPS     float64  `json:"rx_pps"`
	TxPPS     float64  `json:"tx_pps"`
	RxDrops   uint64   `json:"rx_drops"` // since boot
	TxDrops   uint64   `json:"tx_drops"`
	RxErrors  uint64   `json:"rx_errors"`
	TxErrors  uint64   `json:"tx_errors"`
	DropsPS   float64  `json:"drops_per_sec"`
}

// NetSummary is the machine-wide TCP picture.
type NetSummary struct {
	TCPInUse        uint64  `json:"tcp_in_use"`
	TCPTimeWait     uint64  `json:"tcp_time_wait"`
	TCPOrphan       uint64  `json:"tcp_orphan"`
	RetransPerSec   float64 `json:"retrans_per_sec"`
	ListenOverflows uint64  `json:"listen_overflows"` // since boot
	FDUsed          uint64  `json:"fd_used"`
	FDMax           uint64  `json:"fd_max"`
}

// Tasks counts processes by state.
type Tasks struct {
	Total   int `json:"total"`
	Running int `json:"running"`
	Blocked int `json:"blocked"` // D state (uninterruptible, usually I/O)
	Zombie  int `json:"zombie"`
	Threads int `json:"threads"`
}

// Service is a group of processes with one owner: a systemd unit, a container, or
// a standalone program.
type Service struct {
	Key        string       `json:"key"`
	Name       string       `json:"name"`
	Kind       string       `json:"kind"` // systemd, container, program, kernel
	Unit       string       `json:"unit,omitempty"`
	CgroupPath string       `json:"cgroup_path,omitempty"`
	Procs      int          `json:"procs"`
	MainPID    int          `json:"main_pid"`
	Exe        string       `json:"exe,omitempty"`
	Cmdline    string       `json:"cmdline,omitempty"`
	User       string       `json:"user,omitempty"`
	CPUCores   float64      `json:"cpu_cores"`
	MemBytes   uint64       `json:"mem_bytes"`  // PSS where readable, else RSS
	AnonBytes  uint64       `json:"anon_bytes"` // private/anonymous share (zero-sum bucket)
	SwapBytes  uint64       `json:"swap_bytes"`
	ReadBPS    float64      `json:"read_bps"`
	WriteBPS   float64      `json:"write_bps"`
	OOMScore   int          `json:"oom_score"`
	OOMAdj     int          `json:"oom_score_adj"`
	Ports      []int        `json:"ports,omitempty"`
	Cgroup     *CgroupStats `json:"cgroup,omitempty"`

	// The live shape of the workload: how it runs, what it holds open, who talks
	// to it and whom it depends on.
	StartedAt     time.Time  `json:"started_at,omitempty"` // main process start
	Threads       int        `json:"threads,omitempty"`
	FDs           int        `json:"fds,omitempty"`      // open file descriptors, all processes
	FDLimit       uint64     `json:"fd_limit,omitempty"` // main process soft RLIMIT_NOFILE
	Parents       []string   `json:"parents,omitempty"`  // launch chain of the main process, e.g. "containerd-shim (812)"
	Listen        []Listener `json:"listen,omitempty"`   // sockets it accepts on, with connected clients
	Peers         []Peer     `json:"peers,omitempty"`    // established outbound connections, grouped
	Systemd       *UnitInfo  `json:"systemd,omitempty"`
	HotThreads    []Thread   `json:"hot_threads,omitempty"` // busiest threads of the busiest process
	HotThreadsPID int        `json:"hot_threads_pid,omitempty"`

	userCores, sysCores float64 // split of CPUCores, for the CPU breakdown only
	pids                []int
}

// CgroupStats are the cgroup v2 interface files for a service (systemd-cgtop).
type CgroupStats struct {
	MemCurrent   uint64  `json:"memory_current"`
	MemMax       uint64  `json:"memory_max,omitempty"` // 0 = unlimited
	MemPSISome10 float64 `json:"memory_psi_some_avg10"`
	MemPSIFull10 float64 `json:"memory_psi_full_avg10"`
	CPUPSISome10 float64 `json:"cpu_psi_some_avg10"`
	IOPSISome10  float64 `json:"io_psi_some_avg10"`
	OOMKills     uint64  `json:"oom_kills"`
	ThrottledPct float64 `json:"cpu_throttled_pct"` // share of periods throttled in the window
	CPUMaxCores  float64 `json:"cpu_max_cores,omitempty"`
	PSIAvailable bool    `json:"psi_available"`

	MemPeak      uint64  `json:"memory_peak,omitempty"` // memory.peak: high-water mark since start
	MemHigh      uint64  `json:"memory_high,omitempty"` // memory.high: throttle point, 0 = none
	SwapCurrent  uint64  `json:"memory_swap_current,omitempty"`
	CPUUsageSec  float64 `json:"cpu_usage_sec,omitempty"`  // cpu.stat usage_usec, lifetime
	IOReadTotal  uint64  `json:"io_read_bytes,omitempty"`  // io.stat rbytes, lifetime
	IOWriteTotal uint64  `json:"io_write_bytes,omitempty"` // io.stat wbytes, lifetime
	PidsCurrent  uint64  `json:"pids_current,omitempty"`
	PidsMax      uint64  `json:"pids_max,omitempty"` // 0 = unlimited
}

// Listener is one listening socket and the clients connected to it right now.
type Listener struct {
	Proto   string `json:"proto"` // tcp, tcp6, udp, udp6
	Addr    string `json:"addr"`  // bind address; 0.0.0.0 / :: = every interface
	Port    int    `json:"port"`
	Clients int    `json:"clients,omitempty"` // established connections on this port
	Service string `json:"service,omitempty"`
	PID     int    `json:"pid,omitempty"`
	Public  bool   `json:"public"` // bound to a non-loopback address
}

// Peer is a group of established outbound connections to one remote endpoint.
type Peer struct {
	Addr  string `json:"addr"`
	Port  int    `json:"port"`
	Conns int    `json:"conns"`
	Name  string `json:"name,omitempty"` // local service owning that port, or the well-known protocol
}

// UnitInfo is what systemd knows about a service (systemctl show).
type UnitInfo struct {
	Description string    `json:"description,omitempty"`
	File        string    `json:"file,omitempty"` // FragmentPath
	DropIns     []string  `json:"drop_ins,omitempty"`
	Type        string    `json:"type,omitempty"`
	Restart     string    `json:"restart,omitempty"`
	Restarts    int       `json:"restarts"` // NRestarts
	ActiveSince time.Time `json:"active_since,omitempty"`
	State       string    `json:"state,omitempty"` // ActiveState/SubState
	User        string    `json:"user,omitempty"`
	WorkingDir  string    `json:"working_dir,omitempty"`
	DependsOn   []string  `json:"depends_on,omitempty"` // Requires/Wants/BindsTo/After, noise filtered
	MemoryMax   string    `json:"memory_max,omitempty"`
	CPUQuota    string    `json:"cpu_quota,omitempty"`
	LimitNOFILE string    `json:"limit_nofile,omitempty"`
}

// Thread is one thread of a process (top -H).
type Thread struct {
	TID   int     `json:"tid"`
	Name  string  `json:"name"`
	Cores float64 `json:"cores"`
	State string  `json:"state"`
}

// Check is one host runtime check: a fact a senior engineer looks at first,
// with the command that verifies it.
type Check struct {
	ID    string `json:"id"`
	Area  string `json:"area"` // memory, files, network, time, logs, security, updates, services, storage
	Name  string `json:"name"`
	State string `json:"state"` // ok, warn, info
	Value string `json:"value"`
	Hint  string `json:"hint,omitempty"`
	Cmd   string `json:"cmd,omitempty"`
}

// Process is one process, the way htop shows it with the full program path.
type Process struct {
	PID       int       `json:"pid"`
	PPID      int       `json:"ppid"`
	Comm      string    `json:"comm"`
	Exe       string    `json:"exe,omitempty"`
	ExeGone   bool      `json:"exe_deleted,omitempty"` // binary replaced on disk since start
	Cmdline   string    `json:"cmdline,omitempty"`     // secrets redacted
	User      string    `json:"user,omitempty"`
	State     string    `json:"state"`
	Service   string    `json:"service"`
	CPUCores  float64   `json:"cpu_cores"`
	UserCores float64   `json:"user_cores"`
	SysCores  float64   `json:"sys_cores"`
	RSSBytes  uint64    `json:"rss_bytes"`
	PSSBytes  uint64    `json:"pss_bytes,omitempty"`
	AnonBytes uint64    `json:"anon_bytes"`
	SwapBytes uint64    `json:"swap_bytes"`
	ReadBPS   float64   `json:"read_bps"`
	WriteBPS  float64   `json:"write_bps"`
	Threads   int       `json:"threads"`
	FDs       int       `json:"fds,omitempty"`
	FDLimit   uint64    `json:"fd_limit,omitempty"`
	OOMScore  int       `json:"oom_score"`
	OOMAdj    int       `json:"oom_score_adj"`
	StartedAt time.Time `json:"started_at"`
}
