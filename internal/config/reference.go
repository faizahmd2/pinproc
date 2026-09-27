package config

import (
	"fmt"
	"os"
)

const ReferenceYAML = `# pinproc managed configuration
# Use "sudo pinproc setup ..." to change the installed configuration.

version: 1

app:
  name: pinproc
  log_level: info

server:
  listen: 127.0.0.1:8080
  api_key: ""

ai:
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
  url: ""
  timeout: 5s
`

func WriteReference(path string, force bool) error {
	if _, err := os.Stat(path); err == nil && !force { return fmt.Errorf("refusing to overwrite %s; pass --force", path) }
	return os.WriteFile(path, []byte(ReferenceYAML), 0600)
}