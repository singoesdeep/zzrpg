# syntax=docker/dockerfile:1
#
# Multi-stage build for the zzrpg backend.
#
# The Go server links zzstat purely at runtime via purego (no cgo), so we only
# need the compiled Rust shared library present next to the binary. Stage 1
# builds that .so from the zzstat repo pinned to the SAME commit the Go module
# depends on (keep ZZSTAT_REF in sync with backend/go.mod). Stage 2 builds the
# static Go binary. The final image carries just the binary + the .so.
#
# Build context is the REPO ROOT (the backend module depends on the sibling
# ./sdk module via a replace directive, so both must be in the context):
#   docker build -f backend/Dockerfile -t zzrpg:latest .

# ── Stage 1: build the Rust zzstat FFI shared library ──────────────────────────
FROM rust:1-slim-bookworm AS zzstat
# Must match github.com/singoesdeep/zzstat/bindings/go in go.mod (commit a9e1e78).
ARG ZZSTAT_REF=a9e1e78827b584c174de28348985998fec1cfa55
RUN apt-get update \
    && apt-get install -y --no-install-recommends git \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /src
RUN git clone https://github.com/singoesdeep/zzstat.git . \
    && git checkout "${ZZSTAT_REF}"
RUN cargo build --release -p zzstat-ffi
# → /src/target/release/libzzstat_ffi.so

# ── Stage 2: build the Go server ───────────────────────────────────────────────
FROM golang:1.26-bookworm AS build
WORKDIR /src
# Both modules: backend resolves the reusable engine SDK via `replace => ../sdk`,
# so the sibling sdk/ must sit next to backend/ in the build tree.
COPY sdk/ ./sdk/
COPY backend/ ./backend/
WORKDIR /src/backend
RUN go mod download
# purego means no cgo; produce a small, reproducible static binary.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/zzrpg ./cmd/server

# ── Stage 3: minimal runtime ───────────────────────────────────────────────────
FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --system --uid 10001 --no-create-home app
WORKDIR /app
COPY --from=zzstat /src/target/release/libzzstat_ffi.so /app/libzzstat_ffi.so
COPY --from=build /out/zzrpg /app/zzrpg
# The loader checks ZZSTAT_LIB_PATH first (see platform/statclient/client.go).
ENV ZZSTAT_LIB_PATH=/app/libzzstat_ffi.so \
    PORT=8080
USER app
EXPOSE 8080
# Provide DATABASE_URL, JWT_SECRET, ALLOWED_ORIGINS, ENV=production at runtime.
ENTRYPOINT ["/app/zzrpg"]
