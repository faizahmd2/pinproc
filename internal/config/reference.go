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

# Optional: send each captured report (as plain text, in the request body) to one
# URL. This is the only traffic pinproc ever sends. Leave disabled for a fully
# offline host. method is POST (default) or GET — the report is the body either way,
# so POST is recommended; use GET only for an endpoint that requires it.
# Note: the callback fires when the daemon CAPTURES an incident, not on every manual
# "pinproc report". To send a manual run on demand, use: pinproc report --callback
callback:
  enabled: false
  url: ""
  method: POST
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
