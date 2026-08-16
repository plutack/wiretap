#!/bin/sh
# Entrypoint for the wiretap-relay image.
#
# Coolify (and plain Docker bind mounts) attach host directories owned by
# root, which the unprivileged app user cannot write to. When the container
# starts as root, fix ownership of the data directory and this app's own
# database files, then re-exec the relay as the app user.
#
# Ownership fixes are deliberately NARROW and never recursive: the mount may
# share a host directory with unrelated content (mounting a system path such
# as /data would otherwise chown an entire host tree). Only the directory,
# the database file, and its WAL/SHM siblings are touched; files the app
# creates later are owned by the app user anyway.
set -e

DB_PATH="${WIRETAP_RELAY_DB:-/data/relay.db}"
DATA_DIR="${DB_PATH%/*}"

mkdir -p "$DATA_DIR"

if [ "$(id -u)" = "0" ]; then
    chown app:app "$DATA_DIR"
    chown app:app "$DB_PATH" "$DB_PATH-wal" "$DB_PATH-shm" 2>/dev/null || true
    exec su-exec app:app "$@"
fi

exec "$@"
