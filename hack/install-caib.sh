#!/bin/bash
set -euo pipefail

REPO="centos-automotive-suite/automotive-dev-operator"

# Determine version
VERSION="${1:-latest}"

if [ "$VERSION" = "latest" ]; then
    VERSION=$(curl -sI "https://github.com/${REPO}/releases/latest" | grep -i '^location:' | sed 's|.*/||' | tr -d '\r')
    if [ -z "$VERSION" ]; then
        echo "Error: could not determine latest release version" >&2
        exit 1
    fi
fi

# Detect OS and architecture
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "${OS}" in
    linux)
        case "${ARCH}" in
            x86_64)  SUFFIX="amd64" ;;
            aarch64) SUFFIX="arm64" ;;
            *) echo "Unsupported architecture: ${ARCH}" >&2; exit 1 ;;
        esac
        ;;
    darwin)
        case "${ARCH}" in
            arm64) SUFFIX="darwin" ;;
            *) echo "Unsupported architecture: ${ARCH}" >&2; exit 1 ;;
        esac
        ;;
    *)
        echo "Unsupported OS: ${OS}" >&2
        exit 1
        ;;
esac

URL="https://github.com/${REPO}/releases/download/${VERSION}/caib-${VERSION}-${SUFFIX}"
INSTALL_DIR="/usr/local/bin"

TMPFILE=$(mktemp)
trap 'rm -f "$TMPFILE"' EXIT

echo "Downloading caib ${VERSION} for ${OS}/${ARCH}..."
curl -fSL -o "$TMPFILE" "$URL"
chmod +x "$TMPFILE"

if [ -w "$INSTALL_DIR" ]; then
    mv "$TMPFILE" "${INSTALL_DIR}/caib"
else
    echo "Installing to ${INSTALL_DIR} (requires sudo)..."
    sudo mv "$TMPFILE" "${INSTALL_DIR}/caib"
fi

echo "caib ${VERSION} installed to ${INSTALL_DIR}/caib"
