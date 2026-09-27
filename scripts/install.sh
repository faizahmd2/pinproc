#!/bin/sh
set -eu

REPO="faizahmd2/pinproc"
BASE="https://github.com/${REPO}/releases"
VERSION="${PINPROC_VERSION:-latest}"

case "$(uname -m)" in
  x86_64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *)
    echo "Unsupported architecture: $(uname -m)" >&2
    exit 1
    ;;
esac

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT INT TERM

if [ "$VERSION" = "latest" ]; then
  asset_url="${BASE}/latest/download/pinproc_${ARCH}.deb"
  sums_url="${BASE}/latest/download/checksums.txt"
else
  asset_url="${BASE}/download/${VERSION}/pinproc_${ARCH}.deb"
  sums_url="${BASE}/download/${VERSION}/checksums.txt"
fi

echo "Downloading pinproc for ${ARCH}..."
curl -fsSL "$asset_url" -o "$tmp/pinproc_${ARCH}.deb"
curl -fsSL "$sums_url" -o "$tmp/checksums.txt"

expected="$(awk -v f="pinproc_${ARCH}.deb" '$2 == f {print $1}' "$tmp/checksums.txt")"
if [ -z "$expected" ]; then
  echo "Checksum entry for pinproc_${ARCH}.deb was not found." >&2
  exit 1
fi

actual="$(sha256sum "$tmp/pinproc_${ARCH}.deb" | awk '{print $1}')"
if [ "$actual" != "$expected" ]; then
  echo "Checksum verification failed." >&2
  exit 1
fi

echo "Checksum verified. Installing..."
exec sudo apt-get install -y "$tmp/pinproc_${ARCH}.deb"
