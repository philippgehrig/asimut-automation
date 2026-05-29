package main

import (
	"log"
	"net/http"
	"time"

	"github.com/philippgehrig/asimuth-automation/backend/api"
	"github.com/philippgehrig/asimuth-automation/backend/asimut"
	"github.com/philippgehrig/asimuth-automation/backend/config"
	"github.com/philippgehrig/asimuth-automation/backend/db"
	"github.com/philippgehrig/asimuth-automation/backend/scheduler"
	"github.com/philippgehrig/asimuth-automation/backend/sync"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	database, err := db.New(cfg.DatabasePath)
	if err != nil {
		log.Fatalf("Failed to init DB: %v", err)
	}
	defer database.Close()

	asimutClient := asimut.NewClient("https://hfm-freiburg.asimut.net", cfg.AsimutEmail, cfg.AsimutPassword)

	sync.StartEventSync(database, asimutClient)

	sched := scheduler.New()
	sched.Start()

	srv := api.NewServer(database, asimutClient, sched, cfg.AppPassword, cfg.CalendarToken)

	rescheduleExisting(database, srv)
	go generateRecurrences(database, srv)

	log.Printf("Starting server on :%s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, srv.Router()); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

func rescheduleExisting(database *db.DB, srv *api.Server) {
	bookings, err := database.GetPendingBookings()
	if err != nil {
		log.Printf("Error loading pending bookings: %v", err)
		return
	}
	for _, b := range bookings {
		srv.ScheduleBookingJob(b.ID, b)
	}
	log.Printf("Rescheduled %d pending bookings", len(bookings))
}

func generateRecurrences(database *db.DB, srv *api.Server) {
	for {
		recurrences, err := database.GetActiveRecurrences()
		if err != nil {
			log.Printf("Error loading recurrences: %v", err)
			time.Sleep(1 * time.Hour)
			continue
		}

		loc, err := time.LoadLocation("Europe/Berlin")
		if err != nil {
			log.Printf("Error loading timezone Europe/Berlin: %v, falling back to UTC", err)
			loc = time.UTC
		}
		now := time.Now().In(loc)

		for _, r := range recurrences {
			for weeksAhead := 0; weeksAhead < 4; weeksAhead++ {
				// DayOfWeek uses 0=Mon..6=Sun. Go's time.Weekday uses 0=Sun..6=Sat.
				// Convert: Mon(0)->time.Monday(1), Tue(1)->time.Tuesday(2), ..., Sun(6)->time.Sunday(0)
				target := nextWeekday(now, time.Weekday((r.DayOfWeek+1)%7), weeksAhead)
				dateStr := target.Format("2006-01-02")

				// Skip slots that have already started
				hm, err := scheduler.ParseTime(r.StartTime)
				if err != nil {
					continue
				}
				slotStart := time.Date(target.Year(), target.Month(), target.Day(), hm[0], hm[1], 0, 0, loc)
				if slotStart.Before(now) {
					continue
				}

				existing, err := database.GetBookingByRecurrenceAndDate(r.ID, dateStr)
				if err != nil {
					log.Printf("Error checking existing booking for recurrence %s date %s: %v", r.ID, dateStr, err)
					continue
				}
				if existing != nil {
					continue
				}

				wish := db.BookingWish{
					Date:            dateStr,
					StartTime:       r.StartTime,
					DurationMinutes: r.DurationMinutes,
					RoomPriorities:  r.RoomPriorities,
					RecurrenceID:    r.ID,
				}
				id, err := database.CreateBooking(wish)
				if err != nil {
					log.Printf("Error creating recurrence booking: %v", err)
					continue
				}
				srv.ScheduleBookingJob(id, wish)
			}
		}

		time.Sleep(1 * time.Hour)
	}
}

func nextWeekday(from time.Time, weekday time.Weekday, weeksAhead int) time.Time {
	daysUntil := int(weekday) - int(from.Weekday())
	if daysUntil < 0 {
		daysUntil += 7
	}
	target := from.AddDate(0, 0, daysUntil+weeksAhead*7)
	return time.Date(target.Year(), target.Month(), target.Day(), 0, 0, 0, 0, target.Location())
}
