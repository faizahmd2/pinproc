package config

import (
	"fmt"
	"os"
)

const ReferenceYAML = `# vm-native-diagnos configuration
# The agent is local-only and reads the machine's OS directly.

server:
  listen: 127.0.0.1:8080

engine:
  budget: normal
  sample_window: 1s
  parallel_width: 3

decision:
  provider: jev
  base_url: https://api.typesafe.ai
  model: jev-latest
  timeout: 10s

narrator:
  enabled: true

agent:
  report_dir: ~/diagnos/reports

output:
  directory: ~/diagnos/reports
`

func WriteReference(path string, force bool) error {
	if _, err := os.Stat(path); err == nil && !force {
		return fmt.Errorf("refusing to overwrite %s; pass --force", path)
	}
	return os.WriteFile(path, []byte(ReferenceYAML), 0600)
}
