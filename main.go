// main.go
package main

import (
	"context"
	"document-converter/services"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

var (
	templates *template.Template
	logger    *slog.Logger
)

const (
	StatusPending  = "pending"
	StatusComplete = "complete"
	StatusFailed   = "failed"
)

type Link struct {
	Href   string `json:"href"`
	Rel    string `json:"rel"`
	Method string `json:"method"`
}

type JobResponse struct {
	*services.ConvertJob
	Links []Link `json:"links"`
}

type Converter struct {
	tempDir string
	logger  *slog.Logger
}

func NewConverter(tempDir string, logger *slog.Logger) *Converter {
	return &Converter{
		tempDir: tempDir,
		logger:  logger,
	}
}

type responseWriter struct {
	http.ResponseWriter
	http.Hijacker
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

func loggingMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Create a response wrapper that includes Hijacker if available
		rw := &responseWriter{
			ResponseWriter: w,
			status:         http.StatusOK,
		}

		// Try to get the Hijacker from the original ResponseWriter
		if hijacker, ok := w.(http.Hijacker); ok {
			rw.Hijacker = hijacker
		}

		// Process request
		next.ServeHTTP(rw, r)

		// Don't log WebSocket upgrade requests that were successful
		if rw.status != http.StatusSwitchingProtocols {
			// Calculate duration
			duration := time.Since(start)

			// Log the request details
			logger.Info("http request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rw.status,
				"duration_ms", duration.Milliseconds(),
				"remote_addr", r.RemoteAddr,
				"content_length", r.ContentLength,
			)
		}
	})
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // In production, you might want to be more restrictive
	},
}

type ClientConnection struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

type WebSocketMessage struct {
	Type    string               `json:"type"`
	Payload *services.ConvertJob `json:"payload"`
}

type WebSocketDeleteMessage struct {
	Type  string `json:"type"`
	JobID string `json:"job_id"`
}

type Server struct {
	converter *Converter
	logger    *slog.Logger
	db        *services.DB
	clients   map[*ClientConnection]bool
	clientsMu sync.RWMutex
}

