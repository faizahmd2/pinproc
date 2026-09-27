#!/bin/sh
set -eu

case "${1:-}" in
    remove|upgrade)
        if command -v systemctl >/dev/null 2>&1; then
            systemctl stop pinproc.service || true
            if [ "${1:-}" = "remove" ]; then
                systemctl disable pinproc.service || true
            fi
        fi
        ;;
esac
