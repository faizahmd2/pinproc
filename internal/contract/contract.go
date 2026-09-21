package contract

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// SchemaVersion is the stable investigation JSON schema version.
const SchemaVersion = 1

// EntityKind identifies a resource-owning Linux entity.
type EntityKind string

const (
	EntityMachine   EntityKind = "machine"
	EntityCgroup    EntityKind = "cgroup"
	EntityContainer EntityKind = "container"
	EntityProcess   EntityKind = "process"
	EntityThread    EntityKind = "thread"
	EntityDevice    EntityKind = "device"
	EntityMount     EntityKind = "mount"
	EntitySocket    EntityKind = "socket"
)

// Dimension identifies the resource dimension under investigation.
type Dimension string

const (
	DimensionCPU        Dimension = "cpu"
	DimensionMemory     Dimension = "memory"
	DimensionIO         Dimension = "io"
	DimensionNetwork    Dimension = "network"
	DimensionScheduling Dimension = "scheduling"
	DimensionFilesystem Dimension = "filesystem"
	DimensionLimits     Dimension = "limits"
)

// Level identifies investigation depth.
type Level int

const (
	L1Machine   Level = 1
	L2Owner     Level = 2
	L3Execution Level = 3
	L4Mechanism Level = 4
	L5Deep      Level = 5
)

// CapabilityKind describes collection behavior.
type CapabilityKind string

const (
	KindSnapshot CapabilityKind = "snapshot"
	KindSampled  CapabilityKind = "sampled"
	KindObserved CapabilityKind = "observed"
)

// Cost describes relative collection cost.
type Cost string

const (
	CostLow    Cost = "low"
	CostMedium Cost = "medium"
	CostHigh   Cost = "high"
)

// Grade records how a hypothesis was established.
type Grade string

const (
	GradeObserved   Grade = "observed"
	GradeCorrelated Grade = "correlated"
	GradeInferred   Grade = "inferred"
)

// Entity is a thing that can own or consume a resource.
type Entity struct {
	Kind     EntityKind `json:"kind"`
	ID       string     `json:"id"`
	Display  string     `json:"display,omitempty"`
	ParentID string     `json:"parent_id,omitempty"`
	Service  *Service   `json:"service,omitempty"`
}

// Service is the human-facing identity of a process.
type Service struct {
	Name        string        `json:"name"`
	NameSource  Provenance    `json:"name_source"`
	Unit        string        `json:"unit,omitempty"`
	UnitFile    string        `json:"unit_file,omitempty"`
	Container   *ContainerRef `json:"container,omitempty"`
	Exe         string        `json:"exe,omitempty"`
	Cmdline     []string      `json:"cmdline,omitempty"`
	Cwd         string        `json:"cwd,omitempty"`
	User        string        `json:"user,omitempty"`
	ListenPorts []Port        `json:"listen_ports,omitempty"`
	LogPaths    []string      `json:"log_paths,omitempty"`
	ConfigPaths []string      `json:"config_paths,omitempty"`
	StartedAt   time.Time     `json:"started_at,omitempty"`
	CgroupPath  string        `json:"cgroup_path,omitempty"`
}

// Provenance describes how identity was established.
type Provenance string

const (
	ProvenanceSystemd Provenance = "systemd"
	ProvenanceDocker  Provenance = "docker"
	ProvenanceK8s     Provenance = "k8s"
	ProvenanceExe     Provenance = "exe"
	ProvenanceCmdline Provenance = "cmdline"
	ProvenancePort    Provenance = "port"
	ProvenanceUnknown Provenance = "unknown"
)

// ContainerRef identifies a container.
type ContainerRef struct {
	Runtime   string `json:"runtime"`
	ID        string `json:"id"`
	Name      string `json:"name,omitempty"`
	Image     string `json:"image,omitempty"`
	PodName   string `json:"pod_name,omitempty"`
	Namespace string `json:"namespace,omitempty"`
}

// Port identifies a listening socket.
type Port struct {
	Proto string `json:"proto"`
	Addr  string `json:"addr"`
	Port  int    `json:"port"`
}

// Observation is one normalized measurement.
type Observation struct {
	Key    string        `json:"key"`
	Value  float64       `json:"value"`
	Unit   string        `json:"unit"`
	Window time.Duration `json:"window_ns,omitempty"`
}

