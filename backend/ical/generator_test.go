package ical

import (
	"strings"
	"testing"
	"time"

	"github.com/philippgehrig/asimuth-automation/backend/db"
)

func TestGenerateCalendar_BasicStructure(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Berlin")
	bookings := []db.BookingWish{
		{
			ID:              "test-1",
			Date:            "2026-06-01",
			StartTime:       "10:00",
			DurationMinutes: 60,
			Status:          "booked",
			ResultRoom:      "MBP-326",
			UpdatedAt:       "2026-06-01 09:00:00",
		},
	}

	result := Generate(bookings, nil, nil, loc)

	if !strings.Contains(result, "BEGIN:VCALENDAR") {
		t.Error("missing VCALENDAR begin")
	}
	if !strings.Contains(result, "END:VCALENDAR") {
		t.Error("missing VCALENDAR end")
	}
	if !strings.Contains(result, "BEGIN:VEVENT") {
		t.Error("missing VEVENT")
	}
	if !strings.Contains(result, "SUMMARY:✅ MBP-326") {
		t.Errorf("missing expected summary, got:\n%s", result)
	}
	if !strings.Contains(result, "UID:booking-test-1@asimut-automation") {
		t.Error("missing expected UID")
	}
}

func TestGenerateCalendar_StatusEmojis(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Berlin")

	tests := []struct {
		status string
		emoji  string
	}{
		{"booked", "✅"},
		{"partially_booked", "✅"},
		{"failed", "❌"},
		{"pending", "⏳"},
		{"scheduled", "⏳"},
	}

	for _, tt := range tests {
		bookings := []db.BookingWish{
			{
				ID: "test-" + tt.status, Date: "2026-06-01", StartTime: "10:00",
				DurationMinutes: 60, Status: tt.status, ResultRoom: "Room1",
				UpdatedAt: "2026-06-01 09:00:00",
			},
		}
		result := Generate(bookings, nil, nil, loc)
		if !strings.Contains(result, "SUMMARY:"+tt.emoji+" ") {
			t.Errorf("status %q: expected emoji %s in summary", tt.status, tt.emoji)
		}
	}
}

func TestGenerateCalendar_ManualEvents(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Berlin")
	events := []db.AsimutEvent{
		{
			EventID:   12345,
			Title:     "Einzelüben",
			RoomName:  "MBP-110",
			StartTime: "2026-06-02T14:00:00.000+02:00",
			EndTime:   "2026-06-02T15:00:00.000+02:00",
		},
	}

	result := Generate(nil, events, nil, loc)

	if !strings.Contains(result, "SUMMARY:📌 Einzelüben") {
		t.Errorf("missing manual event summary, got:\n%s", result)
	}
	if !strings.Contains(result, "UID:asimut-12345@asimut-automation") {
		t.Error("missing manual event UID")
	}
}

func TestGenerateCalendar_Deduplication(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Berlin")

	bookings := []db.BookingWish{
		{
			ID: "b1", Date: "2026-06-02", StartTime: "14:00",
			DurationMinutes: 60, Status: "booked", ResultRoom: "MBP-110",
			UpdatedAt: "2026-06-02 13:00:00",
		},
	}
	events := []db.AsimutEvent{
		{
			EventID:   99999,
			RoomName:  "MBP-110",
			StartTime: "2026-06-02T14:00:00.000+02:00",
			EndTime:   "2026-06-02T15:00:00.000+02:00",
		},
	}

	result := Generate(bookings, events, nil, loc)

	if strings.Contains(result, "asimut-99999") {
		t.Error("duplicate Asimut event should have been suppressed")
	}
	if !strings.Contains(result, "booking-b1") {
		t.Error("automation booking should still be present")
	}
}

func TestGenerateCalendar_PendingRoomResolution(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Berlin")

	bookings := []db.BookingWish{
		{
			ID: "pending-1", Date: "2026-06-03", StartTime: "09:00",
			DurationMinutes: 60, Status: "pending",
			RoomPriorities: []int{114, 50},
			UpdatedAt:      "2026-06-03 08:00:00",
		},
	}
	roomsCache := map[int]string{114: "MBP-326", 50: "FG1"}

	result := Generate(bookings, nil, roomsCache, loc)

	if !strings.Contains(result, "SUMMARY:⏳ MBP-326") {
		t.Errorf("expected pending booking to resolve room name, got:\n%s", result)
	}
}