func (s *Server) handlePanel(w http.ResponseWriter, r *http.Request) {
	err := templates.ExecuteTemplate(w, "panel.html", nil)
	if err != nil {
		s.logger.Error("failed to execute template", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

func (s *Server) handleListConverts(w http.ResponseWriter, r *http.Request) {

	jobs, err := s.db.GetAllJobs()
	if err != nil {
		s.logger.Error("failed to get jobs", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(jobs)
}

func (s *Server) handleDownloadConvert(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	job, err := s.db.GetJob(id)
	if err != nil {
		s.logger.Error("failed to get job", "error", err, "id", id)
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}

	if job.Status != StatusComplete {
		http.Error(w, "Conversion not complete", http.StatusNotFound)
		return
	}

	if job.ConvertedFile == "" {
		http.Error(w, "No converted file available", http.StatusNotFound)
		return
	}

	// Open and serve the file
	file, err := os.Open(job.ConvertedFile)
	if err != nil {
		s.logger.Error("failed to open converted file",
			"error", err,
			"path", job.ConvertedFile,
		)
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}
	defer file.Close()

	// Set appropriate headers
	w.Header().Set("Content-Type", "text/html")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s",
		filepath.Base(job.ConvertedFile)))

	// Stream the file
	if _, err := io.Copy(w, file); err != nil {
		s.logger.Error("failed to stream file",
			"error", err,
			"job_id", id,
		)
	}
}

func (s *Server) handleGetConvert(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	job, err := s.db.GetJob(id)
	if err != nil {
		s.logger.Error("failed to get job", "error", err, "id", id)
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}

	// Create response with links
	response := JobResponse{
		ConvertJob: job,
		Links: []Link{
			{
				Href:   fmt.Sprintf("/converts/%s", job.ID),
				Rel:    "self",
				Method: "GET",
			},
		},
	}

	// Add download link only if conversion is complete
	if job.Status == StatusComplete && job.ConvertedFile != "" {
		response.Links = append(response.Links, Link{
			Href:   fmt.Sprintf("/convert-outcomes/%s", job.ID),
			Rel:    "download",
			Method: "GET",
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *Server) handleCreateConvert(w http.ResponseWriter, r *http.Request) {
	s.logger.Info("starting new conversion request")

	// Define allowed file extensions
	allowedExtensions := map[string]bool{
		".docx": true,
		".xlsx": true,
		".odt":  true,
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		s.logger.Error("no file provided in request",
			"error", err,
			"headers", header,
		)
		http.Error(w, "No file provided", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Validate file extension
	ext := strings.ToLower(filepath.Ext(header.Filename))
	if !allowedExtensions[ext] {
		s.logger.Error("invalid file extension",
			"filename", header.Filename,
			"extension", ext,
		)
		http.Error(w, "Invalid file type. Allowed types: .docx, .xlsx, .odt", http.StatusBadRequest)
		return
	}

	// Validate Content-Type
	allowedMimeTypes := map[string]bool{
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document": true,
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":       true,
		"application/vnd.oasis.opendocument.text":                                 true,
	}

	contentType := header.Header.Get("Content-Type")
	if !allowedMimeTypes[contentType] {
		s.logger.Error("invalid content type",
			"filename", header.Filename,
			"content_type", contentType,
		)
		http.Error(w, "Invalid file type", http.StatusBadRequest)
		return
	}

	s.logger.Info("received file for conversion",
		"filename", header.Filename,
		"size", header.Size,
		"content_type", header.Header.Get("Content-Type"),
	)

	jobID := uuid.New().String()

	job := &services.ConvertJob{
		ID:           jobID,
		OriginalFile: header.Filename,
		Status:       StatusPending,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	if err := s.db.CreateJob(job); err != nil {
		s.logger.Error("failed to create job in database",
			"error", err,
			"job_id", jobID,
		)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Broadcast the initial job creation
	s.broadcastJobUpdate(job)

	s.logger.Info("conversion job created",
		"job_id", jobID,
		"filename", header.Filename,
	)

	// Start processing in a goroutine to not block the response
	go s.processConversion(jobID, file, header.Filename)

	w.Header().Set("Location", fmt.Sprintf("/converts/%s", jobID))
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"id": jobID})
}

func (s *Server) processConversion(jobID string, file io.Reader, filename string) {
	s.logger.Info("starting conversion process",
		"job_id", jobID,
		"filename", filename,
	)

	// Get initial job state to broadcast
	initialJob, _ := s.db.GetJob(jobID)
	if initialJob != nil {
		s.broadcastJobUpdate(initialJob)
	}

	// Create job directory structure
	jobDir := filepath.Join(s.converter.tempDir, jobID)
	originalDir := filepath.Join(jobDir, "original")
	convertedDir := filepath.Join(jobDir, "converted")

	// Create directories
	for _, dir := range []string{originalDir, convertedDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			s.logger.Error("failed to create directory",
				"path", dir,
				"error", err,
				"job_id", jobID,
			)
			s.updateJobStatus(jobID, StatusFailed, err.Error())
			return
		}
	}

	// Save original file
	originalPath := filepath.Join(originalDir, filename)
	if err := saveFile(file, originalPath); err != nil {
		s.logger.Error("failed to save original file",
			"path", originalPath,
			"error", err,
			"job_id", jobID,
		)
		s.updateJobStatus(jobID, StatusFailed, err.Error())
		return
	}

	// Run conversion
	cmd := exec.Command(
		"libreoffice",
		"--convert-to", "html:HTML:EmbedImages",
		"--headless",
		"--outdir", convertedDir,
		originalPath,
	)

	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	// Check for specific error patterns in the output
	if err != nil || strings.Contains(outputStr, "Error:") || strings.Contains(outputStr, "failed:") {
		errorMsg := "Conversion failed"
		if strings.Contains(outputStr, "Error:") {
			// Extract error message between "Error:" and the next newline
			if idx := strings.Index(outputStr, "Error:"); idx != -1 {
				errorPart := outputStr[idx:]
				if newLineIdx := strings.Index(errorPart, "\n"); newLineIdx != -1 {
					errorMsg = errorPart[:newLineIdx]
				} else {
					errorMsg = errorPart
				}
			}
		}

		s.logger.Error("libreoffice conversion failed",
			"error", err,
			"output", outputStr,
			"command", cmd.String(),
			"job_id", jobID,
		)
		s.updateJobStatus(jobID, StatusFailed, errorMsg)
		return
	}

	// Log successful conversion
	s.logger.Info("libreoffice conversion completed",
		"output", outputStr,
		"job_id", jobID,
	)

	// Add permissions check and fix
	if err := os.Chmod(convertedDir, 0755); err != nil {
		s.logger.Error("failed to set converted directory permissions",
			"error", err,
			"path", convertedDir,
			"job_id", jobID,
		)
		s.updateJobStatus(jobID, StatusFailed, "Failed to set directory permissions")
		return
	}

	// Get the original filename without extension
	baseName := strings.TrimSuffix(filepath.Base(originalPath), filepath.Ext(originalPath))
	convertedFile := filepath.Join(convertedDir, baseName+".html")

	// Verify converted file exists and set permissions
	if _, err := os.Stat(convertedFile); os.IsNotExist(err) {
		errMsg := "Converted file not found after conversion"
		s.logger.Error(errMsg,
			"expected_file", convertedFile,
			"job_id", jobID,
		)
		s.updateJobStatus(jobID, StatusFailed, errMsg)
		return
	}

	// Set permissions on the converted file
	if err := os.Chmod(convertedFile, 0644); err != nil {
		s.logger.Error("failed to set converted file permissions",
			"error", err,
			"path", convertedFile,
			"job_id", jobID,
		)
		s.updateJobStatus(jobID, StatusFailed, "Failed to set file permissions")
		return
	}

	// Update job with converted file path
	job := &services.ConvertJob{
		ID:            jobID,
		Status:        StatusComplete,
		ConvertedFile: convertedFile,
		UpdatedAt:     time.Now(),
	}

	if err := s.db.UpdateJob(job); err != nil {
		s.logger.Error("failed to update job with converted file",
			"error", err,
			"job_id", jobID,
		)
		return
	}

	// Get the updated job to broadcast
	updatedJob, err := s.db.GetJob(jobID)
	if err != nil {
		s.logger.Error("failed to get updated job for broadcast",
			"error", err,
			"job_id", jobID,
		)
		return
	}

	// Broadcast the final update
	s.broadcastJobUpdate(updatedJob)
}

func saveFile(src io.Reader, dst string) error {
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, src)
	return err
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error("websocket upgrade failed", "error", err)
		return
	}

	client := &ClientConnection{conn: conn}

	s.clientsMu.Lock()
	s.clients[client] = true
	s.clientsMu.Unlock()

	defer func() {
		s.clientsMu.Lock()
		delete(s.clients, client)
		s.clientsMu.Unlock()
		conn.Close()
	}()

	for {
		messageType, _, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				s.logger.Error("websocket error", "error", err)
			}
			break
		}
		if messageType == websocket.PingMessage {
			client.mu.Lock()
			client.conn.WriteMessage(websocket.PongMessage, []byte{})
			client.mu.Unlock()
		}
	}
}

func (s *Server) handleDeleteConvert(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	// Check if job exists
	job, err := s.db.GetJob(id)
	if err != nil {
		s.logger.Error("failed to get job for deletion",
			"error", err,
			"id", id,
		)
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}

	// Only allow deletion of completed or failed jobs
	if job.Status != StatusComplete && job.Status != StatusFailed {
		s.logger.Warn("attempted to delete job with invalid status",
			"job_id", id,
			"status", job.Status,
		)
		http.Error(w, "Cannot delete job in progress", http.StatusForbidden)
		return
	}

	// Delete job files
	jobDir := filepath.Join(s.converter.tempDir, job.ID)
	if err := os.RemoveAll(jobDir); err != nil {
		s.logger.Error("failed to remove job directory",
			"error", err,
			"path", jobDir,
		)
		// Continue with DB deletion even if files deletion fails
	}

	// Delete from database
	if err := s.db.DeleteJob(id); err != nil {
		s.logger.Error("failed to delete job",
			"error", err,
			"id", id,
		)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Broadcast deletion via websocket
	s.broadcastJobDelete(id)

	// Return 204 No Content
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) broadcastJobDelete(jobID string) {
	message := WebSocketDeleteMessage{
		Type:  "job_delete",
		JobID: jobID,
	}

	messageBytes, err := json.Marshal(message)
	if err != nil {
		s.logger.Error("failed to marshal websocket delete message", "error", err)
		return
	}

	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()

	for client := range s.clients {
		client.mu.Lock()
		err := client.conn.WriteMessage(websocket.TextMessage, messageBytes)
		client.mu.Unlock()

		if err != nil {
			s.logger.Error("failed to send websocket delete message", "error", err)
			continue
		}
	}
}

func (s *Server) broadcastJobUpdate(job *services.ConvertJob) {
	message := WebSocketMessage{
		Type:    "job_update",
		Payload: job,
	}

	messageBytes, err := json.Marshal(message)
	if err != nil {
		s.logger.Error("failed to marshal websocket message", "error", err)
		return
	}

	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()

	for client := range s.clients {
		client.mu.Lock()
		err := client.conn.WriteMessage(websocket.TextMessage, messageBytes)
		client.mu.Unlock()

		if err != nil {
			s.logger.Error("failed to send websocket message", "error", err)
			continue
		}
	}
}

func (s *Server) updateJobStatus(jobID, status, errorMsg string) {
	job := &services.ConvertJob{
		ID:        jobID,
		Status:    status,
		Error:     errorMsg,
		UpdatedAt: time.Now(),
	}

	if err := s.db.UpdateJob(job); err != nil {
		s.logger.Error("failed to update job status",
			"error", err,
			"job_id", jobID,
			"status", status,
		)
	}

	// Broadcast the update to all connected clients
	s.broadcastJobUpdate(job)
}

func setupTempDir(logger *slog.Logger) (string, error) {
	tempDir := os.Getenv("APP_TEMP_DIR")
	if tempDir == "" {
		tempDir = "/tmp/converter"
	}

	// Only create the base temp directory
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		logger.Error("failed to create directory",
			"path", tempDir,
			"error", err,
		)
		return "", err
	}

	logger.Info("temp directory setup complete", "path", tempDir)
	return tempDir, nil
}

func setupLogger() *slog.Logger {
	// Get log level from environment
	logLevel := slog.LevelInfo
	if lvl := os.Getenv("APP_LOG_LEVEL"); lvl != "" {
		switch strings.ToLower(lvl) {
		case "debug":
			logLevel = slog.LevelDebug
		case "info":
			logLevel = slog.LevelInfo
		case "warn":
			logLevel = slog.LevelWarn
		case "error":
			logLevel = slog.LevelError
		}
	}

	// Create handler with options
	opts := &slog.HandlerOptions{
		Level:     logLevel,
		AddSource: true,
	}

	// Use JSON format if specified in environment
	var handler slog.Handler
	if useJSON := os.Getenv("APP_LOG_JSON"); strings.ToLower(useJSON) == "true" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	return slog.New(handler)
}

func (s *Server) startCleanupJob(ctx context.Context) {
	cleanupInterval := 1 * time.Hour // Run hourly
	if interval := os.Getenv("CLEANUP_INTERVAL"); interval != "" {
		if d, err := time.ParseDuration(interval); err == nil {
			cleanupInterval = d
		}
	}

	retentionPeriod := 24 * time.Hour // Keep files for 24 hours
	if period := os.Getenv("RETENTION_PERIOD"); period != "" {
		if d, err := time.ParseDuration(period); err == nil {
			retentionPeriod = d
		}
	}

	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()

	s.logger.Info("starting cleanup job",
		"interval", cleanupInterval,
		"retention_period", retentionPeriod,
	)

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("stopping cleanup job")
			return
		case <-ticker.C:
			cutoffTime := time.Now().Add(-retentionPeriod)
			jobs, err := s.db.GetOldJobs(cutoffTime)
			if err != nil {
				s.logger.Error("failed to get old jobs", "error", err)
				continue
			}

			for _, job := range jobs {
				// Delete files
				jobDir := filepath.Join(s.converter.tempDir, job.ID)
				if err := os.RemoveAll(jobDir); err != nil {
					s.logger.Error("failed to remove job directory",
						"error", err,
						"path", jobDir,
					)
					continue
				}

				// Delete from database
				if err := s.db.DeleteJob(job.ID); err != nil {
					s.logger.Error("failed to delete job from database",
						"error", err,
						"job_id", job.ID,
					)
					continue
				}

				s.logger.Info("cleaned up old job",
					"job_id", job.ID,
					"created_at", job.CreatedAt,
				)
			}
		}
	}
}

func main() {
	// Initialize logger
	logger = setupLogger()

	logger.Info("initializing application")

	// Load HTML templates
	var err error
	templates, err = template.ParseGlob("templates/*")
	if err != nil {
		logger.Error("failed to parse templates",
			"error", err,
		)
		os.Exit(1)
	}

	tempDir, err := setupTempDir(logger)
	if err != nil {
		logger.Error("failed to setup temp directory", "error", err)
		os.Exit(1)
	}

	db, err := services.InitDB()
	if err != nil {
		logger.Error("failed to initialize database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	converter := NewConverter(tempDir, logger)
	server := &Server{
		converter: converter,
		logger:    logger,
		db:        db,
		clients:   make(map[*ClientConnection]bool),
	}

	// Set up routes with logging middleware
	mux := http.NewServeMux()

	// Both / and /panel point to the same handler
	mux.HandleFunc("GET /", server.handlePanel)
	mux.HandleFunc("GET /panel", server.handlePanel)

	// Convert endpoints
	mux.HandleFunc("GET /converts", server.handleListConverts)
	mux.HandleFunc("POST /converts", server.handleCreateConvert)
	mux.HandleFunc("GET /converts/{id}", server.handleGetConvert)
	mux.HandleFunc("GET /convert-outcomes/{id}", server.handleDownloadConvert)
	mux.HandleFunc("DELETE /converts/{id}", server.handleDeleteConvert)
	mux.HandleFunc("GET /ws", server.handleWebSocket)

	// Wrap all handlers with logging middleware
	handler := loggingMiddleware(logger, mux)

	// Configure server
	srv := &http.Server{
		Addr:         ":8080",
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start cleanup job
	go server.startCleanupJob(ctx)

	// Handle shutdown signals
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigChan

		logger.Info("received shutdown signal", "signal", sig)
		cancel() // Cancel context for cleanup job

		// Give cleanup routines time to finish
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer shutdownCancel()

		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Error("server shutdown failed", "error", err)
		}
	}()

	logger.Info("server starting", "address", ":8080")
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		logger.Error("server failed to start",
			"error", err,
		)
		os.Exit(1)
	}
}
