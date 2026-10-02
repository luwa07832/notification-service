// Package store owns the SQLite file and every write the service performs.
package store

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// Store wraps the SQLite handle so callers never touch database/sql directly.
type Store struct {
	db *sql.DB
}

// Open prepares the database file and the schema this service needs.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable wal: %w", err)
	}
	if _, err := db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set busy timeout: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &Store{db: db}, nil
}

// Ping reports whether the storage layer is usable.
func (s *Store) Ping() error { return s.db.Ping() }

// Close releases the database handle.
func (s *Store) Close() error { return s.db.Close() }

const schema = `
CREATE TABLE IF NOT EXISTS service_metadata (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS delivery_records (
	id             TEXT PRIMARY KEY,
	template_id    TEXT NOT NULL,
	channel        TEXT NOT NULL,
	occurred_at    TEXT NOT NULL,
	status         TEXT NOT NULL,
	retry_count    INTEGER NOT NULL CHECK (retry_count >= 0),
	failure_reason TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_delivery_records_channel_time
	ON delivery_records (channel, occurred_at);

CREATE TABLE IF NOT EXISTS notification_templates (
	template_id TEXT PRIMARY KEY,
	name        TEXT NOT NULL,
	body        TEXT NOT NULL,
	channels    TEXT NOT NULL,
	enabled     INTEGER NOT NULL CHECK (enabled IN (0, 1))
);
`
