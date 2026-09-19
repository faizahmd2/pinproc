package config

import (
	"fmt"
	"os"
)

const ReferenceYAML = `# diagnos configuration
# Every setting is optional. Precedence: CLI flags > DIAGNOS_* env vars > this file > defaults.

# ssh reaches the target with native SSH; no Ansible or Python is required.
ssh:
  # user: ubuntu
  # port: 22
  # Private key path. This is NOT the .pub file in authorized_keys.
  # key_path: ~/.ssh/id_ed25519
  # host_key_policy: prompt # prompt | strict | accept-new | insecure
  # known_hosts: ~/.ssh/known_hosts
  # jump_hosts: [bastion.internal:22]
  # connect_timeout: 10s
  # command_timeout: 30s
  # max_parallel: 4 # capped at 8 because sshd MaxSessions commonly defaults to 10
  # max_output_bytes: 1048576

# Existing target aliases remain supported. You may also pass a host directly to ` + "`diagnos debug <host>`" + `.
targets: {}


ai:
  # provider: anthropic # anthropic | openai | internal
  # model: claude-sonnet-4-6
  # base_url: "" # optional endpoint override
  # API key is read from DIAGNOS_AI_API_KEY; do not commit it here.
  request_limits: { max_lines_context_file: 250 } # Compact shared evidence budget.

output:
  # app-metrics writes a compact direct report. with-ai enables the full
  # existing two-stage AI investigation.
  report_type: app-metrics
`

func WriteReference(path string, force bool) error {
	if _, err := os.Stat(path); err == nil && !force {
		return fmt.Errorf("refusing to overwrite %s; pass --force", path)
	}
	return os.WriteFile(path, []byte(ReferenceYAML), 0600)
}
