package logs

import (
	"fmt"
	"strings"
	"time"

	"github.com/faizahmd2/diagnos/internal/executor"
)

type KernelCollector struct {
	Executor *executor.NativeExecutor
	Timeout  time.Duration
}

func NewKernelCollector(
	exec *executor.NativeExecutor,
	timeout time.Duration,
) *KernelCollector {
	return &KernelCollector{
		Executor: exec,
		Timeout:  timeout,
	}
}

func (c *KernelCollector) Collect(
	targetHost string,
	since time.Duration,
) JournalResult {
	if since <= 0 {
		return JournalResult{
			Status: StatusFailed,
			Error:  "kernel log collection window must be greater than zero",
		}
	}

	command := fmt.Sprintf(
		"journalctl -k --since '%d seconds ago' --no-pager -o short-iso",
		int(since.Seconds()),
	)

	output, err := c.Executor.Run(
		command,
		c.Timeout,
	)

	if err != nil {
		if strings.Contains(
			strings.ToLower(err.Error()),
			"timeout",
		) {
			return JournalResult{
				Status: StatusTimeout,
				Error:  err.Error(),
			}
		}

		return JournalResult{
			Status: StatusFailed,
			Error:  err.Error(),
		}
	}

	return JournalResult{
		Status: StatusSuccess,
		Output: output,
	}
}

func (c *KernelCollector) CollectEvidence(
	targetHost string,
	since time.Duration,
) ([]Evidence, error) {
	if !c.Executor.Supports("journalctl") {
		return nil, nil
	}
	result := c.Collect(targetHost, since)

	if result.Status != StatusSuccess {
		return nil, fmt.Errorf(
			"kernel log collection %s: %s",
			result.Status,
			result.Error,
		)
	}

	lines := Filter(result.Output)

	if len(lines) == 0 {
		return nil, nil
	}

	drainResult, err := Compress(lines)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to compress kernel logs: %w",
			err,
		)
	}

	return []Evidence{
		{
			Source:   "systemd-journal-kernel",
			LogArea:  "kernel",
			Severity: "warning-or-higher",
			Lines:    lines,
			Drain:    &drainResult,
		},
	}, nil
}
