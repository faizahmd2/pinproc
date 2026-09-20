#!/usr/bin/env bash
set -euo pipefail
target=${DIAGNOS_FAULTBENCH_TARGET:-}
[[ -n "$target" ]] || { echo "Set DIAGNOS_FAULTBENCH_TARGET to a disposable filesystem path"; exit 1; }
mkdir -p "$target"
file="$target/.pinproc-fault-fill"
free=$(df -Pk "$target" | awk 'NR==2 {print $4}')
reserve=$((free / 20))
fill=$((free - reserve))
(( fill > 0 )) || { echo "Not enough free space to run disk-full faultbench"; exit 1; }
echo "Run: pinproc investigate --dimension filesystem --budget normal"
fallocate -l "${fill}K" "$file"
trap 'rm -f "$file"' EXIT
sleep 10
