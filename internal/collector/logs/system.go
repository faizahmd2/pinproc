package logs

import (
	"fmt"
	"strings"
	"time"

	"github.com/faizahmd2/diagnos/internal/executor"
)

type SystemCollector struct {
	Executor *executor.NativeExecutor
	Timeout  time.Duration
}

func NewSystemCollector(
	exec *executor.NativeExecutor,
	timeout time.Duration,
) *SystemCollector {
	return &SystemCollector{
		Executor: exec,
		Timeout:  timeout,
	}
}

func (c *SystemCollector) CollectEvidence(
	targetHost string,
	since time.Duration,
) ([]Evidence, error) {
	// System service and unit logs are meaningful only when both commands are
	// available. Other collectors continue normally on non-systemd hosts.
	if !c.Executor.Supports("systemctl") || !c.Executor.Supports("journalctl") {
		return nil, nil
	}
	command := "systemctl --failed --no-legend"

	output, err := c.Executor.Run(
		command,
		c.Timeout,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to collect system service state: %w",
			err,
		)
	}

	services := parseFailedServices(output)

	if len(services) == 0 {
		return nil, nil
	}

	var evidence []Evidence

	for _, service := range services {
		serviceCommand := fmt.Sprintf(
			"journalctl -u '%s' --since '%d seconds ago' --no-pager -o short-iso",
			service,
			int(since.Seconds()),
		)

		serviceOutput, err := c.Executor.Run(
			serviceCommand,
			c.Timeout,
		)
		if err != nil {
			evidence = append(evidence, Evidence{
				Source:   "systemd-service:" + service,
				LogArea:  "service",
				Severity: "unknown",
				Status:   "unavailable",
				Error:    err.Error(),
			})

			continue
		}

		lines := Filter(serviceOutput)

		if len(lines) == 0 {
			continue
		}

		drainResult, err := Compress(lines)
		if err != nil {
			return nil, fmt.Errorf(
				"failed to compress logs for service %s: %w",
				service,
				err,
			)
		}

		evidence = append(evidence, Evidence{
			Source:   "systemd-service:" + service,
			LogArea:  "service",
			Severity: "failed-service",
			Lines:    lines,
			Drain:    &drainResult,
		})
	}

	return evidence, nil
}

func parseFailedServices(output string) []string {
	var services []string

	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)

		if len(fields) == 0 {
			continue
		}

		service := fields[0]

		if strings.HasSuffix(service, ".service") {
			services = append(services, service)
		}
	}

	return services
}
