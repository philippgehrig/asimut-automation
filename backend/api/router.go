package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/philippgehrig/asimuth-automation/backend/asimut"
	"github.com/philippgehrig/asimuth-automation/backend/db"
	"github.com/philippgehrig/asimuth-automation/backend/scheduler"
)

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

	r.Route("/api", func(r chi.Router) {
		// Calendar endpoint — no auth (token in URL provides authentication)
		r.Get("/calendar/{token}.ics", s.calendarHandler)

		r.Group(func(r chi.Router) {
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
	})
	return r
}

// writeJSON encodes v as JSON and writes it to the response.
func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
