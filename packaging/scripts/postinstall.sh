#!/bin/sh
set -eu

USER_NAME=pinproc
GROUP_NAME=pinproc
DATA_DIR=/var/lib/pinproc
CONFIG_DIR=/etc/pinproc
CONFIG_FILE=$CONFIG_DIR/config.yaml
DEFAULT_CONFIG=/usr/share/pinproc/default-config.yaml

created_account=0
created_group=0

if ! getent group "$GROUP_NAME" >/dev/null 2>&1; then
    groupadd --system "$GROUP_NAME"
    created_group=1
fi

if ! getent passwd "$USER_NAME" >/dev/null 2>&1; then
    useradd --system --gid "$GROUP_NAME" --home-dir "$DATA_DIR" --no-create-home --shell /usr/sbin/nologin "$USER_NAME"
    created_account=1
fi

install -d -m 0750 -o "$USER_NAME" -g "$GROUP_NAME" "$DATA_DIR"
install -d -m 0750 -o root -g "$GROUP_NAME" "$CONFIG_DIR"

if [ ! -e "$CONFIG_FILE" ]; then
    install -m 0640 -o root -g "$GROUP_NAME" "$DEFAULT_CONFIG" "$CONFIG_FILE"
fi

if [ "$created_account" -eq 1 ]; then
    touch "$DATA_DIR/.pinproc-created-account"
    chown "$USER_NAME:$GROUP_NAME" "$DATA_DIR/.pinproc-created-account"
    chmod 0600 "$DATA_DIR/.pinproc-created-account"
fi

if [ "$created_group" -eq 1 ]; then
    touch "$DATA_DIR/.pinproc-created-group"
    chown "$USER_NAME:$GROUP_NAME" "$DATA_DIR/.pinproc-created-group"
    chmod 0600 "$DATA_DIR/.pinproc-created-group"
fi

if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload || true
    systemctl enable pinproc.service || true
    systemctl restart pinproc.service || true
fi
