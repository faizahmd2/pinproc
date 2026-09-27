#!/usr/bin/env bash
set -Eeuo pipefail

REPO="${PINPROC_REPO:-faizahmd2/pinproc}"
VERSION="${PINPROC_VERSION:-}"
TMP_DIR="$(mktemp -d)"
cleanup() { rm -rf "$TMP_DIR"; }
trap cleanup EXIT

case "$(uname -m)" in
    x86_64) ARCH=amd64 ;;
    aarch64|arm64) ARCH=arm64 ;;
    *) echo "Unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

if [[ -z "$VERSION" ]]; then
    echo "Set PINPROC_VERSION to a release version, for example PINPROC_VERSION=0.1.0." >&2
    exit 2
fi

URL="https://github.com/$REPO/releases/download/v${VERSION}/pinproc_${VERSION}_${ARCH}.deb"
DEB="$TMP_DIR/pinproc.deb"
curl -fsSL "$URL" -o "$DEB"
sudo apt install -y "$DEB"
