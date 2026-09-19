package logs

import (
	"regexp"
	"strings"
)

var interestingLogPattern = regexp.MustCompile(
	`(?i)\b(error|failed|failure|fatal|critical|panic|oom|killed|denied|refused|timeout|timed out|reset|unreachable|corrupt|I/O error|no space|read-only|segfault)\b`,
)

var ignoredLogPatterns = []*regexp.Regexp{
	regexp.MustCompile(`_raw_params=.*journalctl`),
}

func Filter(output string) []string {
	lines := strings.Split(output, "\n")

	var interesting []string

	for _, line := range lines {
		line = strings.TrimSpace(line)

		if line == "" {
			continue
		}

		if isIgnored(line) {
			continue
		}

		if interestingLogPattern.MatchString(line) {
			interesting = append(interesting, line)
		}
	}

	return interesting
}

func isIgnored(line string) bool {
	for _, pattern := range ignoredLogPatterns {
		if pattern.MatchString(line) {
			return true
		}
	}

	return false
}
