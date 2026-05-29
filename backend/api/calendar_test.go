package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/philippgehrig/asimuth-automation/backend/asimut"
	"github.com/philippgehrig/asimuth-automation/backend/db"
	"github.com/philippgehrig/asimuth-automation/backend/scheduler"
)

func setupTestServer(t *testing.T) (*Server, *db.DB) {
	t.Helper()
	dbPath := t.TempDir() + "/test.db"
	database, err := db.New(dbPath)
	if err != nil {
		t.Fatalf("failed to create test db: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	client := asimut.NewClient("http://localhost", "test@test.com", "pass")
	sched := scheduler.New()

	srv := NewServer(database, client, sched, "testpass", "secret-token")
	return srv, database
}

func TestCalendarEndpoint_ValidToken(t *testing.T) {
	srv, database := setupTestServer(t)

	id, err := database.CreateBooking(db.BookingWish{
		Date:            "2026-06-01",
		StartTime:       "10:00",
		DurationMinutes: 60,
		RoomPriorities:  []int{114},
		Status:          "booked",
	})
	if err != nil {
		t.Fatalf("failed to create booking: %v", err)
	}

	err = database.UpdateBookingStatus(id, "booked", "MBP-326", nil, "")
	if err != nil {
		t.Fatalf("failed to update booking status: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/calendar/secret-token.ics", nil)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/calendar") {
		t.Errorf("expected text/calendar content type, got %q", ct)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "BEGIN:VCALENDAR") {
		t.Error("response is not valid iCal")
	}
	if !strings.Contains(body, "✅ MBP-326") {
		t.Errorf("expected booked event in calendar, got:\n%s", body)
	}
}

func TestCalendarEndpoint_InvalidToken(t *testing.T) {
	srv, _ := setupTestServer(t)

	req := httptest.NewRequest("GET", "/api/calendar/wrong-token.ics", nil)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for invalid token, got %d", rec.Code)
	}
}

func TestCalendarEndpoint_EmptyToken(t *testing.T) {
	dbPath := t.TempDir() + "/test.db"
	database, err := db.New(dbPath)
	if err != nil {
		t.Fatalf("failed to create test db: %v", err)
	}
	defer database.Close()

	client := asimut.NewClient("http://localhost", "test@test.com", "pass")
	sched := scheduler.New()
	srv := NewServer(database, client, sched, "testpass", "")

	req := httptest.NewRequest("GET", "/api/calendar/anything.ics", nil)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when calendar token is empty, got %d", rec.Code)
	}
}
