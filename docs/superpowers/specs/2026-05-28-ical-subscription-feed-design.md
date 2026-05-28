# iCal Subscription Feed — Design Spec

## Goal

Expose a subscribable iCal (.ics) feed so that calendar apps (Apple Calendar, Google Calendar, etc.) can show all of Klara's room bookings — both those created by the automation and those booked manually on Asimut.

## Requirements

1. Calendar subscription URL with a secret token for authentication
2. Include all automation bookings (full history from DB)
3. Include manual bookings from Asimut (next 4 weeks, synced hourly)
4. Differentiate booking status via emoji prefix in event title:
   - `✅` — succeeded (`booked` / `partially_booked`)
   - `❌` — failed
   - `⏳` — pending / scheduled
   - `📅` — manual (from Asimut directly)
5. Deduplicate: when an automation booking also appears in Asimut sync, suppress the Asimut copy

## Architecture

### New Endpoint

`GET /api/calendar/{token}.ics`

- No auth middleware — the token in the URL path IS the authentication
- Token is configured via `CALENDAR_TOKEN` environment variable
- Returns `Content-Type: text/calendar; charset=utf-8`
- Returns a valid RFC 5545 iCalendar document

### Data Sources

#### 1. Automation Bookings (from SQLite `booking_wishes` table)

All bookings are included regardless of age. Each booking wish maps to one VEVENT:

- **UID**: `booking-{id}@asimut-automation`
- **DTSTART/DTEND**: derived from `date` + `start_time` + `duration_minutes` (for pending/failed) or `result_duration` (for booked)
- **SUMMARY**: `{emoji} {result_room or "Room TBD"}`
- **DESCRIPTION**: status details, failure reason if applicable
- **LOCATION**: result_room (if booked)

Status-to-emoji mapping:
| Status | Emoji | Meaning |
|--------|-------|---------|
| `pending`, `scheduled`, `executing` | ⏳ | Awaiting execution |
| `booked` | ✅ | Full duration achieved |
| `partially_booked` | ✅ | Partial success (noted in description) |
| `failed` | ❌ | Booking failed |

#### 2. Manual Asimut Bookings (synced from Asimut API)

Fetched from Asimut and cached in a new SQLite table. Synced every hour via background goroutine.

### Asimut Event Sync

#### API Discovery

The Asimut API doesn't have a documented "list my events" endpoint in the current reference. Candidate approaches to discover/use:

1. **`GET /services/v2/events`** with date range parameters — most likely candidate, common REST pattern
2. **`GET /services/v2/arrangement/event_id=...;direction=forward`** — the arrangement endpoint might list user events
3. **`GET /services/v2/quota/date=...`** — the quota endpoint might return event IDs alongside quota info

Implementation plan: try approach 1 first. If the endpoint doesn't exist, try variations (`/services/v2/events/person_id={user_id}`, `/services/v2/myevents`, etc.). If none work, fall back to scraping the location schedule for rooms in the allowed list and filtering by the logged-in user's person ID (965).

#### New Table: `asimut_events`

```sql
CREATE TABLE IF NOT EXISTS asimut_events (
    event_id    INTEGER PRIMARY KEY,
    title       TEXT NOT NULL,
    room_name   TEXT NOT NULL,
    start_time  TEXT NOT NULL,  -- ISO 8601
    end_time    TEXT NOT NULL,  -- ISO 8601
    last_synced TEXT NOT NULL   -- ISO 8601
);
```

#### Sync Logic (background goroutine)

- Runs every 60 minutes
- Fetches events for the next 4 weeks from Asimut
- Upserts into `asimut_events` table (insert or replace by event_id)
- Deletes events from `asimut_events` that are no longer returned by Asimut (cancelled)
- Requires a successful login; skips sync cycle if login fails (logs warning)

#### Deduplication

When generating the iCal feed, Asimut events are excluded if there exists a `booking_wishes` row with:
- `status` IN (`booked`, `partially_booked`)
- Same date (comparing `booking_wishes.date` with the date portion of `asimut_events.start_time`)
- Overlapping time range (start times within 5 minutes AND same room)

