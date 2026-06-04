# Asimut v2 API Reference

Base URL: `https://hfm-freiburg.asimut.net`

## Authentication

**POST** `/public/login.php`

Form-encoded body:
```
authenticate-url=/public/hfm-freiburg.asimut.net
authenticate-useraccount=<email>
authenticate-password=<password>
authenticate-verification=ok
```

Response: HTTP 302 redirect. Sets `PHPSESSID` and `date` cookies.
All subsequent requests use cookie-based auth (send cookies from jar).

---

## Known Endpoints

All endpoints use path: `/services/v2/<resource>/<filters>` where filters are semicolon-separated `key=value` pairs.

### Heartbeat (Session Check)

**GET** `/services/v2/heartbeat/me`

Returns session state and user info.

Response:
```json
{
  "response": {
    "heartbeat": {
      "loggedin": true,
      "me": {
        "id": 965,
        "name": "Klara",
        "surname": "Gehrig",
        "username": "[BM]Ob956",
        "email": "K.Gehrig@mh-freiburg.de",
        "booking_horizon": "2026-06-06T11:06:00+02:00",
        "maximum_booking_length": 180,
        "minimum_booking_gap": 60,
        "minimum_booking_length": 30,
        "authentication": "Autonomous",
        "visible_horizon": "2026-10-19T00:00:00+02:00"
      }
    }
  }
}
```

Key fields:
- `me.id` — User ID (needed for some API calls)
- `me.booking_horizon` — Latest datetime the user can book into
- `me.maximum_booking_length` — Max booking duration in minutes
- `me.minimum_booking_gap` — Min gap between consecutive bookings in minutes

---

### Locations (Rooms)

**GET** `/services/v2/locations`

Returns all rooms/locations.

Response:
```json
{
  "response": {
    "locations": [
      {
        "id": 52,
        "name": "MBP-101",
        "secondary_name": "Seminar, Hauptgebäude, 56 m²",
        "bookable": true,
        "type": "location"
      }
    ]
  }
}
```

---

### Location Groups

**GET** `/services/v2/locationgroups`

Returns groups that locations belong to.

---

### Single Event

**GET** `/services/v2/event/event_id=<id>`

Returns a single event by ID.

Response:
```json
{
  "response": {
    "event": {
      "id": 487960,
      "ar": "Einzelüben",
      "st": "2026-06-04T15:00:00+02:00",
      "en": "2026-06-04T18:00:00+02:00",
      "rs": [{"id": 52, "dn": "MBP-101 (Seminar, Hauptgebäude, 56 m²)"}],
      "pe": [{"id": 965, "ro": 1, "dn": "Teiln: Klara Gehrig ([BM]Ob956)"}]
    }
  }
}
```

Event field key:
- `id` — Event ID
- `ar` — Activity/title (e.g., "Einzelüben")
- `st` — Start time (ISO 8601 with timezone)
- `en` — End time (ISO 8601 with timezone)
- `rs` — Rooms array: `id` (room ID), `dn` (display name with description)
- `pe` — Participants array
- `ca` — Category ID
- `ev` — Event description
- `de` — Description

---

### Search Events (User's Bookings)

**POST** `/services/v2/search/type=events;load_from=<ISO>;direction=<dir>;is_participating=<bool>;may_signup=<bool>;limit=<n>`

This is the **only working way** to fetch a user's events. There is no dedicated "list my events" endpoint.

Path parameters (semicolon-separated):
- `load_from` — ISO 8601 timestamp (e.g., `2026-06-04T00:00:00.000+02:00`)
- `direction` — `"forward"` or `"backward"`
- `is_participating` — `"true"` to filter to user's own events
- `may_signup` — `"true"` to include sign-up events
- `limit` — Max events to return (e.g., `50`, `100`)

Request body (JSON, **must not be empty string**):
```json
{"search": "Einzelüben"}
```

The `search` field filters by event title. An empty string `""` causes HTTP 400. Use `"Einzelüben"` for practice room bookings.

Response:
```json
{
  "response": {
    "events": {
      "2026-06-04T00:00:00+02:00": [
        {
          "dateHeader": "Mi, 4. Juni 2026",
          "events": [
            {
              "id": 487960,
              "ar": "Einzelüben",
              "st": "2026-06-04T15:00:00+02:00",
              "en": "2026-06-04T18:00:00+02:00",
              "rs": [{"id": 52, "dn": "MBP-101 (Seminar, Hauptgebäude, 56 m²)"}],
              "pe": [{"id": 965, "ro": 1, "dn": "Teiln: Klara Gehrig ([BM]Ob956)"}]
            }
          ]
        }
      ]
    },
    "pagination": {
      "first_event_date": "2026-06-04T00:00:00+02:00",
      "last_event_date": "2026-06-12T00:00:00+02:00",
      "event_count": 4,
      "horizon_reached": false
    },
    "success": true
  }
}
```

Structure: `events` is a **map** keyed by date ISO strings. Each date contains an array of groups, each group has a `dateHeader` and `events` array.

Pagination: when `event_count >= limit`, there are more events — call again with `load_from` set to `last_event_date`.

---

### Search (Autocomplete)