// Evidence is typed evidence emitted by a capability.
type Evidence struct {
	ID           string        `json:"id"`
	Capability   string        `json:"capability"`
	Entity       Entity        `json:"entity"`
	Dimension    Dimension     `json:"dimension"`
	Level        Level         `json:"level"`
	CollectedAt  time.Time     `json:"collected_at"`
	Window       time.Duration `json:"window_ns,omitempty"`
	Observations []Observation `json:"observations"`
	Facts        any           `json:"facts,omitempty"`
	Derived      []string      `json:"derived,omitempty"`
	Sources      []string      `json:"sources"`
	Verify       []string      `json:"verify,omitempty"`
	Unavailable  string        `json:"unavailable,omitempty"`
	TimedOut     bool          `json:"timed_out,omitempty"`
	Err          string        `json:"err,omitempty"`
}

// Hypothesis is a claim whose evidentiary grade is explicit.
type Hypothesis struct {
	ID          string      `json:"id"`
	Statement   string      `json:"statement"`
	Dimension   Dimension   `json:"dimension"`
	Entity      Entity      `json:"entity"`
	Grade       Grade       `json:"grade"`
	Support     []string    `json:"support"`
	Contradicts []string    `json:"contradicts,omitempty"`
	Confidence  float64     `json:"confidence"`
	Source      string      `json:"source"`
	LogContext  *LogContext `json:"log_context,omitempty"`
}

type LogContext struct {
	Path     string    `json:"path"`
	Line     string    `json:"line"`
	Count    int       `json:"count,omitempty"`
	LastSeen time.Time `json:"last_seen,omitempty"`
}

type Notice struct {
	Capability string `json:"capability"`
	Message    string `json:"message"`
	Count      int    `json:"count,omitempty"`
}

// Candidate is one legal next investigation.
type Candidate struct {
	Capability string  `json:"capability"`
	Scope      Entity  `json:"scope"`
	Score      float64 `json:"score"`
	Forced     bool    `json:"forced,omitempty"`
	Reason     string  `json:"reason,omitempty"`
}

// Step records a traversal decision.
type Step struct {
	Depth      Level         `json:"depth"`
	Capability string        `json:"capability"`
	Scope      string        `json:"scope"`
	DecidedBy  string        `json:"decided_by"`
	Reason     string        `json:"reason,omitempty"`
	Duration   time.Duration `json:"duration_ns,omitempty"`
	Bytes      int64         `json:"bytes,omitempty"`
	Err        string        `json:"err,omitempty"`
}

// Spend records accumulated investigation spend.
type Spend struct {
	Depth          int           `json:"depth"`
	Steps          int           `json:"steps"`
	Bytes          int64         `json:"bytes"`
	DecisionCalls  int           `json:"decision_calls"`
	NarrationCalls int           `json:"narration_calls"`
	Wall           time.Duration `json:"wall_ns"`
}

// Budget bounds one investigation.
type Budget struct {
	MaxDepth          int           `json:"max_depth"`
	MaxSteps          int           `json:"max_steps"`
	MaxWall           time.Duration `json:"max_wall_ns"`
	MaxBytes          int64         `json:"max_bytes"`
	MaxDecisionCalls  int           `json:"max_decision_calls"`
	MaxNarrationCalls int           `json:"max_narration_calls"`
	SampleWindow      time.Duration `json:"sample_window_ns"`
}

// BudgetFast returns a short budget.
func BudgetFast() Budget {
	return Budget{3, 12, 20 * time.Second, 2 << 20, 3, 1, 500 * time.Millisecond}
}

// BudgetNormal returns the default budget.
func BudgetNormal() Budget { return Budget{5, 30, 60 * time.Second, 8 << 20, 8, 1, time.Second} }

// BudgetDeep returns an expanded budget.
func BudgetDeep() Budget { return Budget{6, 60, 180 * time.Second, 32 << 20, 16, 1, 3 * time.Second} }

// StopReason explains why traversal ended.
type StopReason string

const (
	StopNoAnomaly          StopReason = "no_anomaly"
	StopSufficientEvidence StopReason = "sufficient_evidence"
	StopBudgetDepth        StopReason = "budget_depth"
	StopBudgetTime         StopReason = "budget_time"
	StopBudgetSteps        StopReason = "budget_steps"
	StopBudgetBytes        StopReason = "budget_bytes"
	StopDeadEnd            StopReason = "dead_end"
	StopUnavailable        StopReason = "capability_unavailable"
	StopError              StopReason = "error"
	StopInconclusive       StopReason = "inconclusive"
)

