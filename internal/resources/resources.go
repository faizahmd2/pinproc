package resources

import "embed"

// Files contains install-time templates.
//
//go:embed systemd/diagnos-check.service.tmpl systemd/diagnos-check.timer.tmpl sudoers.tmpl
var Files embed.FS