**POST** `/services/v2/search/type=all`

Body: `{"search": "<query>"}`

Returns autocomplete suggestions across categories:
- `events` — Events matching query in title
- `person-agenda` — People matching query (id = user ID)
- `location-agenda` — Rooms matching query (id = room ID)

---

### Event Default (Booking Template)

**POST** `/services/v2/eventdefault`

Body:
```json
{
  "st": "2026-06-04T15:00:00.000+02:00",
  "ca": 1,
  "rs": [{"id": 52}]
}
```

Returns a pre-filled event template for creating a booking.

---

### Create Booking

**POST** `/services/v2/event/type=check`

Validates a booking before saving. Body: `{"event": <event_data>}`

**POST** `/services/v2/event/type=save`

Creates the booking. Body: `{"event": <event_data>}`

Response includes `event_ids` array with the created event ID(s).

---

### Modify Booking

**PATCH** `/services/v2/event/event_id=<id>;type=check`

Validates modification. Body: `{"event": <modified_event>}`

**PATCH** `/services/v2/event/event_id=<id>;type=save`

Saves modification.

---

### End Booking Now

**PATCH** `/services/v2/event/event_id=<id>;type=end_now`

Ends an in-progress booking immediately.

---

### Quota

**GET** `/services/v2/quota/date=<YYYY-MM-DD>`

Returns booking quota info:
```json
{
  "response": {
    "quota_overview": {
      "booking_quota": 18000,
      "booking_quota_d": 7200,
      "booking_quota_p": 3600
    }
  }
}
```

Values are in seconds. `booking_quota` = total/term, `booking_quota_d` = daily, `booking_quota_p` = already used today.

---

## Endpoints That Do NOT Exist

These were tested and return "Unknown resource requested" (501):
- `/services/v2/events/...` (any variation)
- `/services/v2/schedule/...`
- `/services/v2/overview/...`
- `/services/v2/bookings/...`
- `/services/v2/myevents/...`
- `/services/v2/mybookings/...`
- `/services/v2/calendar/...`
- `/services/v2/locationevents/...`
- `/services/v2/participantevents/...`
- `/services/v2/userevents/...`
- `/services/v2/timetable/...`
- `/services/v2/grid/...`

---

## Complete Endpoint List (from JS Bundle)

Extracted from the frontend Angular app (`main.*.js`):

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/services/v2/heartbeat/me` | GET | Session check + user info |
| `/services/v2/locations` | GET | All rooms |
| `/services/v2/locationgroups` | GET | Room groups |
| `/services/v2/categories` | GET | Event categories |
| `/services/v2/roles` | GET | Participant roles |
| `/services/v2/event/event_id=<id>` | GET | Single event |
| `/services/v2/event/type=check` | POST | Validate new booking |
| `/services/v2/event/type=save` | POST | Create booking |
| `/services/v2/event/event_id=<id>;type=check` | PATCH | Validate modification |
| `/services/v2/event/event_id=<id>;type=save` | PATCH | Save modification |
| `/services/v2/event/event_id=<id>;type=end_now` | PATCH | End booking now |
| `/services/v2/event/event_id=<id>;type=reconfirm` | POST | Reconfirm event |
| `/services/v2/eventdefault` | POST | Booking template |
| `/services/v2/eventgroups/category_id=<id>` | GET | Event groups by category |
| `/services/v2/eventgroups/group_id=<id>` | GET | Single event group |
| `/services/v2/eventsignup/event_signup_id=<id>` | PATCH | Modify event signup |
| `/services/v2/search/type=all` | POST | Autocomplete search |
| `/services/v2/search/type=events;...` | POST | Event search (see above) |
| `/services/v2/attendance/type=event-mark-all;eventId=<id>` | POST | Mark attendance |
| `/services/v2/quota/date=<date>` | GET | Booking quota |
| `/services/v2/usersettings/...` | GET/PATCH | User preferences |
| `/services/v2/menu/type=main` | GET | Navigation menu |
| `/services/v2/runtimetranslations` | GET | UI translations |
| `/services/v2/login` | GET | SSO login |
| `/services/v2/logout` | POST | Logout |
| `/services/v2/bookingrules` | GET | Booking rules |
| `/services/v2/debug` | GET | Debug info |
| `/services/v2/logperformance/error` | POST | Error reporting |
| `/services/v2/resetpassword/...` | POST | Password reset |

---

## Time Formats

The API uses two ISO 8601 formats:
- With milliseconds: `2026-06-04T15:00:00.000+02:00` (used in requests, eventdefault)
- Without milliseconds: `2026-06-04T15:00:00+02:00` (used in search responses)

Both use the Europe/Berlin timezone offset (`+01:00` winter, `+02:00` summer).

---

## Notes

- This API is undocumented and was reverse-engineered from the Angular frontend.
- The `Autonomous` authentication type means self-service booking (no instructor).
- `disable_participant_agenda_link: true` in heartbeat indicates limited user permissions.
- `may_access_advanced_interface: false` restricts access to admin features.
- Room `dn` (display name) format: `"MBP-101 (Seminar, Hauptgebäude, 56 m²)"` — room name is before the first ` (`.
