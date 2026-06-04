package ical

import (
	"fmt"
	"strings"
	"time"

	"github.com/philippgehrig/asimuth-automation/backend/db"
)

const icalTimeFormat = "20060102T150405"

var asimutTimeFormats = []string{
	"2006-01-02T15:04:05.000-07:00",
	"2006-01-02T15:04:05-07:00",
}

func parseAsimutTime(s string) (time.Time, error) {
	for _, f := range asimutTimeFormats {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse asimut time: %s", s)
}

func Generate(bookings []db.BookingWish, asimutEvents []db.AsimutEvent, roomsCache map[int]string, loc *time.Location) string {
	var b strings.Builder

	b.WriteString("BEGIN:VCALENDAR\r\n")
	b.WriteString("VERSION:2.0\r\n")
	b.WriteString("PRODID:-//Asimut Automation//Booking Calendar//EN\r\n")
	b.WriteString("CALSCALE:GREGORIAN\r\n")
	b.WriteString("METHOD:PUBLISH\r\n")
	b.WriteString("X-WR-CALNAME:Practice Room Bookings\r\n")
	b.WriteString("X-WR-TIMEZONE:Europe/Berlin\r\n")
	b.WriteString("REFRESH-INTERVAL;VALUE=DURATION:PT1H\r\n")

	for _, bk := range bookings {
		writeBookingEvent(&b, bk, roomsCache, loc)
	}

	filtered := filterDuplicates(asimutEvents, bookings, loc)
	for _, ev := range filtered {
		writeAsimutEvent(&b, ev, loc)
	}

	b.WriteString("END:VCALENDAR\r\n")
	return b.String()
}

func writeBookingEvent(b *strings.Builder, bk db.BookingWish, roomsCache map[int]string, loc *time.Location) {
	emoji := statusEmoji(bk.Status)
	room := bk.ResultRoom
	if room == "" {
		room = resolveRoomFromPriorities(bk.RoomPriorities, roomsCache)
	}

	start, end := bookingTimes(bk, loc)
	if start.IsZero() {
		return
	}

	description := statusDescription(bk)

	b.WriteString("BEGIN:VEVENT\r\n")
	fmt.Fprintf(b, "UID:booking-%s@asimut-automation\r\n", bk.ID)
	fmt.Fprintf(b, "DTSTART;TZID=Europe/Berlin:%s\r\n", start.Format(icalTimeFormat))
	fmt.Fprintf(b, "DTEND;TZID=Europe/Berlin:%s\r\n", end.Format(icalTimeFormat))
	fmt.Fprintf(b, "SUMMARY:%s %s\r\n", emoji, room)
	if description != "" {
		fmt.Fprintf(b, "DESCRIPTION:%s\r\n", escapeIcal(description))
	}
	if room != "" && room != "Pending" {
		fmt.Fprintf(b, "LOCATION:%s\r\n", room)
	}
	if bk.UpdatedAt != "" {
		if t, err := time.ParseInLocation("2006-01-02 15:04:05", bk.UpdatedAt, loc); err == nil {
			fmt.Fprintf(b, "LAST-MODIFIED:%s\r\n", t.UTC().Format("20060102T150405Z"))
		}
	}
	b.WriteString("END:VEVENT\r\n")
}

func writeAsimutEvent(b *strings.Builder, ev db.AsimutEvent, loc *time.Location) {
	start, _ := parseAsimutTime(ev.StartTime)
	end, _ := parseAsimutTime(ev.EndTime)
	if start.IsZero() || end.IsZero() {
		return
	}
	start = start.In(loc)
	end = end.In(loc)

	b.WriteString("BEGIN:VEVENT\r\n")
	fmt.Fprintf(b, "UID:asimut-%d@asimut-automation\r\n", ev.EventID)
	fmt.Fprintf(b, "DTSTART;TZID=Europe/Berlin:%s\r\n", start.Format(icalTimeFormat))
	fmt.Fprintf(b, "DTEND;TZID=Europe/Berlin:%s\r\n", end.Format(icalTimeFormat))
	fmt.Fprintf(b, "SUMMARY:📅 %s\r\n", ev.RoomName)
	b.WriteString("DESCRIPTION:Manual booking from Asimut\r\n")
	if ev.RoomName != "" {
		fmt.Fprintf(b, "LOCATION:%s\r\n", ev.RoomName)
	}
	b.WriteString("END:VEVENT\r\n")
}

func filterDuplicates(asimutEvents []db.AsimutEvent, bookings []db.BookingWish, loc *time.Location) []db.AsimutEvent {
	type bookingKey struct {
		date string
		room string
		hour int
		min  int
	}

	bookedSet := make(map[bookingKey]bool)
	for _, bk := range bookings {
		if bk.Status != "booked" && bk.Status != "partially_booked" {
			continue
		}
		if bk.ResultRoom == "" {
			continue
		}
		parts := strings.Split(bk.StartTime, ":")
		if len(parts) != 2 {
			continue
		}
		var h, m int
		fmt.Sscanf(parts[0], "%d", &h)
		fmt.Sscanf(parts[1], "%d", &m)
		bookedSet[bookingKey{date: bk.Date, room: bk.ResultRoom, hour: h, min: m}] = true
	}

	var filtered []db.AsimutEvent
	for _, ev := range asimutEvents {
		t, err := parseAsimutTime(ev.StartTime)
		if err != nil {
			filtered = append(filtered, ev)
			continue
		}
		t = t.In(loc)

		isDup := false
		for delta := -5; delta <= 5; delta++ {
			checkTime := t.Add(time.Duration(delta) * time.Minute)
			k := bookingKey{
				date: checkTime.Format("2006-01-02"),
				room: ev.RoomName,
				hour: checkTime.Hour(),
				min:  checkTime.Minute(),
			}
			if bookedSet[k] {
				isDup = true
				break
			}
		}
		if !isDup {
			filtered = append(filtered, ev)
		}
	}
	return filtered
}

func statusEmoji(status string) string {
	switch status {
	case "booked", "partially_booked":
		return "✅"
	case "moved":
		return "⚠️"
	case "failed":
		return "❌"
	default:
		return "⏳"
	}
}

func statusDescription(bk db.BookingWish) string {
	switch bk.Status {
	case "booked":
		if bk.ResultDuration != nil {
			return fmt.Sprintf("Booked: %d minutes", *bk.ResultDuration)
		}
		return "Booked successfully"
	case "partially_booked":
		if bk.ResultDuration != nil {
			return fmt.Sprintf("Partially booked: %d/%d minutes", *bk.ResultDuration, bk.DurationMinutes)
		}
		return "Partially booked"
	case "moved":
		if bk.FailureReason != "" {
			return "Modified: " + bk.FailureReason
		}
		return "Booking was modified or cancelled on Asimut"
	case "failed":
		if bk.FailureReason != "" {
			return "Failed: " + bk.FailureReason
		}
		return "Booking failed"
	case "pending", "scheduled":
		return "Pending execution"
	default:
		return ""
	}
}

func resolveRoomFromPriorities(priorities []int, cache map[int]string) string {
	if len(priorities) == 0 {
		return "Pending"
	}
	if cache != nil {
		if name, ok := cache[priorities[0]]; ok {
			return name
		}
	}
	return "Pending"
}

func bookingTimes(bk db.BookingWish, loc *time.Location) (time.Time, time.Time) {
	date, err := time.ParseInLocation("2006-01-02", bk.Date, loc)
	if err != nil {
		return time.Time{}, time.Time{}
	}

	parts := strings.Split(bk.StartTime, ":")
	if len(parts) != 2 {
		return time.Time{}, time.Time{}
	}
	var h, m int
	fmt.Sscanf(parts[0], "%d", &h)
	fmt.Sscanf(parts[1], "%d", &m)

	start := time.Date(date.Year(), date.Month(), date.Day(), h, m, 0, 0, loc)

	duration := bk.DurationMinutes
	if bk.ResultDuration != nil && *bk.ResultDuration > 0 {
		duration = *bk.ResultDuration
	}
	end := start.Add(time.Duration(duration) * time.Minute)

	return start, end
}

func escapeIcal(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, ";", "\\;")
	s = strings.ReplaceAll(s, ",", "\\,")
	s = strings.ReplaceAll(s, "\n", "\\n")
	return s
}
