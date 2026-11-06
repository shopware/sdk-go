# Build stage
FROM golang:1.26-alpine AS builder

WORKDIR /build

# The build context is this example directory. The SDK modules are downloaded
# as published versions, exactly as in your own app.
COPY . .

# Download dependencies
RUN go mod download

# Build a static binary; the SQLite driver is pure Go, so no C toolchain is needed
RUN CGO_ENABLED=0 GOOS=linux go build -o admin-extension .

# Runtime stage
FROM alpine:latest

# Install runtime dependencies
RUN apk --no-cache add ca-certificates tzdata

# Create app directory and non-root user
RUN addgroup -g 1001 appuser && \
    adduser -D -u 1001 -G appuser appuser && \
    mkdir -p /app/data && \
    chown -R appuser:appuser /app

WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/admin-extension .

# Use non-root user
USER appuser

# Expose port (internal)
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

# Run the application
CMD ["./admin-extension"]
