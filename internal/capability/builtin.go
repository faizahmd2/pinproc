package capability

import (
	"fmt"

	"github.com/faizahmd2/vm-native-diagnos/internal/capability/cgroup"
	"github.com/faizahmd2/vm-native-diagnos/internal/capability/machine"
	"github.com/faizahmd2/vm-native-diagnos/internal/capability/process"
	"github.com/faizahmd2/vm-native-diagnos/internal/capability/thread"
	"github.com/faizahmd2/vm-native-diagnos/internal/contract"
)

// BuildBuiltin creates the V2 capability graph.
func BuildBuiltin() (*Registry, error) {
	r := NewRegistry()
	caps := []Capability{
		machine.CPU(), machine.Memory(), machine.IO(), machine.Network(), machine.Limits(), machine.Processes(),
		process.CPU(), process.Memory(), process.IO(), process.Files(), process.Sockets(), process.MemoryMaps(), process.Limits(), process.Tree(),
		thread.CPU(), thread.Scheduler(), thread.Wait(),
		cgroup.CPU(), cgroup.Memory(), cgroup.IO(),
	}
	for _, c := range caps {
		if err := r.Register(c); err != nil {
			return nil, err
		}
	}
	edges := map[string][]string{
		"machine.cpu":       {"machine.processes", "machine.io"},
		"machine.memory":    {"machine.processes"},
		"machine.io":        {"machine.processes"},
		"machine.network":   {"machine.processes"},
		"machine.limits":    {"machine.processes"},
		"machine.processes": {"process.cpu", "process.memory", "process.io"},
		"process.cpu":       {"thread.cpu", "thread.scheduler", "thread.wait", "cgroup.cpu"},
		"process.memory":    {"process.memory_maps", "cgroup.memory"},
		"process.io":        {"process.files", "process.limits", "cgroup.io"},
		"process.files":     {"process.sockets"},
	}
	for id, leadsTo := range edges {
		c, ok := r.Get(id)
		if !ok {
			return nil, fmt.Errorf("edge references unknown capability %q", id)
		}
		c.LeadsTo = append([]string(nil), leadsTo...)
		r.items[id] = c
	}
	if err := r.ValidateGraph(); err != nil {
		return nil, fmt.Errorf("validate capability graph: %w", err)
	}
	return r, nil
}

// CapabilityDimensionNames returns supported dimensions.
func CapabilityDimensionNames() []contract.Dimension {
	return []contract.Dimension{
		contract.DimensionCPU,
		contract.DimensionMemory,
		contract.DimensionIO,
		contract.DimensionNetwork,
		contract.DimensionScheduling,
		contract.DimensionFilesystem,
		contract.DimensionLimits,
	}
}
