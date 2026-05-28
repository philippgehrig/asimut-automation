# iCal Subscription Feed Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Expose a subscribable iCal (.ics) feed at `/api/calendar/{token}.ics` showing all room bookings (automated + manual from Asimut) with status-based emoji prefixes.

**Architecture:** New HTTP endpoint outside the auth middleware group, using a secret URL token. Background goroutine syncs manual events from Asimut hourly into a cache table. iCal is generated on-demand from SQLite data.

**Tech Stack:** Go (chi router, database/sql + SQLite), RFC 5545 iCal format (no external library), Europe/Berlin timezone.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `backend/config/config.go` | Add `CalendarToken` field (env: `CALENDAR_TOKEN`) |
| `backend/db/sqlite.go` | Add `asimut_events` + `rooms_cache` tables to migration |
| `backend/db/asimut_events.go` | `AsimutEvent` model + CRUD (upsert, list, delete stale) |
| `backend/db/rooms_cache.go` | `CachedRoom` model + CRUD (upsert, lookup by ID) |
| `backend/asimut/client.go` | Add `GetMyEvents(from, to)` method |
| `backend/sync/events.go` | Background goroutine: sync Asimut events + rooms cache |
| `backend/ical/generator.go` | Generate RFC 5545 VCALENDAR from booking data |
| `backend/api/calendar.go` | HTTP handler for `/api/calendar/{token}.ics` |
| `backend/api/router.go` | Register calendar route outside auth group |
| `backend/main.go` | Start sync goroutine, pass `CalendarToken` to Server |
| `docker-compose.yml` | Add `CALENDAR_TOKEN` env var |

---

### Task 1: Configuration — Add CALENDAR_TOKEN

**Files:**
- Modify: `backend/config/config.go`
- Modify: `docker-compose.yml`

- [ ] **Step 1: Add CalendarToken to config struct and Load()**

In `backend/config/config.go`, add the field to the struct and load it from env:

```go
type Config struct {
	AsimutEmail    string
	AsimutPassword string
	AppPassword    string
	CalendarToken  string
	DatabasePath   string
	Port           string
}

func Load() (*Config, error) {
	cfg := &Config{
		AsimutEmail:    os.Getenv("ASIMUT_EMAIL"),
		AsimutPassword: os.Getenv("ASIMUT_PASSWORD"),
		AppPassword:    os.Getenv("APP_PASSWORD"),
		CalendarToken:  os.Getenv("CALENDAR_TOKEN"),
		DatabasePath:   getEnvOrDefault("DATABASE_PATH", "/data/asimut.db"),
		Port:           getEnvOrDefault("PORT", "8080"),
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}
```

No validation for `CalendarToken` — empty means feature disabled.

- [ ] **Step 2: Add env var to docker-compose.yml**

Add to backend service environment:

```yaml
    environment:
      - ASIMUT_EMAIL=${ASIMUT_EMAIL}
      - ASIMUT_PASSWORD=${ASIMUT_PASSWORD}
      - APP_PASSWORD=${APP_PASSWORD}
      - CALENDAR_TOKEN=${CALENDAR_TOKEN}
      - DATABASE_PATH=/data/asimut.db
```

- [ ] **Step 3: Commit**

```bash
git add backend/config/config.go docker-compose.yml
git commit -m "feat(config): add CALENDAR_TOKEN environment variable"
```

---

### Task 2: Database — Add asimut_events and rooms_cache tables

**Files:**
- Modify: `backend/db/sqlite.go`
- Create: `backend/db/asimut_events.go`
- Create: `backend/db/rooms_cache.go`

- [ ] **Step 1: Add tables to migration in sqlite.go**

Append to the `schema` constant in `migrate()`:

```go
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
```

- [ ] **Step 2: Create backend/db/asimut_events.go**

```go
package db

import "fmt"

type AsimutEvent struct {
	EventID   int    `json:"event_id"`
	Title     string `json:"title"`
	RoomName  string `json:"room_name"`
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time"`
}

func (d *DB) UpsertAsimutEvent(e AsimutEvent) error {
	_, err := d.conn.Exec(`
		INSERT INTO asimut_events (event_id, title, room_name, start_time, end_time, last_synced)
		VALUES (?, ?, ?, ?, ?, datetime('now'))
		ON CONFLICT(event_id) DO UPDATE SET
			title = excluded.title,
			room_name = excluded.room_name,
			start_time = excluded.start_time,
			end_time = excluded.end_time,
			last_synced = datetime('now')`,
		e.EventID, e.Title, e.RoomName, e.StartTime, e.EndTime,
	)
	if err != nil {
		return fmt.Errorf("upsert asimut event: %w", err)
	}
	return nil
}

