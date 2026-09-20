#!/usr/bin/env bash
set -euo pipefail
command -v stress-ng >/dev/null || { echo "stress-ng is required"; exit 1; }
trap 'jobs -pr | xargs -r kill 2>/dev/null || true' EXIT
echo "Run: diagnos investigate --dimension memory --budget fast"
stress-ng --vm 1 --vm-bytes 75% --timeout 20s &
wait
