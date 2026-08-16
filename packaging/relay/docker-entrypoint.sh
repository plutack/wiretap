#!/bin/sh
# Entrypoint for the wiretap-relay image.
#
# Coolify (and plain Docker bind mounts) attach host directories owned by
# root, which the unprivileged app user cannot write to. When the container
# starts as root, fix ownership of the data directory and this app's own
# database files, then re-exec the relay as the app user.
#
# Safety rules, learned the hard way:
#   - never chown recursively (a shared mount must not re-own host trees)
#   - never chown a directory that holds foreign content: if anything other
#     than the database files lives there, the mount is probably wrong
#     (e.g. the host's /data), so touch nothing and fail loudly instead.
# Files the app creates later are owned by the app user anyway.
set -e

DB_PATH="${WIRETAP_RELAY_DB:-/data/relay.db}"
DATA_DIR="${DB_PATH%/*}"

mkdir -p "$DATA_DIR"

if [ "$(id -u)" = "0" ]; then
    foreign=0
    for entry in "$DATA_DIR"/*; do
        [ -e "$entry" ] || continue
        case "$entry" in
            "$DB_PATH" | "$DB_PATH"-wal | "$DB_PATH"-shm) ;;
            *) foreign=1 ;;
        esac
    done

    if [ "$foreign" = "0" ]; then
        chown app:app "$DATA_DIR"
        chown app:app "$DB_PATH" "$DB_PATH"-wal "$DB_PATH"-shm 2>/dev/null || true
    else
        echo "wiretap-relay: $DATA_DIR contains files other than ${DB_PATH##*/}[-wal|-shm]; refusing to change its ownership." >&2
        echo "wiretap-relay: mount a dedicated directory (or volume) at the database location." >&2
    fi
    exec su-exec app:app "$@"
fi

exec "$@"