func (d *DB) ListAsimutEvents() ([]AsimutEvent, error) {
	rows, err := d.conn.Query(`
		SELECT event_id, title, room_name, start_time, end_time
		FROM asimut_events
		ORDER BY start_time`)
	if err != nil {
		return nil, fmt.Errorf("query asimut events: %w", err)
	}
	defer rows.Close()

	var events []AsimutEvent
	for rows.Next() {
		var e AsimutEvent
		if err := rows.Scan(&e.EventID, &e.Title, &e.RoomName, &e.StartTime, &e.EndTime); err != nil {
			return nil, fmt.Errorf("scan asimut event: %w", err)
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

func (d *DB) DeleteAsimutEventsNotIn(eventIDs []int) error {
	if len(eventIDs) == 0 {
		_, err := d.conn.Exec(`DELETE FROM asimut_events`)
		return err
	}

	query := `DELETE FROM asimut_events WHERE event_id NOT IN (`
	args := make([]interface{}, len(eventIDs))
	for i, id := range eventIDs {
		if i > 0 {
			query += ","
		}
		query += "?"
		args[i] = id
	}
	query += ")"

	_, err := d.conn.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("delete stale asimut events: %w", err)
	}
	return nil
}
```

- [ ] **Step 3: Create backend/db/rooms_cache.go**

```go
package db

import "fmt"

type CachedRoom struct {
	RoomID int    `json:"room_id"`
	Name   string `json:"name"`
}

func (d *DB) UpsertCachedRoom(roomID int, name string) error {
	_, err := d.conn.Exec(`
		INSERT INTO rooms_cache (room_id, name, updated_at)
		VALUES (?, ?, datetime('now'))
		ON CONFLICT(room_id) DO UPDATE SET
			name = excluded.name,
			updated_at = datetime('now')`,
		roomID, name,
	)
	if err != nil {
		return fmt.Errorf("upsert cached room: %w", err)
	}
	return nil
}

func (d *DB) GetCachedRoomName(roomID int) (string, error) {
	var name string
	err := d.conn.QueryRow(`SELECT name FROM rooms_cache WHERE room_id = ?`, roomID).Scan(&name)
	if err != nil {
		return "", fmt.Errorf("get cached room name: %w", err)
	}
	return name, nil
}
```

- [ ] **Step 4: Verify it compiles**

```bash
cd backend && go build ./...
```

- [ ] **Step 5: Commit**

```bash
git add backend/db/sqlite.go backend/db/asimut_events.go backend/db/rooms_cache.go
git commit -m "feat(db): add asimut_events and rooms_cache tables"
```

---

### Task 3: Asimut Client — Add GetMyEvents method

**Files:**
- Modify: `backend/asimut/client.go`

- [ ] **Step 1: Add GetMyEvents method**

This method needs API discovery. The most likely endpoint pattern for Asimut's v2 API is fetching events via `/services/v2/events` with query parameters. Based on the existing API patterns (semicolon-delimited params in URL path), try:

```go
// AsimutEventInfo represents an event fetched from the Asimut calendar.
type AsimutEventInfo struct {
	ID        int    `json:"id"`
	Title     string `json:"title"`
	RoomName  string `json:"room_name"`
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time"`
}

// GetMyEvents fetches the logged-in user's events between from and to.
// It queries the Asimut schedule API and returns parsed event info.
func (c *Client) GetMyEvents(from, to time.Time) ([]AsimutEventInfo, error) {
	fromStr := from.Format(timeFormat)
	toStr := to.Format(timeFormat)
	path := fmt.Sprintf("/services/v2/events/from=%s;to=%s", fromStr, toStr)

	respBody, err := c.doJSON("GET", path, nil)
	if err != nil {
		return nil, fmt.Errorf("getting events: %w", err)
	}

	response, ok := respBody["response"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected events response format")
	}

	eventsRaw, ok := response["events"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("no events array in response (keys: %v)", keys(response))
	}

	var events []AsimutEventInfo
	for _, er := range eventsRaw {
		em, ok := er.(map[string]interface{})
		if !ok {
			continue
		}

		roomName := ""
		if rs, ok := em["rs"].([]interface{}); ok && len(rs) > 0 {
			if rm, ok := rs[0].(map[string]interface{}); ok {
				roomName = stringFromInterface(rm["dn"])
				if name := stringFromInterface(rm["name"]); name != "" {
					roomName = name
				}
			}
		}

		events = append(events, AsimutEventInfo{
			ID:        intFromInterface(em["id"]),
			Title:     stringFromInterface(em["ar"]),
			RoomName:  roomName,
			StartTime: stringFromInterface(em["st"]),
			EndTime:   stringFromInterface(em["en"]),
		})
	}

	return events, nil
}
```

> **NOTE:** The exact endpoint path (`/services/v2/events/from=...;to=...`) is a best guess based on API conventions observed in the codebase. If it returns an error at runtime, try these alternatives in order:
> 1. `GET /services/v2/events?from={from}&to={to}`
> 2. `GET /services/v2/myevents/from={from};to={to}`
> 3. `GET /services/v2/calendar/from={from};to={to}`
>
> Log the response body when it fails to understand the API's error format. This will need manual testing against the live Asimut instance.

- [ ] **Step 2: Verify it compiles**

```bash
cd backend && go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add backend/asimut/client.go
git commit -m "feat(asimut): add GetMyEvents method for fetching user events"
```

---

### Task 4: Background Sync — Asimut events + rooms cache

**Files:**
- Create: `backend/sync/events.go`
- Modify: `backend/main.go`

- [ ] **Step 1: Create backend/sync/events.go**

```go
package sync

import (
	"log"
	"time"

	"github.com/philippgehrig/asimuth-automation/backend/asimut"
	"github.com/philippgehrig/asimuth-automation/backend/db"
)

func StartEventSync(database *db.DB, client *asimut.Client) {
	go func() {
		for {
			syncEvents(database, client)
			syncRooms(database, client)
			time.Sleep(1 * time.Hour)
		}
	}()
}

func syncEvents(database *db.DB, client *asimut.Client) {
	if err := client.Login(); err != nil {
		log.Printf("[sync] login failed, skipping event sync: %v", err)
		return
	}

	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		log.Printf("[sync] timezone error: %v", err)
		return
	}

	now := time.Now().In(loc)
	from := now
	to := now.AddDate(0, 0, 28) // 4 weeks ahead

	events, err := client.GetMyEvents(from, to)
	if err != nil {
		log.Printf("[sync] failed to fetch events from Asimut: %v", err)
		return
	}

	var syncedIDs []int
	for _, e := range events {
		syncedIDs = append(syncedIDs, e.ID)
		if err := database.UpsertAsimutEvent(db.AsimutEvent{
			EventID:   e.ID,
			Title:     e.Title,
			RoomName:  e.RoomName,
			StartTime: e.StartTime,
			EndTime:   e.EndTime,
		}); err != nil {
			log.Printf("[sync] failed to upsert event %d: %v", e.ID, err)
		}
	}

	if err := database.DeleteAsimutEventsNotIn(syncedIDs); err != nil {
		log.Printf("[sync] failed to clean stale events: %v", err)
	}

	log.Printf("[sync] synced %d events from Asimut", len(events))
}

func syncRooms(database *db.DB, client *asimut.Client) {
	locations, err := client.GetLocations()
	if err != nil {
		log.Printf("[sync] failed to fetch locations: %v", err)
		return
	}

	count := 0
	for _, loc := range locations {
		if loc.Type != "location" || !loc.Bookable {
			continue
		}
		if err := database.UpsertCachedRoom(loc.ID, loc.Name); err != nil {
			log.Printf("[sync] failed to cache room %d: %v", loc.ID, err)
			continue
		}
		count++
	}
	log.Printf("[sync] cached %d room names", count)
}
```

- [ ] **Step 2: Wire up in main.go**

Add import and call in `main()`, after the `asimutClient` is created:

```go
import (
	// ... existing imports ...
	"github.com/philippgehrig/asimuth-automation/backend/sync"
)

// In main(), after asimutClient creation:
sync.StartEventSync(database, asimutClient)
```

- [ ] **Step 3: Verify it compiles**

```bash
cd backend && go build ./...
```

- [ ] **Step 4: Commit**

```bash
git add backend/sync/events.go backend/main.go
git commit -m "feat(sync): add background goroutine for Asimut event + room sync"
```

---

### Task 5: iCal Generator

**Files:**
- Create: `backend/ical/generator.go`
- Create: `backend/ical/generator_test.go`

- [ ] **Step 1: Write the failing test**

Create `backend/ical/generator_test.go`:

```go
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
			ID:        "test-1",
			Date:      "2026-06-01",
			StartTime: "10:00",
			DurationMinutes: 60,
			Status:    "booked",
			ResultRoom: "MBP-326",
			UpdatedAt: "2026-06-01 09:00:00",
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

	if !strings.Contains(result, "SUMMARY:📅 MBP-110") {
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
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && go test ./ical/ -v
```

Expected: compilation error (package doesn't exist yet)

- [ ] **Step 3: Implement the generator**

Create `backend/ical/generator.go`:

```go
package ical

import (
	"fmt"
	"strings"
	"time"

	"github.com/philippgehrig/asimuth-automation/backend/db"
)

const asimutTimeFormat = "2006-01-02T15:04:05.000-07:00"
const icalTimeFormat = "20060102T150405"

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
	start, _ := time.Parse(asimutTimeFormat, ev.StartTime)
	end, _ := time.Parse(asimutTimeFormat, ev.EndTime)
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
		t, err := time.Parse(asimutTimeFormat, ev.StartTime)
		if err != nil {
			filtered = append(filtered, ev)
			continue
		}
		t = t.In(loc)
		key := bookingKey{
			date: t.Format("2006-01-02"),
			room: ev.RoomName,
			hour: t.Hour(),
			min:  t.Minute(),
		}
		// Check with 5-minute tolerance
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
		_ = key // used in tolerance loop above
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
```

- [ ] **Step 4: Run tests**

```bash
cd backend && go test ./ical/ -v
```

Expected: all 4 tests pass.

- [ ] **Step 5: Commit**

```bash
git add backend/ical/generator.go backend/ical/generator_test.go
git commit -m "feat(ical): add RFC 5545 calendar generator with deduplication"
```

---

### Task 6: HTTP Handler — Calendar endpoint

**Files:**
- Create: `backend/api/calendar.go`
- Modify: `backend/api/router.go`

- [ ] **Step 1: Create backend/api/calendar.go**

```go
package api

import (
	"crypto/subtle"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/philippgehrig/asimuth-automation/backend/ical"
)

func (s *Server) calendarHandler(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")

	if s.calendarToken == "" || subtle.ConstantTimeCompare([]byte(token), []byte(s.calendarToken)) != 1 {
		http.NotFound(w, r)
		return
	}

	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	bookings, err := s.db.ListBookings()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	asimutEvents, err := s.db.ListAsimutEvents()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	roomsCache := s.loadRoomsCache(bookings)

	content := ical.Generate(bookings, asimutEvents, roomsCache, loc)

	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", "inline; filename=\"bookings.ics\"")
	w.Write([]byte(content))
}

func (s *Server) loadRoomsCache(bookings []db.BookingWish) map[int]string {
	needIDs := make(map[int]bool)
	for _, bk := range bookings {
		if bk.ResultRoom != "" {
			continue
		}
		for _, id := range bk.RoomPriorities {
			needIDs[id] = true
		}
	}

	if len(needIDs) == 0 {
		return nil
	}

	cache := make(map[int]string)
	for id := range needIDs {
		name, err := s.db.GetCachedRoomName(id)
		if err == nil {
			cache[id] = name
		}
	}
	return cache
}
```

- [ ] **Step 2: Add calendarToken field to Server struct and update NewServer**

Modify `backend/api/router.go`:

```go
// Server holds the dependencies for all HTTP handlers.
type Server struct {
	db            *db.DB
	asimut        *asimut.Client
	scheduler     *scheduler.Scheduler
	password      string
	calendarToken string
}

// NewServer creates a new Server with all required dependencies.
func NewServer(database *db.DB, asimutClient *asimut.Client, sched *scheduler.Scheduler, password string, calendarToken string) *Server {
	return &Server{db: database, asimut: asimutClient, scheduler: sched, password: password, calendarToken: calendarToken}
}

// Router builds and returns the chi router with all API routes.
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// Calendar endpoint — outside auth middleware (token in URL)
	r.Get("/api/calendar/{token}.ics", s.calendarHandler)

	r.Route("/api", func(r chi.Router) {
		r.Use(AuthMiddleware(s.password))
		r.Get("/bookings", s.listBookings)
		r.Post("/bookings", s.createBooking)
		r.Delete("/bookings/{id}", s.deleteBooking)
		r.Get("/recurrences", s.listRecurrences)
		r.Post("/recurrences", s.createRecurrence)
		r.Patch("/recurrences/{id}", s.updateRecurrence)
		r.Delete("/recurrences/{id}", s.deleteRecurrence)
		r.Get("/rooms", s.listRooms)
		r.Get("/allowed-rooms", s.getAllowedRooms)
		r.Put("/allowed-rooms", s.setAllowedRooms)
		r.Get("/settings/status", s.getStatus)
		r.Post("/settings/reconnect", s.reconnect)
	})
	return r
}
```

- [ ] **Step 3: Update main.go to pass CalendarToken**

In `backend/main.go`, update the `NewServer` call:

```go
srv := api.NewServer(database, asimutClient, sched, cfg.AppPassword, cfg.CalendarToken)
```

- [ ] **Step 4: Add missing import in calendar.go**

Make sure `backend/api/calendar.go` imports the db package:

```go
import (
	"crypto/subtle"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/philippgehrig/asimuth-automation/backend/db"
	"github.com/philippgehrig/asimuth-automation/backend/ical"
)
```

- [ ] **Step 5: Verify it compiles**

```bash
cd backend && go build ./...
```

- [ ] **Step 6: Commit**

```bash
git add backend/api/calendar.go backend/api/router.go backend/main.go
git commit -m "feat(api): add /api/calendar/{token}.ics endpoint"
```

---

### Task 7: Integration Test — Calendar endpoint

**Files:**
- Create: `backend/api/calendar_test.go`

- [ ] **Step 1: Write the integration test**

Create `backend/api/calendar_test.go`:

```go
package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/philippgehrig/asimuth-automation/backend/asimut"
	"github.com/philippgehrig/asimuth-automation/backend/db"
	"github.com/philippgehrig/asimuth-automation/backend/scheduler"
)

