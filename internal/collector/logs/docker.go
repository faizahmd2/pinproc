package logs

import (
	"fmt"
	"strings"
	"time"

	"github.com/faizahmd2/vm-native-diagnos/internal/executor"
)

type DockerCollector struct {
	Executor *executor.NativeExecutor
	Timeout  time.Duration
}

func NewDockerCollector(
	exec *executor.NativeExecutor,
	timeout time.Duration,
) *DockerCollector {
	return &DockerCollector{
		Executor: exec,
		Timeout:  timeout,
	}
}

func (c *DockerCollector) CollectEvidence(
	targetHost string,
) ([]Evidence, error) {
	available, err := c.isAvailable(targetHost)
	if err != nil {
		return nil, err
	}

	if !available {
		return nil, nil
	}

	containers, err := c.discoverContainers(targetHost)
	if err != nil {
		return nil, nil
	}

	var evidence []Evidence

	for _, container := range containers {
		command := fmt.Sprintf(
			"docker logs --tail 500 '%s' 2>&1",
			escapeShell(container.ID),
		)

		output, err := c.Executor.Run(
			command,
			c.Timeout,
		)

		if err != nil {
			evidence = append(evidence, Evidence{
				Source:     container.Name,
				SourceType: "docker",
				LogArea:    "application",
				Severity:   "candidate-errors",
				Status:     "unavailable",
				Error:      err.Error(),
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
			Source:     container.Name,
			SourceType: "docker",
			LogArea:    "application",
			Severity:   "candidate-errors",
			Lines:      lines,
			Drain:      &drainResult,
		})
	}

	return evidence, nil
}

type dockerContainer struct {
	ID   string
	Name string
}

func (c *DockerCollector) discoverContainers(
	targetHost string,
) ([]dockerContainer, error) {
	output, err := c.Executor.Run(
		"docker ps --format '{{.ID}}|{{.Names}}'",
		c.Timeout,
	)
	if err != nil {
		return nil, err
	}

	var containers []dockerContainer

	for _, line := range strings.Split(output, "\n") {
		fields := strings.SplitN(strings.TrimSpace(line), "|", 2)

		if len(fields) != 2 || fields[0] == "" || fields[1] == "" {
			continue
		}

		containers = append(containers, dockerContainer{
			ID:   fields[0],
			Name: fields[1],
		})
	}

	return containers, nil
}

func (c *DockerCollector) isAvailable(
	targetHost string,
) (bool, error) {
	_, err := c.Executor.Run(
		"command -v docker >/dev/null 2>&1",
		c.Timeout,
	)

	if err != nil {
		return false, nil
	}

	return true, nil
}
