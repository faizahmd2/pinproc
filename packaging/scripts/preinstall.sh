#!/bin/sh
set -eu

USER_NAME=pinproc
GROUP_NAME=pinproc
DATA_DIR=/var/lib/pinproc

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
