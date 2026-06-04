package db

import (
	"os"
	"path/filepath"
	"testing"
)

func tempDB(t *testing.T) *DB {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	d, err := New(path)
	if err != nil {
		t.Fatalf("failed to create db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestNewDB_CreatesTablesOnInit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	d, err := New(path)
	if err != nil {
		t.Fatalf("failed to create db: %v", err)
	}
	defer d.Close()

	// Verify db file exists.
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("db file was not created")
	}

	// Verify tables exist by querying sqlite_master.
	var count int
	err = d.conn.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name IN ('booking_wishes', 'recurring_schedules')`).Scan(&count)
	if err != nil {
		t.Fatalf("failed to query sqlite_master: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 tables, got %d", count)
	}
}

func TestCreateAndListBookings(t *testing.T) {
	d := tempDB(t)

	// Create two bookings.
	id1, err := d.CreateBooking(BookingWish{
		Date:            "2026-05-12",
		StartTime:       "09:00",
		DurationMinutes: 60,
		RoomPriorities:  []int{1, 2, 3},
	})
	if err != nil {
		t.Fatalf("create booking 1: %v", err)
	}
	if id1 == "" {
		t.Fatal("expected non-empty id")
	}

	id2, err := d.CreateBooking(BookingWish{
		Date:            "2026-05-12",
		StartTime:       "10:00",
		DurationMinutes: 30,
		RoomPriorities:  []int{4, 5},
	})
	if err != nil {
		t.Fatalf("create booking 2: %v", err)
	}
	if id2 == "" {
		t.Fatal("expected non-empty id")
	}

	// List all bookings.
	bookings, err := d.ListBookings()
	if err != nil {
		t.Fatalf("list bookings: %v", err)
	}
	if len(bookings) != 2 {
		t.Fatalf("expected 2 bookings, got %d", len(bookings))
	}

	// Verify ordering (by start_time).
	if bookings[0].StartTime != "09:00" {
		t.Errorf("expected first booking at 09:00, got %s", bookings[0].StartTime)
	}
	if bookings[1].StartTime != "10:00" {
		t.Errorf("expected second booking at 10:00, got %s", bookings[1].StartTime)
	}

	// Verify room priorities unmarshaled correctly.
	if len(bookings[0].RoomPriorities) != 3 || bookings[0].RoomPriorities[0] != 1 {
		t.Errorf("unexpected room priorities: %v", bookings[0].RoomPriorities)
	}

	// Verify default status.
	if bookings[0].Status != "pending" {
		t.Errorf("expected status 'pending', got %q", bookings[0].Status)
	}

	// Test GetPendingBookings.
	pending, err := d.GetPendingBookings()
	if err != nil {
		t.Fatalf("get pending bookings: %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("expected 2 pending bookings, got %d", len(pending))
	}

	// Update status of first booking.
	dur := 60
	err = d.UpdateBookingStatus(id1, "confirmed", "Room A", &dur, nil, "")
	if err != nil {
		t.Fatalf("update booking status: %v", err)
	}

	// Now only one should be pending.
	pending, err = d.GetPendingBookings()
	if err != nil {
		t.Fatalf("get pending bookings after update: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending booking, got %d", len(pending))
	}

	// Delete second booking.
	err = d.DeleteBooking(id2)
	if err != nil {
		t.Fatalf("delete booking: %v", err)
	}

	bookings, err = d.ListBookings()
	if err != nil {
		t.Fatalf("list bookings after delete: %v", err)
	}
	if len(bookings) != 1 {
		t.Fatalf("expected 1 booking after delete, got %d", len(bookings))
	}
}
