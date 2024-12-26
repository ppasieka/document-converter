package services

import (
    "database/sql"
    "time"

    _ "github.com/mattn/go-sqlite3"
)

type DB struct {
    *sql.DB
}

type ConvertJob struct {
    ID            string    `json:"id"`
    OriginalFile  string    `json:"original_file"`
    ConvertedFile string    `json:"converted_file,omitempty"`
    Status        string    `json:"status"`
    Error         string    `json:"error,omitempty"`
    CreatedAt     time.Time `json:"created_at"`
    UpdatedAt     time.Time `json:"updated_at"`
}

func InitDB() (*DB, error) {
    db, err := sql.Open("sqlite3", "./converter.db")
    if err != nil {
        return nil, err
    }

    // Create converts table
    _, err = db.Exec(`
        CREATE TABLE IF NOT EXISTS converts (
            id TEXT PRIMARY KEY,
            original_file TEXT NOT NULL,
            converted_file TEXT,
            status TEXT NOT NULL,
            error TEXT,
            created_at DATETIME NOT NULL,
            updated_at DATETIME NOT NULL
        )
    `)
    if err != nil {
        return nil, err
    }

    return &DB{db}, nil
}

func (db *DB) CreateJob(job *ConvertJob) error {
    _, err := db.Exec(`
        INSERT INTO converts (
            id, original_file, converted_file, status, error, created_at, updated_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?)
    `,
        job.ID,
        job.OriginalFile,
        job.ConvertedFile,
        job.Status,
        job.Error,
        job.CreatedAt,
        job.UpdatedAt,
    )
    return err
}

func (db *DB) UpdateJob(job *ConvertJob) error {
    _, err := db.Exec(`
        UPDATE converts 
        SET status = ?,
            error = ?,
            converted_file = ?,
            updated_at = ?
        WHERE id = ?
    `,
        job.Status,
        job.Error,
        job.ConvertedFile,
        job.UpdatedAt,
        job.ID,
    )
    return err
}

func (db *DB) GetJob(id string) (*ConvertJob, error) {
    job := &ConvertJob{}
    err := db.QueryRow(`
        SELECT id, original_file, converted_file, status, error, created_at, updated_at
        FROM converts
        WHERE id = ?
    `, id).Scan(
        &job.ID,
        &job.OriginalFile,
        &job.ConvertedFile,
        &job.Status,
        &job.Error,
        &job.CreatedAt,
        &job.UpdatedAt,
    )
    if err != nil {
        return nil, err
    }
    return job, nil
}

func (db *DB) GetOldJobs(cutoffTime time.Time) ([]*ConvertJob, error) {
    rows, err := db.Query(`
        SELECT id, original_file, converted_file, status, error, created_at, updated_at
        FROM converts
        WHERE created_at < ?
    `, cutoffTime)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var jobs []*ConvertJob
    for rows.Next() {
        job := &ConvertJob{}
        err := rows.Scan(
            &job.ID,
            &job.OriginalFile,
            &job.ConvertedFile,
            &job.Status,
            &job.Error,
            &job.CreatedAt,
            &job.UpdatedAt,
        )
        if err != nil {
            return nil, err
        }
        jobs = append(jobs, job)
    }
    return jobs, rows.Err()
}

func (db *DB) DeleteJob(id string) error {
    _, err := db.Exec("DELETE FROM converts WHERE id = ?", id)
    return err
}

func (db *DB) GetAllJobs() ([]*ConvertJob, error) {
    rows, err := db.Query(`
        SELECT id, original_file, converted_file, status, error, created_at, updated_at
        FROM converts
        ORDER BY created_at DESC
        LIMIT 100
    `)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var jobs []*ConvertJob
    for rows.Next() {
        job := &ConvertJob{}
        err := rows.Scan(
            &job.ID,
            &job.OriginalFile,
            &job.ConvertedFile,
            &job.Status,
            &job.Error,
            &job.CreatedAt,
            &job.UpdatedAt,
        )
        if err != nil {
            return nil, err
        }
        jobs = append(jobs, job)
    }
    return jobs, rows.Err()
}
