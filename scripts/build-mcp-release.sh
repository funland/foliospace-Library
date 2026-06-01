#!/usr/bin/env sh
set -eu

VERSION="${VERSION:-0.881}"
OUT_DIR="${OUT_DIR:-dist/releases}"
BIN_NAME="foliospace-mcp"
PACKAGE_PREFIX="foliospace-mcp"
GO_CMD="${GO:-go}"
DOCKER_CMD="${DOCKER:-docker}"

TARGETS="
darwin amd64
darwin arm64
linux amd64
linux arm64
"

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
OUT_ABS="$ROOT_DIR/$OUT_DIR"

rm -rf "$OUT_ABS"
mkdir -p "$OUT_ABS"

while read -r GOOS GOARCH; do
  [ -n "${GOOS:-}" ] || continue

  name="${PACKAGE_PREFIX}_${VERSION}_${GOOS}_${GOARCH}"
  work="$OUT_ABS/$name"
  mkdir -p "$work"

  echo "building $name"
  if command -v "$GO_CMD" >/dev/null 2>&1; then
    (
      cd "$ROOT_DIR"
      CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" "$GO_CMD" build -trimpath -ldflags="-s -w" -o "$work/$BIN_NAME" ./cmd/foliospace-mcp
    )
  elif command -v "$DOCKER_CMD" >/dev/null 2>&1; then
    "$DOCKER_CMD" run --rm \
      -v "$ROOT_DIR:/src" \
      -w /src \
      -e CGO_ENABLED=0 \
      -e GOOS="$GOOS" \
      -e GOARCH="$GOARCH" \
      golang:1.22-alpine \
      go build -trimpath -ldflags="-s -w" -o "/src/$OUT_DIR/$name/$BIN_NAME" ./cmd/foliospace-mcp
  else
    echo "go is required. Install Go 1.22+, or install Docker and rerun this script." >&2
    exit 1
  fi

  cat > "$work/README.md" <<EOF
# FolioSpace Library MCP

Version: $VERSION
Target: $GOOS/$GOARCH

Set these environment variables in your MCP client config:

\`\`\`text
FOLIOSPACE_BASE_URL=http://your-nas-ip:8080
FOLIOSPACE_API_TOKEN=your-access-key
\`\`\`

The executable in this package is:

\`\`\`text
$BIN_NAME
\`\`\`
EOF

  chmod +x "$work/$BIN_NAME"
  tar -C "$OUT_ABS" -czf "$OUT_ABS/$name.tar.gz" "$name"
  rm -rf "$work"
done <<EOF
$TARGETS
EOF

(
  cd "$OUT_ABS"
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 *.tar.gz > checksums.txt
  elif command -v sha256sum >/dev/null 2>&1; then
    sha256sum *.tar.gz > checksums.txt
  else
    echo "warning: shasum/sha256sum not found; checksums.txt was not generated" >&2
  fi
)

echo "release artifacts written to $OUT_ABS"
