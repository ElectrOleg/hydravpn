# Build stage
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build binary
RUN CGO_ENABLED=0 GOOS=linux go build -o hydra ./cmd/hydra

# Runtime stage
FROM alpine:3.19

# Install runtime dependencies for networking
RUN apk add --no-cache \
    iptables \
    iproute2 \
    ca-certificates

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/hydra .

# Copy entrypoint script
COPY docker-entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

# Expose VPN port
EXPOSE 8443/tcp
EXPOSE 8443/udp

# Use entrypoint for NAT setup
ENTRYPOINT ["/entrypoint.sh"]
CMD ["server", "--listen", ":8443"]
