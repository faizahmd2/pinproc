package system

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/faizahmd2/vm-native-diagnos/internal/executor"
)

type MachineMetadata struct {
	Hostname      string        `json:"hostname"`
	IP            string        `json:"ip"`
	OS            string        `json:"os"`
	OSVersion     string        `json:"os_version"`
	Kernel        string        `json:"kernel"`
	Architecture  string        `json:"architecture"`
	CPUCores      int           `json:"cpu_cores"`
	LoadAverage   LoadAverage   `json:"load_average"`
	Memory        MemorySummary `json:"memory"`
	Disk          DiskSummary   `json:"disk"`
	UptimeSeconds uint64        `json:"uptime_seconds"`
	TopProcesses  []ProcessInfo `json:"top_processes"`
}

type LoadAverage struct {
	OneMinute      float64 `json:"1m"`
	FiveMinutes    float64 `json:"5m"`
	FifteenMinutes float64 `json:"15m"`
}

type MemorySummary struct {
	TotalBytes     uint64 `json:"total_bytes"`
	AvailableBytes uint64 `json:"available_bytes"`
}

type DiskSummary struct {
	TotalBytes     uint64 `json:"total_bytes"`
	AvailableBytes uint64 `json:"available_bytes"`
}

type ProcessInfo struct {
	PID     int     `json:"pid"`
	Command string  `json:"command"`
	CPU     float64 `json:"cpu_percent"`
	Memory  float64 `json:"memory_percent"`
}

type MetadataCollector struct {
	Executor *executor.NativeExecutor
	Timeout  time.Duration
}

func NewMetadataCollector(
	exec *executor.NativeExecutor,
	timeout time.Duration,
) *MetadataCollector {
	return &MetadataCollector{
		Executor: exec,
		Timeout:  timeout,
	}
}

func (c *MetadataCollector) Collect() (MachineMetadata, error) {
	command := `
hostname

printf '\n---IP---\n'
hostname -I | awk '{print $1}'

printf '\n---OS---\n'
. /etc/os-release 2>/dev/null && printf '%s|%s\n' "$ID" "$VERSION_ID"

printf '\n---KERNEL---\n'
uname -r

printf '\n---ARCH---\n'
uname -m

printf '\n---CPU---\n'
nproc

printf '\n---LOAD---\n'
cat /proc/loadavg

printf '\n---MEMORY---\n'
awk '
/^MemTotal:/ {total=$2 * 1024}
/^MemAvailable:/ {available=$2 * 1024}
END {printf "%d|%d\n", total, available}
' /proc/meminfo

printf '\n---DISK---\n'
df -B1 / | awk 'NR==2 {print $2 "|" $4}'

printf '\n---UPTIME---\n'
awk '{print int($1)}' /proc/uptime

printf '\n---PROCESSES---\n'
ps -eo pid=,pcpu=,pmem=,comm= --sort=-pcpu | head -n 10
`

	output, err := c.Executor.Run(
		command,
		c.Timeout,
	)
	if err != nil {
		return MachineMetadata{}, fmt.Errorf(
			"collect machine metadata: %w",
			err,
		)
	}

	return parseMetadata(output)
}

