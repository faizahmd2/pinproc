#!/usr/bin/env bash
set -euo pipefail
[[ -w /sys/fs/cgroup ]] || { echo "cgroup v2 writable hierarchy is required"; exit 1; }
cg=/sys/fs/cgroup/pinproc-faultbench
sudo mkdir -p "$cg"
cleanup() { echo max 100000 | sudo tee "$cg/cpu.max" >/dev/null 2>&1 || true; sudo rmdir "$cg" 2>/dev/null || true; kill "${pid:-0}" 2>/dev/null || true; }
trap cleanup EXIT
echo "10000 100000" | sudo tee "$cg/cpu.max" >/dev/null
( while :; do :; done ) &
pid=$!
echo "$pid" | sudo tee "$cg/cgroup.procs" >/dev/null
echo "Run: pinproc investigate --dimension cpu --budget normal"
sleep 20
