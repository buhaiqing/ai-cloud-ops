# syntax=docker/dockerfile:1.7
#
# Multi-stage build for the Go backend.
# Final image: ~15MB (vs ~150MB for Python equivalent).

# --- Stage 1: build ---
FROM golang:1.23-alpine AS build

WORKDIR /src

# Cache module downloads
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build static binary (no cgo, stripped, no symbol tables)
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -trimpath \
    -o /out/aico ./cmd/aico

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -trimpath \
    -o /out/aico-mcp ./cmd/aico-mcp

# --- Stage 2: runtime ---
FROM gcr.io/distroless/static-debian12:nonroot AS runtime

COPY --from=build /out/aico /usr/local/bin/aico
COPY --from=build /out/aico-mcp /usr/local/bin/aico-mcp

USER nonroot:nonroot
EXPOSE 8080 8081

ENTRYPOINT ["/usr/local/bin/aico"]
CMD ["serve"]

# Healthcheck — pings /healthz via wget (distroless doesn't have curl)
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
    CMD ["/usr/local/bin/aico", "version"]