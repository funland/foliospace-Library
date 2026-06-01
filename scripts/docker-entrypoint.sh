#!/usr/bin/env sh
set -eu

config_dir="${FOLIOSPACE_CONFIG_DIR:-/config}"
library_dir="${FOLIOSPACE_LIBRARY_DIR:-/library}"

mkdir -p "$config_dir" "$library_dir"

if [ "$(id -u)" = "0" ]; then
  chown -R foliospace:foliospace "$config_dir" /app 2>/dev/null || true
  exec su-exec foliospace "$@"
fi

exec "$@"
