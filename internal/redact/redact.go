package redact

import (
	"net/url"
	"regexp"
	"strings"
)

var secretRE = regexp.MustCompile("(?i)(authorization\\s*[:=]\\s*bearer\\s+[^\\s]+|api[_-]?key\\s*[:=]\\s*[^\\s]+|token\\s*[:=]\\s*[^\\s]+)")

// Text removes common credential values from captured text.
func Text(s string) string { return secretRE.ReplaceAllString(s, "[REDACTED]") }

// Path removes URL query strings.
func Path(s string) string {
	if u, e := url.Parse(s); e == nil && u.RawQuery != "" {
		u.RawQuery = ""
		return u.String()
	}
	return strings.TrimSpace(s)
}
