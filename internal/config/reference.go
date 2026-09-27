package config

import (
	"fmt"
	"os"
)

const ReferenceYAML = `# pinproc managed configuration
# Production configuration should be changed with:
#   sudo pinproc setup ai
#   sudo pinproc setup callback
#   sudo pinproc setup server

app:
  name: pinproc
  log_level: info

server:
  listen: 127.0.0.1:8080

ai:
  enabled: false
  provider: ""
  config: {}

narrator:
  enabled: true

source:
  read_timeout: 2s

report:
  max_findings: 5

callback:
  enabled: false
  timeout: 5s
`

func WriteReference(path string, force bool) error {
	if _, err := os.Stat(path); err == nil && !force {
		return fmt.Errorf("refusing to overwrite %s; pass --force", path)
	}
	return os.WriteFile(path, []byte(ReferenceYAML), 0600)
}
