# Build stage
# Track the 1.26 line (not a fixed patch) so rebuilds pick up stdlib security
# fixes; the module's `go` directive requires 1.26.0 or newer.
FROM golang:1.26-alpine AS builder

WORKDIR /build

# The build context is this example directory. The SDK modules are downloaded
# as published versions, exactly as in your own app.
COPY . .

# Download dependencies
RUN go mod download

# Build a static binary; the SQLite driver is pure Go, so no C toolchain is needed
RUN CGO_ENABLED=0 GOOS=linux go build -o app .

# Runtime stage
FROM alpine:latest

# Install ca-certificates for HTTPS
RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/app .

# Create non-root user and data directory
RUN addgroup -g 1000 appuser && \
    adduser -D -u 1000 -G appuser appuser && \
    mkdir -p /app/data && \
    chown -R appuser:appuser /app

USER appuser

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

# Run the binary
ENTRYPOINT ["./app"]

