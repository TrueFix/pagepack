# syntax=docker/dockerfile:1
FROM node:22-bookworm-slim AS frontend
WORKDIR /src
COPY package.json package-lock.json ./
RUN npm ci
COPY index.html vite.config.js jsconfig.json ./
COPY frontend ./frontend
RUN npm run build

FROM golang:1.25-bookworm AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY main.go assets.go ./
COPY pkg ./pkg
COPY --from=frontend /src/dist ./dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/pagepack .

FROM debian:bookworm-slim AS runtime
LABEL org.opencontainers.image.title="Pagepack" \
      org.opencontainers.image.description="Self-hosted website archiver with live progress and S3-compatible storage" \
      org.opencontainers.image.version="1.0.0" \
      org.opencontainers.image.source="https://github.com/TrueFix/pagepack"
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates wget \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 10001 pagepack \
    && useradd --uid 10001 --gid 10001 --create-home --shell /usr/sbin/nologin pagepack \
    && mkdir -p /var/lib/pagepack/tmp \
    && chown -R pagepack:pagepack /var/lib/pagepack
COPY --from=backend /out/pagepack /usr/local/bin/pagepack
WORKDIR /var/lib/pagepack
ENV HTTP_ADDR=0.0.0.0:8000 \
    TEMP_DIR=/var/lib/pagepack/tmp
USER pagepack
EXPOSE 8000
STOPSIGNAL SIGTERM
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD wget --quiet --tries=1 --timeout=3 -O /dev/null http://127.0.0.1:8000/api/v1/health || exit 1
ENTRYPOINT ["/usr/local/bin/pagepack"]
