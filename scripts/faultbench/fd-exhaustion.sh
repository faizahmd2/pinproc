#!/usr/bin/env bash
set -euo pipefail
tmp=${DIAGNOS_FAULTBENCH_DIR:-/tmp/diagnos-faultbench}
mkdir -p "$tmp"
echo "Run: diagnos investigate --dimension limits --budget normal"
python3 - "$tmp" <<'PY'
import os, sys, time
d=sys.argv[1]
fds=[]
for i in range(1024):
    try:
        fds.append(open(os.path.join(d, f"fd-{i}"), "w"))
    except OSError:
        break
time.sleep(15)
for f in fds:
    f.close()
PY
