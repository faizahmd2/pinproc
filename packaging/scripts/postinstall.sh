#!/bin/sh
set -eu

USER_NAME=pinproc
GROUP_NAME=pinproc
DATA_DIR=/var/lib/pinproc
CONFIG_DIR=/etc/pinproc

install -d -m 0750 -o "$USER_NAME" -g "$GROUP_NAME" "$DATA_DIR"
install -d -m 0750 -o root -g "$GROUP_NAME" "$CONFIG_DIR"

if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload || true
    systemctl enable pinproc.service || true
    systemctl restart pinproc.service || true
fi
