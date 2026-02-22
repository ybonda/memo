#!/bin/sh
set -eu

GITHUB_REPO="ybonda/memo"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

GREEN='\033[0;32m'
RED='\033[0;31m'
RESET='\033[0m'

info() {
    printf "${GREEN}%s${RESET} %s\n" "✓" "$1"
}

error() {
    printf "${RED}%s${RESET} %s\n" "✗" "$1" >&2
    exit 1
}

# --- OS detection ---
RAW_OS="$(uname -s)"
case "$RAW_OS" in
    Darwin) OS="darwin" ;;
    Linux)  OS="linux" ;;
    *)      error "Error: unsupported OS: $RAW_OS" ;;
esac

# --- Arch detection ---
RAW_ARCH="$(uname -m)"
case "$RAW_ARCH" in
    x86_64|amd64)   ARCH="amd64" ;;
    arm64|aarch64)   ARCH="arm64" ;;
    *)               error "Error: unsupported architecture: $RAW_ARCH" ;;
esac

info "Detected platform: ${OS}/${ARCH}"

# --- Version resolution ---
if [ -n "${VERSION:-}" ]; then
    info "Using specified version: ${VERSION}"
else
    VERSION="$(curl -sSfL "https://api.github.com/repos/${GITHUB_REPO}/releases/latest" \
        | grep '"tag_name"' \
        | sed 's/.*"v\(.*\)".*/\1/')" \
        || error "Error: failed to fetch latest release version from GitHub"
    if [ -z "$VERSION" ]; then
        error "Error: could not determine latest version"
    fi
    info "Resolved latest version: ${VERSION}"
fi

# --- Temp directory with cleanup ---
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

# --- Download ---
ARCHIVE="memo_${VERSION}_${OS}_${ARCH}.tar.gz"
DOWNLOAD_URL="https://github.com/${GITHUB_REPO}/releases/download/v${VERSION}/${ARCHIVE}"
CHECKSUMS_URL="https://github.com/${GITHUB_REPO}/releases/download/v${VERSION}/checksums.txt"

printf "Downloading %s ...\n" "$ARCHIVE"
curl -sSfL -o "${TMP_DIR}/${ARCHIVE}" "$DOWNLOAD_URL" \
    || error "Error: failed to download ${DOWNLOAD_URL}"
info "Downloaded archive"

curl -sSfL -o "${TMP_DIR}/checksums.txt" "$CHECKSUMS_URL" \
    || error "Error: failed to download checksums.txt"
info "Downloaded checksums"

# --- Checksum verification ---
if command -v sha256sum >/dev/null 2>&1; then
    SHA_CMD="sha256sum"
elif command -v shasum >/dev/null 2>&1; then
    SHA_CMD="shasum -a 256"
else
    error "Error: neither sha256sum nor shasum found; cannot verify checksum"
fi

EXPECTED="$(grep "$ARCHIVE" "${TMP_DIR}/checksums.txt" | awk '{print $1}')"
if [ -z "$EXPECTED" ]; then
    error "Error: archive ${ARCHIVE} not found in checksums.txt"
fi

ACTUAL="$(cd "$TMP_DIR" && $SHA_CMD "$ARCHIVE" | awk '{print $1}')"

if [ "$EXPECTED" != "$ACTUAL" ]; then
    error "Error: checksum verification failed (expected ${EXPECTED}, got ${ACTUAL})"
fi
info "Checksum verified"

# --- Extract ---
tar xzf "${TMP_DIR}/${ARCHIVE}" -C "$TMP_DIR" \
    || error "Error: failed to extract archive"
info "Extracted archive"

# --- Install ---
if [ -w "$INSTALL_DIR" ]; then
    cp "${TMP_DIR}/memo" "${INSTALL_DIR}/memo"
else
    printf "Installing to %s requires elevated permissions.\n" "$INSTALL_DIR"
    sudo cp "${TMP_DIR}/memo" "${INSTALL_DIR}/memo"
fi
chmod +x "${INSTALL_DIR}/memo"
info "Installed memo to ${INSTALL_DIR}/memo"

printf "\n${GREEN}memo v%s${RESET} installed successfully at %s/memo\n" "$VERSION" "$INSTALL_DIR"
