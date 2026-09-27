#!/bin/sh
set -eu

if command -v systemd-sysusers >/dev/null 2>&1; then
  systemd-sysusers /usr/lib/sysusers.d/pinproc.conf
fi

if getent group pinproc >/dev/null 2>&1; then
  chown root:pinproc /etc/pinproc/config.yaml
fi
chmod 0640 /etc/pinproc/config.yaml

if [ -d /run/systemd/system ]; then
  systemctl daemon-reload || true
  systemctl enable pinproc.service || true
  systemctl restart pinproc.service || true
fi

exit 0
