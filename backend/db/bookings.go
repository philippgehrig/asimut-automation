package db

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

// BookingWish represents a single booking request.
type BookingWish struct {
	ID              string `json:"id"`
	Date            string `json:"date"`
	StartTime       string `json:"start_time"`
	DurationMinutes int    `json:"duration_minutes"`
	RoomPriorities  []int  `json:"room_priorities"`
	RecurrenceID    string `json:"recurrence_id,omitempty"`
	Status          string `json:"status"`
	ResultRoom      string `json:"result_room,omitempty"`
	ResultDuration  *int   `json:"result_duration,omitempty"`
	ResultEventID   *int   `json:"result_event_id,omitempty"`
	FailureReason   string `json:"failure_reason,omitempty"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

// CreateBooking inserts a new booking wish and returns the generated ID.
func (d *DB) CreateBooking(wish BookingWish) (string, error) {
	wish.ID = uuid.New().String()

	priorities, err := json.Marshal(wish.RoomPriorities)
	if err != nil {
		return "", fmt.Errorf("marshal room priorities: %w", err)
	}

	_, err = d.conn.Exec(`
		INSERT INTO booking_wishes (id, date, start_time, duration_minutes, room_priorities, recurrence_id, status)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		wish.ID, wish.Date, wish.StartTime, wish.DurationMinutes, string(priorities),
		nullableString(wish.RecurrenceID), coalesceStatus(wish.Status),
	)
	if err != nil {
		return "", fmt.Errorf("insert booking: %w", err)
	}

	return wish.ID, nil
}

// ListBookings returns all booking wishes ordered by date and start_time.
func (d *DB) ListBookings() ([]BookingWish, error) {
	rows, err := d.conn.Query(`
		SELECT id, date, start_time, duration_minutes, room_priorities, recurrence_id,
		       status, result_room, result_duration, result_event_id, failure_reason, created_at, updated_at
		FROM booking_wishes
		ORDER BY date, start_time`)
	if err != nil {
		return nil, fmt.Errorf("query bookings: %w", err)
	}
	defer rows.Close()

	return scanBookings(rows)
}

// GetPendingBookings returns bookings with status 'pending' or 'scheduled'.
func (d *DB) GetPendingBookings() ([]BookingWish, error) {
	rows, err := d.conn.Query(`
		SELECT id, date, start_time, duration_minutes, room_priorities, recurrence_id,
		       status, result_room, result_duration, result_event_id, failure_reason, created_at, updated_at
		FROM booking_wishes
		WHERE status IN ('pending', 'scheduled')
		ORDER BY date, start_time`)
	if err != nil {
		return nil, fmt.Errorf("query pending bookings: %w", err)
	}
	defer rows.Close()

	return scanBookings(rows)
}

// UpdateBookingStatus updates the status and result fields of a booking.
func (d *DB) UpdateBookingStatus(id, status, resultRoom string, resultDuration *int, resultEventID *int, failureReason string) error {
	_, err := d.conn.Exec(`
		UPDATE booking_wishes
		SET status = ?, result_room = ?, result_duration = ?, result_event_id = ?, failure_reason = ?, updated_at = datetime('now')
		WHERE id = ?`,
		status, nullableString(resultRoom), resultDuration, resultEventID, nullableString(failureReason), id,
	)
	if err != nil {
		return fmt.Errorf("update booking status: %w", err)
	}
	return nil
}

// DeleteBooking removes a booking wish by ID.
func (d *DB) DeleteBooking(id string) error {
	_, err := d.conn.Exec(`DELETE FROM booking_wishes WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete booking: %w", err)
	}
	return nil
}

// GetBookingByRecurrenceAndDate finds a booking for a given recurrence and date.
func (d *DB) GetBookingByRecurrenceAndDate(recurrenceID, date string) (*BookingWish, error) {
	row := d.conn.QueryRow(`
		SELECT id, date, start_time, duration_minutes, room_priorities, recurrence_id,
		       status, result_room, result_duration, result_event_id, failure_reason, created_at, updated_at
		FROM booking_wishes
		WHERE recurrence_id = ? AND date = ?`,
		recurrenceID, date,
	)

	b, err := scanBooking(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get booking by recurrence and date: %w", err)
	}
	return b, nil
}

// scanBookings reads all rows into a slice of BookingWish.
func scanBookings(rows *sql.Rows) ([]BookingWish, error) {
	var bookings []BookingWish
	for rows.Next() {
		var b BookingWish
		var priorities string
		var recurrenceID sql.NullString
		var resultRoom sql.NullString
		var resultDuration sql.NullInt64
		var resultEventID sql.NullInt64
		var failureReason sql.NullString

		err := rows.Scan(
			&b.ID, &b.Date, &b.StartTime, &b.DurationMinutes, &priorities,
			&recurrenceID, &b.Status, &resultRoom, &resultDuration, &resultEventID, &failureReason,
			&b.CreatedAt, &b.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan booking: %w", err)
		}

		if err := json.Unmarshal([]byte(priorities), &b.RoomPriorities); err != nil {
			return nil, fmt.Errorf("unmarshal room priorities: %w", err)
		}

		b.RecurrenceID = recurrenceID.String
		b.ResultRoom = resultRoom.String
		if resultDuration.Valid {
			dur := int(resultDuration.Int64)
			b.ResultDuration = &dur
		}
		if resultEventID.Valid {
			eid := int(resultEventID.Int64)
			b.ResultEventID = &eid
		}
		b.FailureReason = failureReason.String

		bookings = append(bookings, b)
	}
	return bookings, rows.Err()
}

// scanBooking reads a single row into a BookingWish.
func scanBooking(row *sql.Row) (*BookingWish, error) {
	var b BookingWish
	var priorities string
	var recurrenceID sql.NullString
	var resultRoom sql.NullString
	var resultDuration sql.NullInt64
	var resultEventID sql.NullInt64
	var failureReason sql.NullString

	err := row.Scan(
		&b.ID, &b.Date, &b.StartTime, &b.DurationMinutes, &priorities,
		&recurrenceID, &b.Status, &resultRoom, &resultDuration, &resultEventID, &failureReason,
		&b.CreatedAt, &b.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal([]byte(priorities), &b.RoomPriorities); err != nil {
		return nil, fmt.Errorf("unmarshal room priorities: %w", err)
	}

	b.RecurrenceID = recurrenceID.String
	b.ResultRoom = resultRoom.String
	if resultDuration.Valid {
		dur := int(resultDuration.Int64)
		b.ResultDuration = &dur
	}
	if resultEventID.Valid {
		eid := int(resultEventID.Int64)
		b.ResultEventID = &eid
	}
	b.FailureReason = failureReason.String

	return &b, nil
}

// nullableString returns a sql.NullString for optional string fields.
func nullableString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

// coalesceStatus returns "pending" if status is empty.
func coalesceStatus(s string) string {
	if s == "" {
		return "pending"
	}
	return s
}
