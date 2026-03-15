package stats

import (
	"database/sql"
	"time"
)

// UpsertSubmission increments the per-minute submission count for username.
// Timestamps are truncated to the minute before storage.
func UpsertSubmission(db *sql.DB, username string, t time.Time) error {
	truncated := t.UTC().Truncate(time.Minute)
	_, err := db.Exec(`
		INSERT INTO submissions (username, timestamp, submission_count)
		VALUES ($1, $2, 1)
		ON CONFLICT (username, timestamp)
		DO UPDATE SET submission_count = submissions.submission_count + 1
	`, username, truncated)
	return err
}

// Migrate runs the initial schema migration.
func Migrate(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS submissions (
			username         VARCHAR(255) NOT NULL,
			timestamp        TIMESTAMP    NOT NULL,
			submission_count INT          NOT NULL DEFAULT 1,
			UNIQUE(username, timestamp)
		)
	`)
	return err
}