func parseMetadata(output string) (MachineMetadata, error) {
	lines := strings.Split(
		strings.TrimSpace(output),
		"\n",
	)

	var metadata MachineMetadata

	for index := 0; index < len(lines); index++ {
		line := strings.TrimSpace(lines[index])

		switch line {
		case "---IP---":
			metadata.IP = nextValue(lines, index)

		case "---OS---":
			if index+1 >= len(lines) {
				return MachineMetadata{}, fmt.Errorf(
					"OS information missing",
				)
			}

			parts := strings.SplitN(
				strings.TrimSpace(lines[index+1]),
				"|",
				2,
			)

			if len(parts) != 2 {
				return MachineMetadata{}, fmt.Errorf(
					"invalid OS information",
				)
			}

			metadata.OS = parts[0]
			metadata.OSVersion = parts[1]

		case "---KERNEL---":
			metadata.Kernel = nextValue(lines, index)

		case "---ARCH---":
			metadata.Architecture = nextValue(lines, index)

		case "---CPU---":
			value := nextValue(lines, index)

			cpuCores, err := strconv.Atoi(value)
			if err != nil {
				return MachineMetadata{}, fmt.Errorf(
					"invalid CPU core count %q: %w",
					value,
					err,
				)
			}

			metadata.CPUCores = cpuCores

		case "---LOAD---":
			value := nextValue(lines, index)

			parts := strings.Fields(value)
			if len(parts) < 3 {
				return MachineMetadata{}, fmt.Errorf(
					"invalid load average %q",
					value,
				)
			}

			oneMinute, err := strconv.ParseFloat(parts[0], 64)
			if err != nil {
				return MachineMetadata{}, fmt.Errorf(
					"invalid 1-minute load average %q: %w",
					parts[0],
					err,
				)
			}

			fiveMinutes, err := strconv.ParseFloat(parts[1], 64)
			if err != nil {
				return MachineMetadata{}, fmt.Errorf(
					"invalid 5-minute load average %q: %w",
					parts[1],
					err,
				)
			}

			fifteenMinutes, err := strconv.ParseFloat(parts[2], 64)
			if err != nil {
				return MachineMetadata{}, fmt.Errorf(
					"invalid 15-minute load average %q: %w",
					parts[2],
					err,
				)
			}

			metadata.LoadAverage = LoadAverage{
				OneMinute:      oneMinute,
				FiveMinutes:    fiveMinutes,
				FifteenMinutes: fifteenMinutes,
			}

		case "---MEMORY---":
			value := nextValue(lines, index)

			parts := strings.SplitN(value, "|", 2)
			if len(parts) != 2 {
				return MachineMetadata{}, fmt.Errorf(
					"invalid memory information %q",
					value,
				)
			}

			total, err := strconv.ParseUint(parts[0], 10, 64)
			if err != nil {
				return MachineMetadata{}, fmt.Errorf(
					"invalid memory total %q: %w",
					parts[0],
					err,
				)
			}

			available, err := strconv.ParseUint(parts[1], 10, 64)
			if err != nil {
				return MachineMetadata{}, fmt.Errorf(
					"invalid memory available %q: %w",
					parts[1],
					err,
				)
			}

			metadata.Memory = MemorySummary{
				TotalBytes:     total,
				AvailableBytes: available,
			}

		case "---DISK---":
			value := nextValue(lines, index)

			parts := strings.SplitN(value, "|", 2)
			if len(parts) != 2 {
				return MachineMetadata{}, fmt.Errorf(
					"invalid disk information %q",
					value,
				)
			}

			total, err := strconv.ParseUint(parts[0], 10, 64)
			if err != nil {
				return MachineMetadata{}, fmt.Errorf(
					"invalid disk total %q: %w",
					parts[0],
					err,
				)
			}

			available, err := strconv.ParseUint(parts[1], 10, 64)
			if err != nil {
				return MachineMetadata{}, fmt.Errorf(
					"invalid disk available %q: %w",
					parts[1],
					err,
				)
			}

			metadata.Disk = DiskSummary{
				TotalBytes:     total,
				AvailableBytes: available,
			}

		case "---UPTIME---":
			value := nextValue(lines, index)

			uptime, err := strconv.ParseUint(
				value,
				10,
				64,
			)
			if err != nil {
				return MachineMetadata{}, fmt.Errorf(
					"invalid uptime value %q: %w",
					value,
					err,
				)
			}

			metadata.UptimeSeconds = uptime

		case "---PROCESSES---":
			processes, err := parseProcesses(lines, index)
			if err != nil {
				return MachineMetadata{}, err
			}

			metadata.TopProcesses = processes

		default:
			if metadata.Hostname == "" &&
				line != "" &&
				!strings.HasPrefix(line, "---") {
				metadata.Hostname = line
			}
		}
	}

	if metadata.Hostname == "" {
		return MachineMetadata{}, fmt.Errorf(
			"hostname missing from metadata response",
		)
	}

	if metadata.IP == "" {
		return MachineMetadata{}, fmt.Errorf(
			"IP address missing from metadata response",
		)
	}

	return metadata, nil
}

func parseProcesses(
	lines []string,
	index int,
) ([]ProcessInfo, error) {
	var processes []ProcessInfo

	for i := index + 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])

		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "---") {
			break
		}

		fields := strings.Fields(line)
		if len(fields) < 4 {
			return nil, fmt.Errorf(
				"invalid process information %q",
				line,
			)
		}

		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			return nil, fmt.Errorf(
				"invalid process PID %q: %w",
				fields[0],
				err,
			)
		}

		cpu, err := strconv.ParseFloat(fields[1], 64)
		if err != nil {
			return nil, fmt.Errorf(
				"invalid process CPU %q: %w",
				fields[1],
				err,
			)
		}

		memory, err := strconv.ParseFloat(fields[2], 64)
		if err != nil {
			return nil, fmt.Errorf(
				"invalid process memory %q: %w",
				fields[2],
				err,
			)
		}

		processes = append(processes, ProcessInfo{
			PID:     pid,
			CPU:     cpu,
			Memory:  memory,
			Command: strings.Join(fields[3:], " "),
		})
	}

	return processes, nil
}

func nextValue(
	lines []string,
	index int,
) string {
	if index+1 >= len(lines) {
		return ""
	}

	return strings.TrimSpace(lines[index+1])
}
