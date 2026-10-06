package config

import (
	"fmt"
	"os"
)

const ReferenceYAML = `# pinproc managed configuration
# pinproc watches the host and captures incidents by itself (no socket, no API).

version: 1

app:
  name: pinproc
  log_level: info

narrator:
  enabled: true

source:
  read_timeout: 2s

report:
  max_findings: 5

# Optional: POST each captured report (as plain text) to one URL. This is the only
# traffic pinproc ever sends. Leave disabled for a fully offline host.
callback:
  enabled: false
  url: ""
  timeout: 5s

# Optional: tune when an incident is captured. Omit to use conservative defaults.
# monitor:
#   cpu: { arm_util: 80, cap_util: 90, sustain: 60s }
#   mem: { arm_util: 85, cap_util: 95, sustain: 60s }
#   io:  { arm_util: 80, cap_util: 95, sustain: 30s }
`

func WriteReference(path string, force bool) error {
	if _, err := os.Stat(path); err == nil && !force {
		return fmt.Errorf("refusing to overwrite %s; pass --force", path)
	}
	return os.WriteFile(path, []byte(ReferenceYAML), 0600)
}
