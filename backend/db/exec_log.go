package db

import "fmt"

// ExecLogEntry represents a single step in a booking's execution trace.
type ExecLogEntry struct {
	ID        int    `json:"id"`
	BookingID string `json:"booking_id"`
	Timestamp string `json:"timestamp"`
	Step      string `json:"step"`
	Message   string `json:"message"`
	Detail    string `json:"detail,omitempty"`
}

// LogExec appends a step to the booking execution log.
func (d *DB) LogExec(bookingID, step, message, detail string) {
	_, _ = d.conn.Exec(`
		INSERT INTO booking_exec_log (booking_id, step, message, detail)
		VALUES (?, ?, ?, ?)`,
		bookingID, step, message, detail,
	)
}

// GetExecLog returns all execution log entries for a booking.
func (d *DB) GetExecLog(bookingID string) ([]ExecLogEntry, error) {
	rows, err := d.conn.Query(`
		SELECT id, booking_id, timestamp, step, message, COALESCE(detail, '')
		FROM booking_exec_log
		WHERE booking_id = ?
		ORDER BY id`, bookingID)
	if err != nil {
		return nil, fmt.Errorf("query exec log: %w", err)
	}
	defer rows.Close()

	var entries []ExecLogEntry
	for rows.Next() {
		var e ExecLogEntry
		if err := rows.Scan(&e.ID, &e.BookingID, &e.Timestamp, &e.Step, &e.Message, &e.Detail); err != nil {
			return nil, fmt.Errorf("scan exec log: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
