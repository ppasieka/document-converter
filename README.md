# Document Converter API

A Go-based REST API service that converts office documents (DOCX, XLSX, ODT) to HTML format using LibreOffice.

## Features

- Converts various document formats to HTML with embedded images
- Real-time conversion status updates via WebSocket
- Simple web interface for manual file uploads
- Automatic cleanup of old conversions
- RESTful API endpoints

## Prerequisites

- Docker
- LibreOffice (included in Docker image)

## Quick Start

```bash
docker build -t document-converter .
docker run --rm -p 8080:8080 document-converter
```

## API Endpoints

### Web Interface
- `GET /panel` - Web interface for manual file uploads

### REST API
- `GET /converts` - List all conversion jobs (limited to 100 most recent)
- `POST /converts` - Create new conversion job
  - Accepts multipart/form-data with `file` field
  - Returns job ID and Location header
- `GET /converts/:id` - Get conversion job status
- `GET /convert-outcomes/:id` - Download converted HTML file

### WebSocket
- `GET /ws` - WebSocket endpoint for real-time job status updates

## API Usage Examples

### Create Conversion Job
```bash
curl -X POST -F "file=@document.docx" http://localhost:8080/converts
```

### Check Conversion Status
```bash
curl http://localhost:8080/converts/{job-id}
```

### Download Converted File
```bash
curl -O http://localhost:8080/convert-outcomes/{job-id}
```

## Environment Variables

- `APP_LOG_LEVEL` - Logging level (debug, info, warn, error)
- `APP_LOG_JSON` - Enable JSON logging format (true/false)
- `APP_TEMP_DIR` - Directory for temporary files
- `CLEANUP_INTERVAL` - Interval for cleanup job (default: 1h)
- `RETENTION_PERIOD` - How long to keep old jobs (default: 24h)

## Response Examples

### Conversion Job Status
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "original_file": "document.docx",
  "status": "complete",
  "created_at": "2024-01-01T12:00:00Z",
  "updated_at": "2024-01-01T12:00:05Z",
  "links": [
    {
      "href": "/converts/550e8400-e29b-41d4-a716-446655440000",
      "rel": "self",
      "method": "GET"
    },
    {
      "href": "/convert-outcomes/550e8400-e29b-41d4-a716-446655440000",
      "rel": "download",
      "method": "GET"
    }
  ]
}
```

## License

MIT License
