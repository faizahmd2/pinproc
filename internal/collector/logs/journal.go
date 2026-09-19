package logs

import (
	"fmt"
	"strings"
	"time"

	"github.com/faizahmd2/diagnos/internal/executor"
)

type JournalStatus string

const (
	StatusSuccess JournalStatus = "success"
	StatusFailed  JournalStatus = "failed"
	StatusTimeout JournalStatus = "timeout"
)

type JournalResult struct {
	Status JournalStatus
	Output string
	Error  string
}

type JournalCollector struct {
	Executor *executor.NativeExecutor
	Timeout  time.Duration
}

func NewJournalCollector(
	exec *executor.NativeExecutor,
	timeout time.Duration,
) *JournalCollector {
	return &JournalCollector{
		Executor: exec,
		Timeout:  timeout,
	}
}

func (c *JournalCollector) Collect(
	targetHost string,
	since time.Duration,
) JournalResult {
	if since <= 0 {
		return JournalResult{
			Status: StatusFailed,
			Error:  "journal collection window must be greater than zero",
		}
	}

	command := fmt.Sprintf(
		"journalctl --since '%d seconds ago' --no-pager -o short-iso",
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

func (c *JournalCollector) CollectEvidence(
	targetHost string,
	since time.Duration,
) ([]Evidence, error) {
	if !c.Executor.Supports("journalctl") {
		return nil, nil
	}
	result := c.Collect(targetHost, since)

	if result.Status != StatusSuccess {
		return nil, fmt.Errorf(
			"journal collection %s: %s",
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
		return nil, fmt.Errorf("failed to compress journal logs: %w", err)
	}

	return []Evidence{
		{
			Source:   "systemd-journal",
			LogArea:  "system",
			Severity: "warning-or-higher",
			Lines:    lines,
			Drain:    &drainResult,
		},
	}, nil
}
