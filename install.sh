#!/bin/sh
set -eu

REPO="mpm/projectsetup"
INSTALL_DIR="${PROJECTSETUP_INSTALL_DIR:-$HOME/.local/bin}"

case "$(uname -s)" in
    Linux*) OS=linux ;;
    Darwin*) OS=darwin ;;
    *) echo "Unsupported operating system: $(uname -s)" >&2; exit 1 ;;
esac

case "$(uname -m)" in
    x86_64) ARCH=amd64 ;;
    aarch64|arm64) ARCH=arm64 ;;
    *) echo "Unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

VERSION="${1:-}"
if [ -z "$VERSION" ]; then
    echo "Fetching latest projectsetup release..."
    VERSION=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" |
        grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/')
    if [ -z "$VERSION" ]; then
        echo "Could not determine the latest projectsetup release" >&2
        exit 1
    fi
fi

ARCHIVE="projectsetup_${VERSION}_${OS}_${ARCH}.tar.gz"
BASE_URL="https://github.com/${REPO}/releases/download/${VERSION}"
TEMP_DIR=$(mktemp -d)
trap 'rm -rf "$TEMP_DIR"' EXIT

echo "Downloading projectsetup ${VERSION} for ${OS}/${ARCH}..."
curl -fsSL "${BASE_URL}/${ARCHIVE}" -o "${TEMP_DIR}/${ARCHIVE}"
curl -fsSL "${BASE_URL}/checksums.txt" -o "${TEMP_DIR}/checksums.txt"

cd "$TEMP_DIR"
if command -v sha256sum >/dev/null 2>&1; then
    grep " ${ARCHIVE}$" checksums.txt | sha256sum -c -
elif command -v shasum >/dev/null 2>&1; then
    grep " ${ARCHIVE}$" checksums.txt | shasum -a 256 -c -
else
    echo "No SHA-256 utility found; refusing to install without verification" >&2
    exit 1
fi

mkdir -p "$INSTALL_DIR"
tar -xzf "$ARCHIVE"
cp "projectsetup_${VERSION}_${OS}_${ARCH}/projectsetup" "$INSTALL_DIR/projectsetup"
chmod +x "$INSTALL_DIR/projectsetup"

echo "Installed projectsetup to ${INSTALL_DIR}/projectsetup"
case ":$PATH:" in
    *":$INSTALL_DIR:"*) ;;
    *) echo "Add ${INSTALL_DIR} to PATH to run projectsetup." ;;
esac
