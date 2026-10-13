package asimut

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPlainText(t *testing.T) {
	got := plainText("Ihre Buchung überschneidet sich mit:<ul><li>Einzelüben</li></ul>")
	if got != "Ihre Buchung überschneidet sich mit:Einzelüben" {
		t.Errorf("unexpected plain text: %q", got)
	}
}

func newDiagServer(t *testing.T, onRoute func(w http.ResponseWriter, r *http.Request) bool) *Client {
	t.Helper()
	logged := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/public/login.php":
			http.SetCookie(w, &http.Cookie{Name: "PHPSESSID", Value: "x"})
			w.WriteHeader(http.StatusFound)
		case r.URL.Path == "/services/v2/eventdefault" && !logged:
			logged = true
			w.Write([]byte(`{"response":{"success":true,"eventdefault":{"events":[{"pe":[{"id":965,"ro":1,"dn":"Teiln: A B (x)"}]}]}}}`))
		case onRoute(w, r):
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	c := NewClient(srv.URL, "a@b.c", "pw")
	if err := c.Login(); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestBookRoom_CheckRejectedReturnsBookingError(t *testing.T) {
	c := newDiagServer(t, func(w http.ResponseWriter, r *http.Request) bool {
		switch r.URL.Path {
		case "/services/v2/eventdefault":
			w.Write([]byte(`{"response":{"eventdefault":{"events":[{"st":"2026-10-15T10:00:00.000+02:00","en":"2026-10-15T11:00:00.000+02:00"}]}}}`))
		case "/services/v2/event/type=check":
			w.Write([]byte(`{"response":{"success":false,"bookingrules":{"issues":[{"type":"location","class":"message-warning","text":"Ihre Buchung überschneidet sich mit:<ul><li>Einzelüben</li></ul>"},{"type":"general","class":"message-info","text":"vorläufig"}]}}}`))
		default:
			return false
		}
		return true
	})

	start := time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)
	_, err := c.BookRoom(42, start, start.Add(30*time.Minute))
	var be *BookingError
	if !errors.As(err, &be) {
		t.Fatalf("expected *BookingError, got %T: %v", err, err)
	}
	if be.Stage != "check" || len(be.Issues) != 2 || !be.HasIssueType(IssueTypeLocation) {
		t.Errorf("unexpected booking error: %+v", be)
	}
	if !strings.Contains(err.Error(), "überschneidet") {
		t.Errorf("Error() should keep the first issue text, got %q", err.Error())
	}
}

func TestGetRoomEvents_FiltersOverlapping(t *testing.T) {
	c := newDiagServer(t, func(w http.ResponseWriter, r *http.Request) bool {
		if !strings.HasPrefix(r.URL.Path, "/services/v2/agenda/") {
			return false
		}
		if !strings.Contains(r.URL.Path, "location_ids=114;") {
			t.Errorf("room id missing in agenda path: %s", r.URL.Path)
		}
		ev := func(id int, st, en string) map[string]interface{} {
			return map[string]interface{}{"id": id, "ar": "Einzelüben", "st": st, "en": en,
				"rs": []interface{}{map[string]interface{}{"dn": "MBP-326 (Klarinette)", "id": 114}}}
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"response": map[string]interface{}{"agenda": map[string]interface{}{
			"2026-10-15T00:00:00+02:00": []interface{}{map[string]interface{}{"events": []interface{}{
				ev(1, "2026-10-15T09:00:00+02:00", "2026-10-15T10:15:00+02:00"), // overlaps
				ev(2, "2026-10-15T08:00:00+02:00", "2026-10-15T10:00:00+02:00"), // ends exactly at start
				ev(3, "2026-10-15T10:30:00+02:00", "2026-10-15T11:00:00+02:00"), // starts at end
			}}},
		}}})
		return true
	})

	from := time.Date(2026, 10, 15, 10, 0, 0, 0, time.FixedZone("CEST", 2*3600))
	got, err := c.GetRoomEvents(114, from, from.Add(30*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != 1 || got[0].RoomName != "MBP-326" {
		t.Errorf("unexpected overlapping events: %+v", got)
	}
}
