package logs

import (
	"fmt"
	"strings"
	"time"

	"github.com/faizahmd2/diagnos/internal/executor"
)

type ApplicationCollector struct {
	Executor   *executor.NativeExecutor
	Timeout    time.Duration
	Discovery  *Discovery
	MaxSources int
}

func NewApplicationCollector(
	exec *executor.NativeExecutor,
	timeout time.Duration,
	maxSources int,
) *ApplicationCollector {
	return &ApplicationCollector{
		Executor:   exec,
		Timeout:    timeout,
		Discovery:  NewDiscovery(exec, timeout),
		MaxSources: maxSources,
	}
}

func (c *ApplicationCollector) CollectEvidence(
	targetHost string,
	configured []LogSource,
	since time.Duration,
) ([]Evidence, error) {
	sources, err := c.Discovery.Discover(
		targetHost,
		configured,
	)
	if err != nil {
		return nil, err
	}

	if c.MaxSources > 0 && len(sources) > c.MaxSources {
		sources = sources[:c.MaxSources]
	}

	var evidence []Evidence

	for _, source := range sources {
		command := fmt.Sprintf(
			"if [ -f '%s' ]; then tail -n 500 '%s'; fi",
			escapeShell(source.Path),
			escapeShell(source.Path),
		)

		output, err := c.Executor.Run(
			command,
			c.Timeout,
		)

		if err != nil {
			evidence = append(evidence, Evidence{
				Source:     source.Path,
				SourceType: source.SourceType,
				LogArea:    logAreaForSource(source),
				Severity:   "candidate-errors",

				PID:         source.PID,
				PPID:        source.PPID,
				Process:     source.Process,
				Executable:  source.Executable,
				CommandLine: source.CommandLine,
				WorkingDir:  source.WorkingDir,
				FD:          source.FD,

				Status: "unavailable",
				Error:  err.Error(),
			})

			continue
		}

		lines := Filter(output)

		if len(lines) == 0 {
			continue
		}

		drainResult, err := Compress(lines)
		if err != nil {
			return nil, err
		}

		evidence = append(evidence, Evidence{
			Source:     source.Path,
			SourceType: source.SourceType,

			LogArea:  logAreaForSource(source),
			Severity: "candidate-errors",

			PID:         source.PID,
			PPID:        source.PPID,
			Process:     source.Process,
			Executable:  source.Executable,
			CommandLine: source.CommandLine,
			WorkingDir:  source.WorkingDir,
			FD:          source.FD,

			Lines: lines,
			Drain: &drainResult,
		})
	}

	return evidence, nil
}

func escapeShell(value string) string {
	return strings.ReplaceAll(value, "'", "'\\''")
}

func logAreaForSource(source LogSource) string {
	switch source.SourceType {
	case "pm2":
		return "application"
	case "process-fd":
		return "application"
	case "configured":
		return "application"
	default:
		return "application"
	}
}