// Facts describes host capabilities.
type Facts struct {
	Kernel   string          `json:"kernel"`
	OSID     string          `json:"os_id"`
	OSLike   []string        `json:"os_like,omitempty"`
	CgroupV2 bool            `json:"cgroup_v2"`
	PSI      bool            `json:"psi"`
	Root     bool            `json:"root"`
	Has      map[string]bool `json:"has,omitempty"`
}

// MachineIdentity is host identity.
type MachineIdentity struct {
	Hostname     string        `json:"hostname"`
	PrimaryIP    string        `json:"primary_ip,omitempty"`
	MachineID    string        `json:"machine_id,omitempty"`
	OS           string        `json:"os,omitempty"`
	Kernel       string        `json:"kernel,omitempty"`
	Architecture string        `json:"architecture,omitempty"`
	CPUs         int           `json:"cpus,omitempty"`
	MemTotal     uint64        `json:"mem_total,omitempty"`
	Uptime       time.Duration `json:"uptime_ns,omitempty"`
}

// MachineSnapshot contains a compact current-state summary for the report.
type MachineSnapshot struct {
	CPUs                 int     `json:"cpus,omitempty"`
	CPUUtilizationPct    float64 `json:"cpu_utilization_pct,omitempty"`
	Load1                float64 `json:"load1,omitempty"`
	MemoryTotalBytes     uint64  `json:"memory_total_bytes,omitempty"`
	MemoryAvailableBytes uint64  `json:"memory_available_bytes,omitempty"`
	MemoryUsedBytes      uint64  `json:"memory_used_bytes,omitempty"`
	MemoryUsedPct        float64 `json:"memory_used_pct,omitempty"`
	SwapUsedPct          float64 `json:"swap_used_pct,omitempty"`
	RootDiskPath         string  `json:"root_disk_path,omitempty"`
	RootDiskTotalBytes   uint64  `json:"root_disk_total_bytes,omitempty"`
	RootDiskUsedBytes    uint64  `json:"root_disk_used_bytes,omitempty"`
	RootDiskFreeBytes    uint64  `json:"root_disk_free_bytes,omitempty"`
	RootDiskUsedPct      float64 `json:"root_disk_used_pct,omitempty"`
}

// Investigation is the stable JSON contract.
type Investigation struct {
	SchemaVersion    int             `json:"schema_version"`
	ID               string          `json:"id"`
	Host             string          `json:"host"`
	Machine          MachineIdentity `json:"machine"`
	MachineSnapshot  MachineSnapshot `json:"machine_snapshot"`
	Facts            Facts           `json:"facts"`
	Trigger          string          `json:"trigger"`
	Hint             string          `json:"hint,omitempty"`
	StartedAt        time.Time       `json:"started_at"`
	Duration         time.Duration   `json:"duration_ns"`
	Budget           Budget          `json:"budget"`
	Spent            Spend           `json:"spent"`
	Evidence         []Evidence      `json:"evidence"`
	Hypotheses       []Hypothesis    `json:"hypotheses"`
	Path             []Step          `json:"path"`
	NotInvestigated  []Candidate     `json:"not_investigated,omitempty"`
	Limitations      []string        `json:"limitations,omitempty"`
	StopReason       StopReason      `json:"stop_reason"`
	IdentityGaps     int             `json:"identity_gaps,omitempty"`
	Notices          []Notice        `json:"notices,omitempty"`
	ObservedEntities []Entity        `json:"observed_entities,omitempty"`
	Narrative        string          `json:"narrative,omitempty"`
}

// JSON returns the stable JSON encoding.
func (i Investigation) JSON() ([]byte, error) { return json.Marshal(i) }

// NormalizeCandidates sorts candidate order deterministically.
func NormalizeCandidates(c []Candidate) {
	sort.SliceStable(c, func(i, j int) bool {
		if c[i].Forced != c[j].Forced {
			return c[i].Forced
		}
		if c[i].Score != c[j].Score {
			return c[i].Score > c[j].Score
		}
		if c[i].Capability != c[j].Capability {
			return c[i].Capability < c[j].Capability
		}
		return c[i].Scope.ID < c[j].Scope.ID
	})
}

// ParseEntityID validates a canonical entity id.
func ParseEntityID(s string) (EntityKind, error) {
	if s == "machine" {
		return EntityMachine, nil
	}
	p := strings.SplitN(s, ":", 2)
	if len(p) == 2 {
		switch EntityKind(p[0]) {
		case EntityProcess, EntityThread, EntityDevice, EntityMount, EntitySocket:
			return EntityKind(p[0]), nil
		case EntityCgroup:
			return EntityCgroup, nil
		}
	}
	return "", fmt.Errorf("invalid entity id %q", s)
}
