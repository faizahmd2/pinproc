#!/bin/sh
set -eu

if [ "${1:-}" = "purge" ] || [ "${1:-}" = "remove" ]; then
    rmdir /usr/libexec/pinproc/providers /usr/libexec/pinproc /usr/share/pinproc/providers /usr/share/pinproc 2>/dev/null || true
fi
