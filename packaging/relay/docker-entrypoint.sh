#!/bin/sh
# Entrypoint for the wiretap-relay image.
#
# Coolify (and plain Docker bind mounts) attach host directories owned by
# root at /data, which the unprivileged app user cannot write to. When the
# container starts as root, fix /data ownership once and re-exec the relay
# as the app user; when already unprivileged, just start the relay.
set -e

mkdir -p /data

if [ "$(id -u)" = "0" ]; then
    chown -R app:app /data
    exec su-exec app:app "$@"
fi

exec "$@"
