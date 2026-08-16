# wiretap-relay container image.
#
# Build:  docker build -t wiretap-relay .
# Run:    docker run -e WIRETAP_ADMIN_TOKEN=... -p 8443:8443 -v relay-data:/data wiretap-relay
#
# The relay serves plain HTTP; terminate TLS with a reverse proxy in front of
# it (Coolify's Caddy, or your own). Pure-Go SQLite (modernc.org/sqlite) keeps
# the image CGO-free and static.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -o /out/wiretap-relay ./cmd/wiretap-relay \
    && mkdir -p /out/data

FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/wiretap-relay /usr/local/bin/wiretap-relay
# Writable home for the SQLite database when a volume is mounted at /data
# (COPY --chown because distroless has no chown binary).
COPY --from=build --chown=nonroot:nonroot /out/data /data
WORKDIR /data
ENV WIRETAP_RELAY_ADDR=:8443 \
    WIRETAP_RELAY_DB=/data/relay.db
USER nonroot:nonroot
EXPOSE 8443
ENTRYPOINT ["wiretap-relay"]