func setupTestServer(t *testing.T) (*Server, *db.DB) {
	t.Helper()
	tmpFile := t.TempDir() + "/test.db"
	database, err := db.New(tmpFile)
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

	// Insert a test booking
	_, err := database.CreateBooking(db.BookingWish{
		Date:            "2026-06-01",
		StartTime:       "10:00",
		DurationMinutes: 60,
		RoomPriorities:  []int{114},
		Status:          "booked",
		ResultRoom:      "MBP-326",
	})
	if err != nil {
		t.Fatalf("failed to create booking: %v", err)
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
	tmpFile, _ := os.CreateTemp("", "test-*.db")
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	database, _ := db.New(tmpFile.Name())
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
```

- [ ] **Step 2: Run tests**

```bash
cd backend && go test ./api/ -v -run TestCalendar
```

Expected: all 3 tests pass.

- [ ] **Step 3: Commit**

```bash
git add backend/api/calendar_test.go
git commit -m "test(api): add integration tests for calendar endpoint"
```

---

### Task 8: Final Wiring — Verify full build and manual test

**Files:**
- None new — verification step

- [ ] **Step 1: Run all tests**

```bash
cd backend && go test ./... -v
```

All tests should pass.

- [ ] **Step 2: Verify docker-compose builds**

```bash
docker compose build backend
```

- [ ] **Step 3: Document the subscription URL**

The subscription URL format is:

```
https://<your-host>/api/calendar/<CALENDAR_TOKEN>.ics
```

Or if running locally via docker-compose with port forwarding:

```
http://localhost:3000/api/calendar/<CALENDAR_TOKEN>.ics
```

(nginx will proxy `/api/calendar/...` to the backend since it matches `/api`)

- [ ] **Step 4: Manual test with curl**

Start the stack with `CALENDAR_TOKEN=test123` set, then:

```bash
curl -s http://localhost:3000/api/calendar/test123.ics
```

Should return valid iCal content starting with `BEGIN:VCALENDAR`.

```bash
curl -s -o /dev/null -w "%{http_code}" http://localhost:3000/api/calendar/wrongtoken.ics
```

Should return `404`.
