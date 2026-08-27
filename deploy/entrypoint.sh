#!/bin/sh
set -eu

STORAGE_ROOT="${STORAGE_ROOT:-/data}"
export STORAGE_ROOT
mkdir -p "$STORAGE_ROOT/subtitles" "$STORAGE_ROOT/temp" "$STORAGE_ROOT/quarantine"
chown -R subs:subs "$STORAGE_ROOT" 2>/dev/null || true
exec su-exec subs:subs /app/subs