This prevents showing both "✅ Room 101" and "📅 Room 101" for the same booking.

### iCal Feed Generation

Generated on each request (no caching of the .ics file itself — the data is already in SQLite so it's fast).

#### VCALENDAR Properties

```
BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//Asimut Automation//Booking Calendar//EN
CALSCALE:GREGORIAN
METHOD:PUBLISH
X-WR-CALNAME:Practice Room Bookings
X-WR-TIMEZONE:Europe/Berlin
REFRESH-INTERVAL;VALUE=DURATION:PT1H
```

#### VEVENT Structure (automation bookings)

```
BEGIN:VEVENT
UID:booking-{id}@asimut-automation
DTSTART:{date}T{start_time}00
DTEND:{date}T{computed_end}00
SUMMARY:{emoji} {room_name}
DESCRIPTION:{status_detail}
LOCATION:{room_name}
LAST-MODIFIED:{updated_at}
END:VEVENT
```

#### VEVENT Structure (manual Asimut bookings)

```
BEGIN:VEVENT
UID:asimut-{event_id}@asimut-automation
DTSTART:{start_time}
DTEND:{end_time}
SUMMARY:📅 {room_name}
DESCRIPTION:Manual booking from Asimut
LOCATION:{room_name}
END:VEVENT
```

### Configuration

New environment variable:

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `CALENDAR_TOKEN` | No | (empty = feature disabled) | Secret token for calendar subscription URL |

If `CALENDAR_TOKEN` is empty/unset, the calendar endpoint returns 404 (feature disabled). This keeps the feature opt-in and avoids breaking existing deployments.

### Routing

The endpoint is registered outside the auth middleware group (since it has its own token-in-URL auth):

```go
r.Get("/api/calendar/{token}.ics", calendarHandler)
```

The handler:
1. Extracts `{token}` from URL
2. Compares (constant-time) against `CALENDAR_TOKEN`
3. If mismatch → 404 (not 401, to avoid leaking that the endpoint exists)
4. If match → generate and return .ics content

### File Structure (new files)

```
backend/
├── api/
│   └── calendar.go          # HTTP handler for /api/calendar/{token}.ics
├── db/
│   └── asimut_events.go     # AsimutEvent model + CRUD for the cache table
├── ical/
│   └── generator.go         # iCal generation logic (VCALENDAR/VEVENT formatting)
└── sync/
    └── events.go            # Background goroutine for Asimut event sync
```

### Go Dependencies

- No external iCal library needed — RFC 5545 format is simple enough to generate with `fmt.Sprintf` / `strings.Builder`
- Timezone handling: use `time.LoadLocation("Europe/Berlin")` for DTSTART/DTEND

### Frontend Changes

None required for MVP. The calendar URL can be shared manually. Optionally, a future enhancement could show the subscription URL in the settings page.

### Room Name Resolution

Pending/scheduled bookings only have `room_priorities` (a list of room IDs), not a room name. The Asimut event sync already fetches room names from the API. To resolve IDs to names for pending bookings:

- During the hourly sync, also refresh and cache the room list (from `GetLocations()`) in a simple `rooms_cache` table (room_id → name)
- For pending bookings: look up the first priority room ID in the cache. If not found, use "Pending"

```sql
CREATE TABLE IF NOT EXISTS rooms_cache (
    room_id   INTEGER PRIMARY KEY,
    name      TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
```

## Edge Cases

1. **Asimut login fails during sync**: Log warning, skip this sync cycle, retry next hour
2. **Booking wish has no result_room yet (pending)**: Use first room from `room_priorities` resolved via `rooms_cache`, fallback to "Pending"
3. **Calendar app requests very frequently**: No issue — SQLite query is fast, no rate limiting needed
4. **Time zones**: All times stored in Europe/Berlin; iCal DTSTART/DTEND include timezone via TZID

## Testing Strategy

1. Unit tests for iCal generation (verify RFC 5545 compliance)
2. Unit tests for deduplication logic
3. Integration test for the HTTP endpoint (token validation, content-type, valid .ics output)
4. Manual test: subscribe in Apple Calendar, verify events appear correctly
