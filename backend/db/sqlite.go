package db

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// DB wraps the SQLite database connection.
type DB struct {
	conn *sql.DB
}

// New opens (or creates) a SQLite database at path and runs migrations.
func New(path string) (*DB, error) {
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	// Enable WAL mode and foreign keys.
	if _, err := conn.Exec("PRAGMA journal_mode=WAL"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("set WAL mode: %w", err)
	}
	if _, err := conn.Exec("PRAGMA foreign_keys=ON"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}

	d := &DB{conn: conn}
	if err := d.migrate(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return d, nil
}

// Close closes the underlying database connection.
func (d *DB) Close() error {
	return d.conn.Close()
}

// migrate creates the necessary tables if they don't exist.
func (d *DB) migrate() error {
	const schema = `
	CREATE TABLE IF NOT EXISTS recurring_schedules (
		id TEXT PRIMARY KEY,
		day_of_week INTEGER NOT NULL,
		start_time TEXT NOT NULL,
		duration_minutes INTEGER NOT NULL,
		room_priorities TEXT NOT NULL,
		active INTEGER DEFAULT 1,
		created_at TEXT DEFAULT (datetime('now'))
	);

	CREATE TABLE IF NOT EXISTS booking_wishes (
		id TEXT PRIMARY KEY,
		date TEXT NOT NULL,
		start_time TEXT NOT NULL,
		duration_minutes INTEGER NOT NULL,
		room_priorities TEXT NOT NULL,
		recurrence_id TEXT,
		status TEXT DEFAULT 'pending',
		result_room TEXT,
		result_duration INTEGER,
		result_event_id INTEGER,
		failure_reason TEXT,
		created_at TEXT DEFAULT (datetime('now')),
		updated_at TEXT DEFAULT (datetime('now')),
		FOREIGN KEY (recurrence_id) REFERENCES recurring_schedules(id) ON DELETE SET NULL
	);

	CREATE TABLE IF NOT EXISTS allowed_rooms (
		room_id INTEGER PRIMARY KEY
	);

	CREATE TABLE IF NOT EXISTS asimut_events (
		event_id    INTEGER PRIMARY KEY,
		title       TEXT NOT NULL,
		room_name   TEXT NOT NULL,
		start_time  TEXT NOT NULL,
		end_time    TEXT NOT NULL,
		last_synced TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS rooms_cache (
		room_id    INTEGER PRIMARY KEY,
		name       TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);
	`

	if _, err := d.conn.Exec(schema); err != nil {
		return fmt.Errorf("exec schema: %w", err)
	}

	// Add result_event_id column if it doesn't exist (migration for existing DBs)
	d.conn.Exec(`ALTER TABLE booking_wishes ADD COLUMN result_event_id INTEGER`)

	return nil
}
