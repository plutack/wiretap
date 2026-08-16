# wiretap-relay container image.
#
# Build:  docker build -t wiretap-relay .
# Run:    docker run -e WIRETAP_ADMIN_TOKEN=... -p 8443:8443 -v relay-data:/data wiretap-relay
#
# The relay serves plain HTTP; terminate TLS with a reverse proxy in front of
# it (Coolify's Caddy, or your own). Pure-Go SQLite (modernc.org/sqlite) keeps
# the binary static.
#
# Runtime is alpine (not distroless) because the entrypoint needs a shell to
# fix /data ownership for bind mounts (Coolify mounts host dirs owned by
# root) before dropping to the unprivileged app user via su-exec; busybox
# wget also backs the container HEALTHCHECK.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -o /out/wiretap-relay ./cmd/wiretap-relay

FROM alpine:3.20
RUN apk add --no-cache su-exec \
    && addgroup -S app && adduser -S -G app app
COPY --from=build /out/wiretap-relay /usr/local/bin/wiretap-relay
COPY packaging/relay/docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod 0755 /usr/local/bin/docker-entrypoint.sh
# No ENV here on purpose: configuration comes from the platform (Coolify's
# dashboard env, docker run -e). The binary's own defaults (:8443, relay.db
# relative to WORKDIR) land the database at /data/relay.db anyway.
WORKDIR /data
USER root
EXPOSE 8443
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD wget -q -O /dev/null http://127.0.0.1:8443/health || exit 1
ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
CMD ["wiretap-relay"]
