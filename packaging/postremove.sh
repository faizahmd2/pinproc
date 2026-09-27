#!/bin/sh
set -eu

case "${1:-remove}" in
  purge)
    if [ -d /run/systemd/system ]; then
      systemctl daemon-reload || true
    fi

    rm -rf /var/lib/pinproc /etc/pinproc

    if getent passwd pinproc >/dev/null 2>&1; then
      shell="$(getent passwd pinproc | cut -d: -f7)"
      home="$(getent passwd pinproc | cut -d: -f6)"
      if [ "$shell" = "/usr/sbin/nologin" ] && [ "$home" = "/nonexistent" ]; then
        userdel pinproc || true
      fi
    fi

    if getent group pinproc >/dev/null 2>&1; then
      groupdel pinproc || true
    fi
    ;;
esac

exit 0
