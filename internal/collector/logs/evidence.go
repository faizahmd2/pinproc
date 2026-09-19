package logs

import (
	"fmt"
	"strings"
)

type Evidence struct {
	Source     string
	SourceType string

	LogArea  string
	Severity string
	Status   string
	Error    string

	PID         string
	PPID        string
	Process     string
	Executable  string
	CommandLine string
	WorkingDir  string
	FD          string

	Lines []string
	Drain *DrainResult
}

type ProcessContext struct {
	PID         string
	PPID        string
	Process     string
	Executable  string
	CommandLine string
	WorkingDir  string
	FD          string
}

func Render(evidence []Evidence) string {
	var b strings.Builder

	b.WriteString("=== LOG EVIDENCE ===\n\n")

	for i, item := range evidence {
		fmt.Fprintf(&b, "[log-%03d]\n", i+1)
		fmt.Fprintf(&b, "source: %s\n", item.Source)
		fmt.Fprintf(&b, "log_area: %s\n", item.LogArea)

		if item.Severity != "" {
			fmt.Fprintf(&b, "severity: %s\n", item.Severity)
		}

		if item.Status != "" {
			fmt.Fprintf(&b, "status: %s\n", item.Status)
		}

		if item.Error != "" {
			fmt.Fprintf(&b, "error: %s\n", item.Error)
		}

		if item.SourceType != "" {
			fmt.Fprintf(&b, "source_type: %s\n", item.SourceType)
		}
		if item.PID != "" {
			fmt.Fprintf(&b, "pid: %s\n", item.PID)
		}
		if item.Process != "" {
			fmt.Fprintf(&b, "process: %s\n", item.Process)
		}
		if item.Executable != "" {
			fmt.Fprintf(&b, "executable: %s\n", item.Executable)
		}
		if item.CommandLine != "" {
			fmt.Fprintf(&b, "command_line: %s\n", item.CommandLine)
		}
		if item.WorkingDir != "" {
			fmt.Fprintf(&b, "working_dir: %s\n", item.WorkingDir)
		}
		if item.FD != "" {
			fmt.Fprintf(&b, "fd: %s\n", item.FD)
		}

		b.WriteString("\n")

		if item.Drain != nil && item.Drain.Used {
			for _, template := range item.Drain.Templates {
				fmt.Fprintf(&b, "template: %s\n", template.Template)
				fmt.Fprintf(&b, "count: %d\n", template.Count)
			}
		} else {
			b.WriteString("raw:\n")

			for _, line := range item.Lines {
				fmt.Fprintf(&b, "  %s\n", line)
			}
		}

		b.WriteString("\n---\n\n")
	}

	return strings.TrimSpace(b.String()) + "\n"
}
