# Build stage
FROM golang:1.23 AS builder

# Add musl-dev for static compilation
RUN apt-get update && apt-get install -y musl-dev musl-tools

WORKDIR /app

# Copy go mod files
COPY go.mod ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build with musl-gcc for Alpine compatibility
RUN CC=musl-gcc CGO_ENABLED=1 GOOS=linux go build -ldflags '-linkmode external -extldflags "-static"' -o document-converter

# Final stage
FROM alpine:3.21

# Install LibreOffice and required fonts
RUN apk add --no-cache \
    libreoffice \
    font-misc-misc \
    font-terminus \
    font-inconsolata \
    font-dejavu \
    font-noto \
    font-noto-cjk \
    font-awesome \
    font-noto-extra \
    font-anonymous-pro-nerd \
    ttf-cantarell \
    ttf-dejavu \
    ttf-droid \
    ttf-font-awesome \
    ttf-freefont \
    ttf-hack \
    ttf-inconsolata \
    ttf-liberation \
    ttf-linux-libertine \
    ttf-mononoki \
    ttf-opensans \
    # Additional dependencies that might be needed
    bash \
    sqlite-libs \
    ca-certificates \
    tzdata \
    tiff \
    cups-libs \
    dbus-libs \
    sqlite \
    # Cleanup
    && rm -rf /var/cache/apk/*

# Create app directory and set permissions
WORKDIR /app

# Copy the binary from builder stage
COPY --from=builder /app/document-converter .

# Copy templates
COPY templates/ templates/
# Copy the entrypoint script
COPY entrypoint.sh /app/

# Set execute permissions for the entrypoint script
RUN chmod +x /app/entrypoint.sh

# Expose the application port
EXPOSE 8080

# Set default environment variables
ENV APP_LOG_LEVEL=info \
    APP_LOG_JSON=false \
    APP_TEMP_DIR=/tmp/converter \
    CLEANUP_INTERVAL=1h \
    RETENTION_PERIOD=24h

# Set the entrypoint and default command
# ENTRYPOINT ["/app/entrypoint.sh"]
CMD ["./document-converter"]
