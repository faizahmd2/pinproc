#!/bin/sh
set -eu

if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload || true
fi

if [ "${1:-}" = "purge" ]; then
    USER_NAME=pinproc
    GROUP_NAME=pinproc
    DATA_DIR=/var/lib/pinproc
    CONFIG_DIR=/etc/pinproc
    created_user=0
    created_group=0
    if [ -e "$DATA_DIR/.pinproc-created-account" ]; then created_user=1; fi
    if [ -e "$DATA_DIR/.pinproc-created-group" ]; then created_group=1; fi
    rm -rf "$CONFIG_DIR" "$DATA_DIR"
    if [ "$created_user" -eq 1 ] && getent passwd "$USER_NAME" >/dev/null 2>&1; then userdel "$USER_NAME" || true; fi
    if [ "$created_group" -eq 1 ] && getent group "$GROUP_NAME" >/dev/null 2>&1; then groupdel "$GROUP_NAME" || true; fi
fi
