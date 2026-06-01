#!/usr/bin/env sh
set -eu

VERSION="${FOLIOSPACE_MCP_VERSION:-0.881}"
BASE_URL="${FOLIOSPACE_MCP_RELEASE_BASE_URL:-https://foliospace.app/releases}"
INSTALL_DIR="${FOLIOSPACE_MCP_INSTALL_DIR:-$HOME/.local/bin}"
BIN_NAME="foliospace-mcp"

usage() {
  cat <<EOF
Install FolioSpace Library MCP.

Environment overrides:
  FOLIOSPACE_MCP_VERSION=0.881
  FOLIOSPACE_MCP_RELEASE_BASE_URL=https://foliospace.app/releases
  FOLIOSPACE_MCP_INSTALL_DIR=\$HOME/.local/bin

Example:
  curl -fsSL https://foliospace.app/install-mcp.sh | sh
EOF
}

case "${1:-}" in
  -h|--help)
    usage
    exit 0
    ;;
esac

os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)

case "$os" in
  darwin|linux) ;;
  *)
    echo "unsupported OS: $os" >&2
    exit 1
    ;;
esac

case "$arch" in
  x86_64|amd64) arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *)
    echo "unsupported architecture: $arch" >&2
    exit 1
    ;;
esac

archive="${BIN_NAME}_${VERSION}_${os}_${arch}.tar.gz"
url="${BASE_URL%/}/$archive"
checksums_url="${BASE_URL%/}/checksums.txt"
tmp_dir=$(mktemp -d 2>/dev/null || mktemp -d -t foliospace-mcp)

cleanup() {
  rm -rf "$tmp_dir"
}
trap cleanup EXIT INT TERM

echo "downloading $url"
if command -v curl >/dev/null 2>&1; then
  curl -fsSL "$url" -o "$tmp_dir/$archive"
elif command -v wget >/dev/null 2>&1; then
  wget -q "$url" -O "$tmp_dir/$archive"
else
  echo "curl or wget is required" >&2
  exit 1
fi

if command -v curl >/dev/null 2>&1; then
  curl -fsSL "$checksums_url" -o "$tmp_dir/checksums.txt" || true
elif command -v wget >/dev/null 2>&1; then
  wget -q "$checksums_url" -O "$tmp_dir/checksums.txt" || true
fi

if [ -s "$tmp_dir/checksums.txt" ]; then
  expected=$(awk -v file="$archive" '$2 == file { print $1 }' "$tmp_dir/checksums.txt")
  if [ -n "$expected" ]; then
    if command -v shasum >/dev/null 2>&1; then
      actual=$(shasum -a 256 "$tmp_dir/$archive" | awk '{ print $1 }')
    elif command -v sha256sum >/dev/null 2>&1; then
      actual=$(sha256sum "$tmp_dir/$archive" | awk '{ print $1 }')
    else
      actual=""
      echo "checksum file found, but no shasum/sha256sum is available; skipping verification" >&2
    fi
    if [ -n "$actual" ] && [ "$actual" != "$expected" ]; then
      echo "checksum mismatch for $archive" >&2
      exit 1
    fi
  else
    echo "checksum entry not found for $archive; skipping verification" >&2
  fi
else
  echo "checksums.txt not found; skipping verification" >&2
fi

tar -xzf "$tmp_dir/$archive" -C "$tmp_dir"
mkdir -p "$INSTALL_DIR"
cp "$tmp_dir/${BIN_NAME}_${VERSION}_${os}_${arch}/$BIN_NAME" "$INSTALL_DIR/$BIN_NAME"
chmod +x "$INSTALL_DIR/$BIN_NAME"

echo "installed $BIN_NAME to $INSTALL_DIR/$BIN_NAME"
echo
echo "MCP client config example:"
cat <<EOF
{
  "mcpServers": {
    "foliospace-library": {
      "command": "$INSTALL_DIR/$BIN_NAME",
      "env": {
        "FOLIOSPACE_BASE_URL": "http://your-nas-ip:8080",
        "FOLIOSPACE_API_TOKEN": "your-access-key"
      }
    }
  }
}
EOF
