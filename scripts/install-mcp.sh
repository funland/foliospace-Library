#!/usr/bin/env sh
set -eu
VERSION="${FOLIOSPACE_MCP_VERSION:-1.03.1}"
BASE_URL="${FOLIOSPACE_MCP_RELEASE_BASE_URL:-https://github.com/funland/foliospace-Library/releases/download/mcp-v$VERSION}"
INSTALL_DIR="${FOLIOSPACE_MCP_INSTALL_DIR:-$HOME/.local/bin}"
case "$VERSION" in ''|*[!0-9.]*) echo 'Invalid MCP version' >&2; exit 1;; esac
case "${1:-}" in -h|--help)
  printf '%s\n' 'Install verified FolioSpace MCP from its independent GitHub release.' \
    'Overrides: FOLIOSPACE_MCP_VERSION, FOLIOSPACE_MCP_RELEASE_BASE_URL, FOLIOSPACE_MCP_INSTALL_DIR.' \
    'Existing URL/token configuration is never modified. Reconnect your MCP host after installation.'
  exit 0;; esac
case "$BASE_URL" in https://*) ;; *) echo 'Release URL must use HTTPS' >&2; exit 1;; esac
os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$os" in darwin|linux) ;; *) echo "Unsupported OS: $os" >&2; exit 1;; esac
case "$arch" in x86_64|amd64) arch=amd64;; arm64|aarch64) arch=arm64;; *) echo "Unsupported architecture: $arch" >&2; exit 1;; esac
command -v curl >/dev/null 2>&1 || { echo 'curl is required' >&2; exit 1; }
tmp_dir=$(mktemp -d)
candidate=''
backup=''
cleanup() {
  [ -z "$candidate" ] || rm -f "$candidate"
  [ -z "$backup" ] || rm -f "$backup"
  rm -rf "$tmp_dir"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
archive="foliospace-mcp_${VERSION}_${os}_${arch}.tar.gz"
fetch() { curl --proto '=https' --proto-redir '=https' -fsSL --connect-timeout 15 --max-time 180 "$1" -o "$2"; }
fetch "${BASE_URL%/}/checksums.txt" "$tmp_dir/checksums.txt"
fetch "${BASE_URL%/}/$archive" "$tmp_dir/$archive"
expected=$(awk -v file="$archive" '$2 == file {print $1}' "$tmp_dir/checksums.txt")
[ "${#expected}" -eq 64 ] || { echo 'Missing or ambiguous checksum' >&2; exit 1; }
case "$expected" in *[!a-f0-9]*) echo 'Invalid checksum' >&2; exit 1;; esac
if command -v shasum >/dev/null 2>&1; then
  actual=$(shasum -a 256 "$tmp_dir/$archive" | awk '{print $1}')
elif command -v sha256sum >/dev/null 2>&1; then
  actual=$(sha256sum "$tmp_dir/$archive" | awk '{print $1}')
else
  echo 'SHA-256 verification tool required' >&2; exit 1
fi
[ "$expected" = "$actual" ] || { echo 'Checksum mismatch; existing installation unchanged' >&2; exit 1; }
mkdir -p "$INSTALL_DIR"
candidate=$(mktemp "$INSTALL_DIR/.foliospace-mcp.candidate.XXXXXX")
# Extract only the named executable to stdout: never trust archive filesystem paths.
tar -xOzf "$tmp_dir/$archive" "${archive%.tar.gz}/foliospace-mcp" > "$candidate"
chmod 755 "$candidate"
printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"initialize"}' | "$candidate" > "$tmp_dir/initialize.json"
grep -F '"name":"foliospace-library"' "$tmp_dir/initialize.json" >/dev/null
grep -F "\"version\":\"$VERSION\"" "$tmp_dir/initialize.json" >/dev/null
if [ -e "$INSTALL_DIR/foliospace-mcp" ]; then
  backup=$(mktemp "$INSTALL_DIR/.foliospace-mcp.backup.XXXXXX")
  cp -p "$INSTALL_DIR/foliospace-mcp" "$backup"
  mv -f "$backup" "$INSTALL_DIR/.foliospace-mcp.previous"
  backup=''
fi
mv -f "$candidate" "$INSTALL_DIR/foliospace-mcp"
candidate=''
printf 'Installed FolioSpace MCP %s at %s/foliospace-mcp\n' "$VERSION" "$INSTALL_DIR"
printf '%s\n' 'URL/token unchanged. Reconnect your MCP host to load the new tools.'
