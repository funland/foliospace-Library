#!/usr/bin/env sh
set -eu
VERSION="${VERSION:-1.03.1}"
OUT_DIR="${OUT_DIR:-dist/releases/mcp-$VERSION}"
GO_CMD="${GO:-go}"
case "$VERSION" in ''|*[!0-9.]*) echo 'Invalid version' >&2; exit 1;; esac
ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
case "$OUT_DIR" in /*) OUT_ABS="$OUT_DIR";; *) OUT_ABS="$ROOT_DIR/$OUT_DIR";; esac
command -v "$GO_CMD" >/dev/null 2>&1 || { echo 'Go 1.24+ is required' >&2; exit 1; }
mkdir -p "$OUT_ABS"
stage=$(mktemp -d "$OUT_ABS/.build.XXXXXX")
trap 'rm -rf "$stage"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
for target in darwin-amd64 darwin-arm64 linux-amd64 linux-arm64; do
  os=${target%-*}; arch=${target#*-}
  name="foliospace-mcp_${VERSION}_${os}_${arch}"
  mkdir "$stage/$name"
  (cd "$ROOT_DIR"; CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" "$GO_CMD" build -trimpath -ldflags='-s -w' -o "$stage/$name/foliospace-mcp" ./cmd/foliospace-mcp)
  printf 'FolioSpace MCP %s (%s). Configure FOLIOSPACE_BASE_URL and FOLIOSPACE_API_TOKEN in your MCP host.\n' "$VERSION" "$target" > "$stage/$name/README.txt"
  tar -C "$stage" -czf "$stage/$name.tar.gz" "$name"
done
cp "$ROOT_DIR/scripts/install-mcp.sh" "$stage/install-mcp.sh"
(cd "$stage"; if command -v shasum >/dev/null 2>&1; then shasum -a 256 ./*.tar.gz install-mcp.sh; else sha256sum ./*.tar.gz install-mcp.sh; fi) | sed 's|  ./|  |' > "$stage/checksums.txt"
# Publish only this version's outputs. Never recursively erase OUT_DIR.
for file in "$stage"/*.tar.gz "$stage/install-mcp.sh" "$stage/checksums.txt"; do mv -f "$file" "$OUT_ABS/"; done
printf 'Release artifacts written to %s\n' "$OUT_ABS"
