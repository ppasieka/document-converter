#!/bin/sh
set -e

# Function to handle signals
cleanup() {
    echo "Received signal to terminate. Cleaning up..."
    # Add any cleanup tasks here
    exit 0
}

# Setup signal handlers
trap cleanup INT TERM

# Function to validate environment variables
validate_env() {
    # Check required directories
    if [ ! -d "/app/templates" ]; then
        echo "Error: Templates directory not found"
        exit 1
    fi

    # Validate LOG_LEVEL if set
    if [ -n "$APP_LOG_LEVEL" ]; then
        case "$APP_LOG_LEVEL" in
            debug|info|warn|error)
                ;;
            *)
                echo "Error: Invalid APP_LOG_LEVEL. Must be one of: debug, info, warn, error"
                exit 1
                ;;
        esac
    fi

    # Validate LOG_JSON if set
    if [ -n "$APP_LOG_JSON" ]; then
        case "$APP_LOG_JSON" in
            true|false)
                ;;
            *)
                echo "Error: Invalid APP_LOG_JSON. Must be true or false"
                exit 1
                ;;
        esac
    fi

    # Check for LibreOffice installation
    if ! command -v libreoffice >/dev/null 2>&1; then
        echo "Error: LibreOffice is not installed"
        exit 1
    fi

    # Check if converter binary exists and is executable
    if [ ! -x "./document-converter" ]; then
        echo "Error: Document converter binary not found or not executable"
        ls -l ./document-converter || echo "Binary does not exist"
        exit 1
    fi

    # Check if APP_TEMP_DIR is writable
    if [ ! -w "${APP_TEMP_DIR:-/tmp/converter}" ]; then
        echo "Error: APP_TEMP_DIR is not writable: ${APP_TEMP_DIR:-/tmp/converter}"
        exit 1
    fi

    # Validate cleanup configuration
    if [ -n "$CLEANUP_INTERVAL" ]; then
        if ! echo "$CLEANUP_INTERVAL" | grep -qE '^[0-9]+h$'; then
            echo "Error: CLEANUP_INTERVAL must be specified in hours (e.g., 1h)"
            exit 1
        fi
    fi

    if [ -n "$RETENTION_PERIOD" ]; then
        if ! echo "$RETENTION_PERIOD" | grep -qE '^[0-9]+h$'; then
            echo "Error: RETENTION_PERIOD must be specified in hours (e.g., 24h)"
            exit 1
        fi
    fi
}

# Function to initialize application
init_app() {
    echo "Initializing document converter service..."
    
    # Check if we can write to the temp directory
    if ! touch "$(mktemp -u)"; then
        echo "Error: Cannot write to temporary directory"
        exit 1
    fi

    # Print configuration
    echo "Configuration:"
    echo "- Log level: ${APP_LOG_LEVEL:-info}"
    echo "- JSON logging: ${APP_LOG_JSON:-false}"
}

# Main execution
echo "Starting entrypoint script..."

# Run validation
validate_env

# Initialize application
init_app

echo "Starting document converter with command: $@"

# Execute the main command
exec "$@"
