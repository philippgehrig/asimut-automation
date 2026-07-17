//go:build integration

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/philippgehrig/asimuth-automation/backend/api"
	"github.com/philippgehrig/asimuth-automation/backend/asimut"
	"github.com/philippgehrig/asimuth-automation/backend/db"
	"github.com/philippgehrig/asimuth-automation/backend/scheduler"
)

func TestFullBookingFlow(t *testing.T) {
	// Use a date 5 days in the future so it's always in the future
	futureDate := time.Now().AddDate(0, 0, 5).Format("2006-01-02")

	// 1. Set up mock Asimut server
	mockAsimut := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/public/login.php":
			http.SetCookie(w, &http.Cookie{Name: "PHPSESSID", Value: "test"})
			w.Header().Set("Location", "/public/hfm-freiburg.asimut.net")
			w.WriteHeader(302)
		case r.URL.Path == "/services/v2/locations":
			w.Write([]byte(`{"response":{"locations":[{"id":114,"name":"MBP-326","secondary_name":"Test Room","bookable":true,"type":"location"}]}}`))
		case r.URL.Path == "/services/v2/eventdefault":
			w.Write([]byte(fmt.Sprintf(`{"response":{"eventdefault":{"events":[{"id":0,"ar":"Einzelüben","ca":1,"st":"%sT14:30:00+02:00","en":"%sT15:00:00+02:00","rs":[{"id":114,"dn":"MBP-326"}],"pe":[{"id":965,"ro":1,"dn":"Teiln: Test User ([BM]Ob956)"}],"ps":[{"me":false,"ri":1,"rs":"T","rh":"T","rc":1,"bo":[{"id":965,"fn":"Test","ln":"User","un":"t"}]}],"ri":{"e":true},"vi":"visible","cl":[]}]},"success":true}}`, futureDate, futureDate)))
		case r.URL.Path == "/services/v2/event/type=check":
			w.Write([]byte(`{"response":{"success":true,"event_ids":[0]}}`))
		case r.URL.Path == "/services/v2/event/type=save":
			w.Write([]byte(`{"response":{"success":true,"event_ids":[99999]}}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer mockAsimut.Close()

	// 2. Set up real DB, scheduler, and server
	database, err := db.New(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	asimutClient := asimut.NewClient(mockAsimut.URL, "test@test.com", "pass")
	sched := scheduler.New()
	sched.Start()
	defer sched.Stop()

	srv := api.NewServer(database, asimutClient, sched, "testpass", "testcaltoken")
	router := srv.Router()

	// 3. Create a booking via API
	body, _ := json.Marshal(map[string]interface{}{
		"date":             futureDate,
		"start_time":      "14:30",
		"duration_minutes": 30,
		"room_priorities":  []int{114},
	})

	req := httptest.NewRequest("POST", "/api/bookings", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer testpass")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("create booking: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// 4. Verify booking exists
	req = httptest.NewRequest("GET", "/api/bookings", nil)
	req.Header.Set("Authorization", "Bearer testpass")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var bookings []db.BookingWish
	json.NewDecoder(w.Body).Decode(&bookings)
	if len(bookings) != 1 {
		t.Fatalf("expected 1 booking, got %d", len(bookings))
	}
	if bookings[0].Status != "scheduled" && bookings[0].Status != "pending" {
		t.Fatalf("expected pending/scheduled status, got %s", bookings[0].Status)
	}

	// 5. Verify rooms endpoint works (login first)
	if err := asimutClient.Login(); err != nil {
		t.Fatalf("asimut login: %v", err)
	}
	req = httptest.NewRequest("GET", "/api/rooms", nil)
	req.Header.Set("Authorization", "Bearer testpass")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("rooms: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// 6. Verify delete works
	deleteReq := httptest.NewRequest("DELETE", "/api/bookings/"+bookings[0].ID, nil)
	deleteReq.Header.Set("Authorization", "Bearer testpass")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, deleteReq)

	if w.Code != 204 {
		t.Fatalf("delete: expected 204, got %d", w.Code)
	}
}
