# Multi-stage Dockerfile for Rig
# Stage 1: Build the Go binary
# Stage 2: Minimal runtime image with binary only

# ─── Stage 1: Builder ────────────────────────────────────────────────
FROM golang:1.25-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates

# Set working directory
WORKDIR /build

# Copy go.mod and go.sum first for better layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary
# CGO_ENABLED=0 for static binary (works with scratch/distroless)
# -ldflags="-w -s" strips debug info for smaller binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s -X main.version=docker" \
    -o rig \
    ./cmd/rig

# ─── Stage 2: Runtime ────────────────────────────────────────────────
FROM gcr.io/distroless/static-debian12:nonroot

# Copy CA certificates from builder
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy binary from builder
COPY --from=builder /build/rig /rig

# Use non-root user (distroless nonroot = UID 65532)
# Already set by :nonroot tag, but explicit for clarity
USER 65532:65532

# Expose default webhook server port
EXPOSE 8080

# Set entrypoint
ENTRYPOINT ["/rig"]

# Default command: run server
CMD ["run"]
