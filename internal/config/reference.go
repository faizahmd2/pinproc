package config

import (
	"fmt"
	"os"
)

const ReferenceYAML = `# diagnos configuration
# Every setting is optional. Precedence: CLI flags > DIAGNOS_* env vars > this file > defaults.




ai:
  # provider: anthropic # anthropic | openai | internal
  # model: claude-sonnet-4-6
  # base_url: "" # optional endpoint override
  # API key is read from DIAGNOS_AI_API_KEY; do not commit it here.
  request_limits: { max_lines_context_file: 250 } # Compact shared evidence budget.

output:
  # app-metrics writes a compact direct report. with-ai enables the full
  # existing two-stage AI investigation.
  report_type: with-ai
`

func WriteReference(path string, force bool) error {
	if _, err := os.Stat(path); err == nil && !force {
		return fmt.Errorf("refusing to overwrite %s; pass --force", path)
	}
	return os.WriteFile(path, []byte(ReferenceYAML), 0600)
}
