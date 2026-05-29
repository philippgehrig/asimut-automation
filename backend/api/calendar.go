package api

import (
	"crypto/subtle"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/philippgehrig/asimuth-automation/backend/db"
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
