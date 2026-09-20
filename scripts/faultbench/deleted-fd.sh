#!/usr/bin/env bash
set -euo pipefail
tmp=${DIAGNOS_FAULTBENCH_DIR:-/tmp/pinproc-faultbench}
mkdir -p "$tmp"
echo "Run: pinproc investigate --dimension filesystem --budget normal"
python3 - "$tmp" <<'PY'
import os, sys, time
path=os.path.join(sys.argv[1], "deleted-open")
fd=os.open(path, os.O_CREAT|os.O_RDWR, 0o600)
os.ftruncate(fd, 600*1024*1024)
os.unlink(path)
try:
    time.sleep(20)
finally:
    os.close(fd)
PY
