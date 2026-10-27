#!/usr/bin/env bash
set -euo pipefail

REPO="${ANIMIX_REPO:-itsmenewbie03/AnimiX}"
VERSION="${ANIMIX_VERSION:-latest}"
INSTALL_DIR="${ANIMIX_INSTALL_DIR:-$HOME/.local/bin}"

usage() {
    cat <<'EOF'
Install the AnimiX binary from GitHub Releases.

Usage:
  install.sh [options]

Options:
  --version, -v <tag>   Release tag to install (default: latest)
  --dir, -d <path>      Install directory (default: ~/.local/bin)
  --repo <owner/repo>   Source repository (default: itsmenewbie03/AnimiX)
  --help, -h            Show this help

Environment:
  ANIMIX_VERSION        Same as --version
  ANIMIX_INSTALL_DIR    Same as --dir
  ANIMIX_REPO           Same as --repo

Examples:
  curl -fsSL https://raw.githubusercontent.com/itsmenewbie03/AnimiX/main/install.sh | bash
  curl -fsSL https://raw.githubusercontent.com/itsmenewbie03/AnimiX/main/install.sh | bash -s -- --version v0.1.0
EOF
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --version | -v)
            [[ $# -ge 2 ]] || { echo "missing value for $1" >&2; exit 1; }
            VERSION="$2"
            shift 2
            ;;
        --dir | -d)
            [[ $# -ge 2 ]] || { echo "missing value for $1" >&2; exit 1; }
            INSTALL_DIR="$2"
            shift 2
            ;;
        --repo)
            [[ $# -ge 2 ]] || { echo "missing value for $1" >&2; exit 1; }
            REPO="$2"
            shift 2
            ;;
        --help | -h)
            usage
            exit 0
            ;;
        *)
            echo "unknown argument: $1" >&2
            usage >&2
            exit 1
            ;;
    esac
done

command -v curl >/dev/null 2>&1 || {
    echo "curl is required" >&2
    exit 1
}

sha256_file() {
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$1" | awk '{print $1}'
    elif command -v shasum >/dev/null 2>&1; then
        shasum -a 256 "$1" | awk '{print $1}'
    else
        echo "sha256sum or shasum is required" >&2
        exit 1
    fi
}

case "$(uname -s)" in
    Linux*) OS=linux ;;
    Darwin*) OS=darwin ;;
    MINGW* | MSYS* | CYGWIN*) OS=windows ;;
    *)
        echo "unsupported operating system: $(uname -s)" >&2
        exit 1
        ;;
esac

case "$(uname -m)" in
    x86_64 | amd64) ARCH=amd64 ;;
    arm64 | aarch64) ARCH=arm64 ;;
    *)
        echo "unsupported architecture: $(uname -m)" >&2
        exit 1
        ;;
esac

EXT=""
if [[ "$OS" == "windows" ]]; then
    EXT=".exe"
fi

if [[ "$VERSION" == "latest" ]]; then
    API="https://api.github.com/repos/$REPO/releases/latest"
    if ! RESPONSE="$(curl -fsSL \
        -H 'Accept: application/vnd.github+json' \
        -H 'User-Agent: animix-install' \
        "$API")"; then
        echo "could not query $API" >&2
        exit 1
    fi
    VERSION="$(printf '%s' "$RESPONSE" | grep -m1 '"tag_name"' | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')"
    if [[ -z "$VERSION" ]]; then
        echo "could not resolve the latest release for $REPO" >&2
        exit 1
    fi
fi

ASSET="animix_${OS}_${ARCH}${EXT}"
BASE="https://github.com/$REPO/releases/download/$VERSION"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

echo "Downloading AnimiX $VERSION for $OS/$ARCH..."
if ! curl -fsSL "$BASE/$ASSET" -o "$TMP/$ASSET"; then
    echo "could not download $BASE/$ASSET" >&2
    exit 1
fi
if ! curl -fsSL "$BASE/SHA256SUMS" -o "$TMP/SHA256SUMS"; then
    echo "could not download $BASE/SHA256SUMS" >&2
    exit 1
fi

EXPECTED="$(awk -v asset="$ASSET" '$2 == asset || $2 == "*" asset { print $1; exit }' "$TMP/SHA256SUMS")"
if [[ -z "$EXPECTED" ]]; then
    echo "no checksum for $ASSET in SHA256SUMS" >&2
    exit 1
fi
ACTUAL="$(sha256_file "$TMP/$ASSET")"
if [[ "$EXPECTED" != "$ACTUAL" ]]; then
    echo "checksum mismatch for $ASSET" >&2
    exit 1
fi

mkdir -p "$INSTALL_DIR"
TARGET="$INSTALL_DIR/animix$EXT"
STAGED="$INSTALL_DIR/.animix-update-$$$EXT"
trap 'rm -rf "$TMP"; rm -f "$STAGED"' EXIT
cp "$TMP/$ASSET" "$STAGED"
chmod 0755 "$STAGED"
# Replacing the directory entry keeps a running AnimiX process on its old
# inode. Writing directly to TARGET fails with ETXTBSY on Linux.
mv -f "$STAGED" "$TARGET"

echo "Installed AnimiX $VERSION to $TARGET"
echo
echo "Next: install a runtime JAR before the first launch:"
echo "  $TARGET --install-runtime             # latest upstream"
echo "  $TARGET --install-runtime fork@latest # Cloudflare-aware fork"
case ":$PATH:" in
    *":$INSTALL_DIR:"*) ;;
    *)
        echo
        echo "Add $INSTALL_DIR to PATH, for example:"
        echo "  export PATH=\"$INSTALL_DIR:\$PATH\""
        ;;
esac
