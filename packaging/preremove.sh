#!/bin/sh
set -eu

case "${1:-remove}" in
  remove|upgrade|0|1)
    if [ -d /run/systemd/system ]; then
      systemctl disable --now pinproc.service || true
    fi
    ;;
esac

exit 0
