#!/usr/bin/env bash
set -euo pipefail
command -v fio >/dev/null || { echo "fio is required"; exit 1; }
dir=${DIAGNOS_FAULTBENCH_DIR:-/tmp/diagnos-faultbench}
mkdir -p "$dir"
trap 'rm -f "$dir"/diagnos-fio-* 2>/dev/null || true' EXIT
echo "Run: diagnos investigate --dimension io --budget normal"
fio --name=diagnos-fio --filename="$dir/diagnos-fio-test" --size=512m --rw=randrw --rwmixread=50 --bs=4k --iodepth=32 --runtime=20s --time_based --direct=1
