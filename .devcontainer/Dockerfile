FROM golang:1.23 
RUN apt-get update && apt-get install -y libreoffice-core --no-install-recommends

ENV APP_LOG_LEVEL=info \
    APP_LOG_JSON=false \
    APP_TEMP_DIR=/tmp/converter \
    CLEANUP_INTERVAL=1h \
    RETENTION_PERIOD=24h