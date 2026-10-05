# syntax=docker/dockerfile:1

# The Share server with its website, in a small image without a shell. The build stages run on
# the builder's own platform and cross-compile, so arm64 images need no emulation. VERSION is
# what `share version` and the About screens say.

# The website, built once: it is the same on every platform.
FROM --platform=$BUILDPLATFORM node:22-slim AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
# The build writes into the server's embed folder, which it empties first.
RUN mkdir -p ../server/internal/webui/dist && npm run build

# The server, with the website inside.
FROM --platform=$BUILDPLATFORM golang:1.27 AS server
WORKDIR /src/server
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
COPY --from=web /src/server/internal/webui/dist ./internal/webui/dist
RUN test -f internal/webui/dist/index.html
ARG TARGETOS TARGETARCH VERSION=dev
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o /out/share ./cmd/share
# Where the files and the database go, with a bucket only the database. A new volume mounted
# there takes its owner.
RUN mkdir -p /out/data

# The image has the CA certificates, for a bucket over https, and /tmp for SQLite.
FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=server /out/share /usr/local/bin/share
COPY --from=server --chown=65532:65532 /out/data /data
COPY LICENSE NOTICE /usr/share/doc/share/
# config.json is mounted here; every command looks for it in the working directory. There is
# no VOLUME: without a mounted volume, `share check` says the files would be lost.
WORKDIR /config
EXPOSE 8080 8443
HEALTHCHECK --interval=30s --timeout=10s --start-period=30s CMD ["/usr/local/bin/share", "health"]
ENTRYPOINT ["/usr/local/bin/share"]
CMD ["serve"]
