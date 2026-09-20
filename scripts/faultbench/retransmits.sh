#!/usr/bin/env bash
set -euo pipefail
command -v tc >/dev/null || { echo "iproute2/tc is required"; exit 1; }
dev=${DIAGNOS_FAULTBENCH_NETDEV:-}
[[ -n "$dev" ]] || { echo "Set DIAGNOS_FAULTBENCH_NETDEV to a disposable interface"; exit 1; }
echo "Run: pinproc investigate --dimension network --budget normal"
cleanup() { sudo tc qdisc del dev "$dev" root netem 2>/dev/null || true; }
trap cleanup EXIT
sudo tc qdisc replace dev "$dev" root netem loss 5%
sleep 15
