package logs

import (
	"fmt"
	"strings"
	"time"

	"github.com/faizahmd2/diagnos/internal/executor"
)

type LogSource struct {
	Name        string
	Path        string
	Service     string
	SourceType  string
	PID         string
	PPID        string
	Process     string
	Executable  string
	CommandLine string
	WorkingDir  string
	FD          string
}

type Discovery struct {
	Executor *executor.NativeExecutor
	Timeout  time.Duration
}

func NewDiscovery(
	exec *executor.NativeExecutor,
	timeout time.Duration,
) *Discovery {
	return &Discovery{
		Executor: exec,
		Timeout:  timeout,
	}
}

func (d *Discovery) Discover(
	targetHost string,
	configured []LogSource,
) ([]LogSource, error) {
	sources := append([]LogSource{}, configured...)

	discovered, err := d.discoverProcessLogs(targetHost)
	if err != nil {
		return nil, err
	}

	sources = append(sources, discovered...)

	sources = deduplicateSources(sources)

	return sources, nil
}

func (d *Discovery) discoverProcessLogs(
	targetHost string,
) ([]LogSource, error) {
	command := `
		for pid in $(ps -eo pid=); do
			process=$(cat /proc/$pid/comm 2>/dev/null || true)
			ppid=$(awk '/^PPid:/ {print $2}' /proc/$pid/status 2>/dev/null || true)
			executable=$(readlink /proc/$pid/exe 2>/dev/null || true)
			command_line=$(tr '\0' ' ' < /proc/$pid/cmdline 2>/dev/null || true)
			working_dir=$(readlink /proc/$pid/cwd 2>/dev/null || true)

			for fd_path in /proc/$pid/fd/*; do
				fd=$(basename "$fd_path")
				path=$(readlink "$fd_path" 2>/dev/null || true)

				case "$path" in
					/*)
						case "$path" in
							/dev/*|/proc/*|/sys/*|/tmp/*)
								continue
								;;
						esac

						if [ -f "$path" ]; then
							printf '%s|%s|%s|%s|%s|%s|%s|%s|%s\n' \
								"$pid" \
								"$ppid" \
								"$process" \
								"$executable" \
								"$command_line" \
								"$working_dir" \
								"$fd" \
								"$path" \
								"process-fd"
						fi
						;;
				esac
			done
		done
		`

	output, err := d.Executor.Run(
		command,
		d.Timeout,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to discover process logs: %w",
			err,
		)
	}

	var sources []LogSource

	for _, line := range strings.Split(output, "\n") {
		fields := strings.SplitN(line, "|", 9)

		if len(fields) != 9 {
			continue
		}

		sources = append(sources, LogSource{
			PID:         fields[0],
			PPID:        fields[1],
			Process:     fields[2],
			Executable:  fields[3],
			CommandLine: fields[4],
			WorkingDir:  fields[5],
			FD:          fields[6],
			Path:        fields[7],
			SourceType:  fields[8],
			Name:        fmt.Sprintf("%s[%s]:fd%s", fields[2], fields[0], fields[6]),
		})
	}

	return sources, nil
}

func deduplicateSources(sources []LogSource) []LogSource {
	seen := make(map[string]struct{})
	result := make([]LogSource, 0, len(sources))

	for _, source := range sources {
		if source.Path == "" {
			continue
		}

		if _, exists := seen[source.Path]; exists {
			continue
		}

		seen[source.Path] = struct{}{}
		result = append(result, source)
	}

	return result
}
