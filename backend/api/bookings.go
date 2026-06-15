package api

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/philippgehrig/asimuth-automation/backend/db"
	"github.com/philippgehrig/asimuth-automation/backend/scheduler"
)

func (s *Server) listBookings(w http.ResponseWriter, r *http.Request) {
	bookings, err := s.db.ListBookings()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if bookings == nil {
		bookings = []db.BookingWish{}
	}
	writeJSON(w, bookings)
}

func (s *Server) createBooking(w http.ResponseWriter, r *http.Request) {
	var wish db.BookingWish
	if err := json.NewDecoder(r.Body).Decode(&wish); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if wish.Date == "" {
		http.Error(w, "date is required", http.StatusBadRequest)
		return
	}
	if _, err := time.Parse("2006-01-02", wish.Date); err != nil {
		http.Error(w, "date must be in YYYY-MM-DD format", http.StatusBadRequest)
		return
	}
	hm, err := scheduler.ParseTime(wish.StartTime)
	if err != nil || hm[0] < 0 || hm[0] > 23 || hm[1] < 0 || hm[1] > 59 {
		http.Error(w, "invalid start_time", http.StatusBadRequest)
		return
	}
	if wish.DurationMinutes < 30 || wish.DurationMinutes > 180 {
		http.Error(w, "duration_minutes must be between 30 and 180", http.StatusBadRequest)
		return
	}
	if len(wish.RoomPriorities) == 0 {
		http.Error(w, "room_priorities must not be empty", http.StatusBadRequest)
		return
	}

	id, err := s.db.CreateBooking(wish)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	wish.ID = id
	s.ScheduleBookingJob(id, wish)

	writeJSON(w, map[string]string{"id": id})
}

func (s *Server) getBookingLog(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	entries, err := s.db.GetExecLog(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if entries == nil {
		entries = []db.ExecLogEntry{}
	}
	writeJSON(w, entries)
}

func (s *Server) deleteBooking(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	s.scheduler.Cancel(id)

	if err := s.db.DeleteBooking(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ScheduleBookingJob calculates the trigger time and schedules the booking execution.
func (s *Server) ScheduleBookingJob(id string, wish db.BookingWish) {
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		log.Printf("failed to load timezone: %v", err)
		return
	}

	trigger, err := scheduler.CalculateTriggerTime(wish.Date, wish.StartTime, loc)
	if err != nil {
		log.Printf("failed to calculate trigger time for booking %s: %v", id, err)
		_ = s.db.UpdateBookingStatus(id, "failed", "", nil, nil, fmt.Sprintf("invalid schedule: %v", err))
		return
	}

	if trigger.Before(time.Now()) {
		// Trigger time already passed — execute immediately instead of failing
		_ = s.db.UpdateBookingStatus(id, "executing", "", nil, nil, "")
		go s.executeBooking(id, wish)
		return
	}

	_ = s.db.UpdateBookingStatus(id, "scheduled", "", nil, nil, "")

	s.scheduler.Schedule(&scheduler.Job{
		ID:          id,
		TriggerTime: trigger,
		Execute: func() {
			s.executeBooking(id, wish)
		},
	})
}

// isTimingError returns true if the error message indicates the booking was
// attempted outside the confirmation window (too early).
func isTimingError(errMsg string) bool {
	lower := strings.ToLower(errMsg)
	return strings.Contains(lower, "vorläufig") ||
		strings.Contains(lower, "bestätigt werden") ||
		strings.Contains(lower, "vorlaufig") ||
		strings.Contains(lower, "bestatigt werden")
}

// executeBooking logs into Asimut and attempts to book a room from the priority list.
func (s *Server) executeBooking(id string, wish db.BookingWish) {
	log.Printf("booking %s: === EXECUTION START === date=%s start=%s duration=%dmin rooms=%v",
		id, wish.Date, wish.StartTime, wish.DurationMinutes, wish.RoomPriorities)
	s.db.LogExec(id, "start", fmt.Sprintf("execution started: date=%s start=%s duration=%dmin",
		wish.Date, wish.StartTime, wish.DurationMinutes),
		fmt.Sprintf("rooms=%v", wish.RoomPriorities))

	if err := s.asimut.Login(); err != nil {
		log.Printf("booking %s: login failed: %v", id, err)
		s.db.LogExec(id, "login", "login failed", err.Error())
		_ = s.db.UpdateBookingStatus(id, "failed", "", nil, nil, fmt.Sprintf("login failed: %v", err))
		return
	}
	log.Printf("booking %s: login successful", id)
	s.db.LogExec(id, "login", "login successful", "")

	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		_ = s.db.UpdateBookingStatus(id, "failed", "", nil, nil, fmt.Sprintf("timezone error: %v", err))
		return
	}

	slotDate, err := time.ParseInLocation("2006-01-02", wish.Date, loc)
	if err != nil {
		s.db.LogExec(id, "error", "invalid date", wish.Date)
		_ = s.db.UpdateBookingStatus(id, "failed", "", nil, nil, fmt.Sprintf("invalid date: %v", err))
		return
	}

	hm, err := scheduler.ParseTime(wish.StartTime)
	if err != nil {
		s.db.LogExec(id, "error", "invalid start time", wish.StartTime)
		_ = s.db.UpdateBookingStatus(id, "failed", "", nil, nil, fmt.Sprintf("invalid start time: %v", err))
		return
	}

	start := time.Date(slotDate.Year(), slotDate.Month(), slotDate.Day(), hm[0], hm[1], 0, 0, loc)

	// Initial booking duration: 30 minutes
	initialDuration := 30 * time.Minute
	end := start.Add(initialDuration)

	log.Printf("booking %s: attempting initial booking: %s to %s (30min)",
		id, start.Format("2006-01-02 15:04"), end.Format("15:04"))
	s.db.LogExec(id, "book_attempt", fmt.Sprintf("attempting booking: %s to %s",
		start.Format("2006-01-02 15:04"), end.Format("15:04")), "")

	var bookedRoom string
	var eventID int
	var lastErr error

	// Retry loop: if we get a timing error (too early for confirmation window),
	// wait and retry up to 6 times (5 min intervals = 30 min total coverage).
	const maxTimingRetries = 6
	const timingRetryInterval = 5 * time.Minute

	for attempt := 0; attempt <= maxTimingRetries; attempt++ {
		if attempt > 0 {
			s.db.LogExec(id, "timing_retry", fmt.Sprintf("timing retry %d/%d: waiting %v",
				attempt, maxTimingRetries, timingRetryInterval), lastErr.Error())
			log.Printf("booking %s: timing error detected, retry %d/%d in %v",
				id, attempt, maxTimingRetries, timingRetryInterval)
			time.Sleep(timingRetryInterval)

			// Re-login before retry
			if err := s.asimut.Login(); err != nil {
				log.Printf("booking %s: re-login before timing retry failed: %v", id, err)
				s.db.LogExec(id, "login", "re-login before timing retry failed", err.Error())
				_ = s.db.UpdateBookingStatus(id, "failed", "", nil, nil, fmt.Sprintf("login failed on timing retry: %v", err))
				return
			}
			s.db.LogExec(id, "login", "re-login before timing retry successful", "")
		}

		bookedRoom = ""
		eventID = 0
		lastErr = nil

		for _, roomID := range wish.RoomPriorities {
			roomName := s.resolveRoomName(roomID)
			log.Printf("booking %s: trying room %d (%s)", id, roomID, roomName)
			s.db.LogExec(id, "try_room", fmt.Sprintf("trying room %d (%s)", roomID, roomName), "")

			result, err := s.asimut.BookRoom(roomID, start, end)
			if err == nil && result.Success {
				bookedRoom = roomName
				eventID = result.EventID
				log.Printf("booking %s: room %d booked successfully, eventID=%d", id, roomID, eventID)
				s.db.LogExec(id, "room_booked", fmt.Sprintf("room %d (%s) booked, eventID=%d",
					roomID, roomName, eventID), "")
				break
			}
			lastErr = err
			log.Printf("booking %s: room %d failed: %v", id, roomID, err)
			s.db.LogExec(id, "room_failed", fmt.Sprintf("room %d (%s) failed", roomID, roomName),
				fmt.Sprintf("%v", err))
		}

		if bookedRoom != "" {
			break
		}

		// If last error is a timing error, retry; otherwise give up
		if lastErr != nil && isTimingError(lastErr.Error()) && attempt < maxTimingRetries {
			continue
		}
		break
	}

	if bookedRoom == "" {
		reason := "no room available"
		if lastErr != nil {
			reason = fmt.Sprintf("no room available: %v", lastErr)
		}
		log.Printf("booking %s: === FAILED === %s", id, reason)
		s.db.LogExec(id, "failed", reason, "")
		_ = s.db.UpdateBookingStatus(id, "failed", "", nil, nil, reason)
		return
	}

	// Extend in 15-minute increments up to desired duration.
	// Each extension waits until exactly 15 min after the previous one
	// (matching the horizon advancement rate). On failure, retry up to
	// 10 times with 5-second gaps before giving up on that extension.
	totalMinutes := 30
	desiredMinutes := wish.DurationMinutes
	extensionCount := 0
	triggerTime := start.Add(-48*time.Hour + 30*time.Minute)
	log.Printf("booking %s: initial 30min booked (event %d, room %s), extending to %d min (trigger=%s)",
		id, eventID, bookedRoom, desiredMinutes, triggerTime.Format("15:04:05"))
	s.db.LogExec(id, "extend_start", fmt.Sprintf("extending from 30min to %dmin, triggerTime=%s",
		desiredMinutes, triggerTime.Format("15:04:05")), "")

	for totalMinutes < desiredMinutes {
		newEnd := end.Add(15 * time.Minute)

		// Wait until trigger + 15min * (extensionCount+1) plus a random jitter
		// of 0–14 minutes to avoid looking like a bot (the priority window is 15-29min)
		targetTime := triggerTime.Add(time.Duration(extensionCount+1) * 15 * time.Minute)
		jitter := time.Duration(rand.Intn(14*60)) * time.Second
		targetTime = targetTime.Add(jitter)

		waitDuration := time.Until(targetTime)
		if waitDuration > 0 {
			log.Printf("booking %s: sleeping until %s (%v) before extension #%d to %s (current now = %s)",
				id, targetTime.Format("15:04:05"), waitDuration.Round(time.Second),
				extensionCount+1, newEnd.Format("15:04"), time.Now().In(loc).Format("15:04:05"))
			s.db.LogExec(id, "extend_wait", fmt.Sprintf("waiting until %s for extension #%d to %s",
				targetTime.Format("15:04:05"), extensionCount+1, newEnd.Format("15:04")),
				fmt.Sprintf("wait=%v", waitDuration.Round(time.Second)))
			time.Sleep(waitDuration)
		} else {
			log.Printf("booking %s: target time %s already passed, proceeding with extension #%d to %s (current now = %s)",
				id, targetTime.Format("15:04:05"), extensionCount+1, newEnd.Format("15:04"), time.Now().In(loc).Format("15:04:05"))
		}
		log.Printf("booking %s: wait complete, now = %s, attempting extension", id, time.Now().In(loc).Format("15:04:05"))

		// Re-login before extension to ensure fresh session after wait
		log.Printf("booking %s: re-login before extension", id)
		if err := s.asimut.Login(); err != nil {
			log.Printf("booking %s: re-login failed: %v, stopping extensions", id, err)
			s.db.LogExec(id, "extend_login_fail", "re-login before extension failed", err.Error())
			break
		}
		log.Printf("booking %s: re-login successful", id)

		// Attempt extension with retries (up to 10 attempts, 5s apart)
		log.Printf("booking %s: extending event %d to %s (%d -> %d min)",
			id, eventID, newEnd.Format("15:04"), totalMinutes, totalMinutes+15)
		s.db.LogExec(id, "extend_attempt", fmt.Sprintf("extending event %d: %d -> %dmin (to %s)",
			eventID, totalMinutes, totalMinutes+15, newEnd.Format("15:04")), "")
		var extErr error
		for attempt := 1; attempt <= 10; attempt++ {
			_, extErr = s.asimut.ExtendBooking(eventID, newEnd)
			if extErr == nil {
				break
			}
			log.Printf("booking %s: extension attempt %d/10 failed: %v", id, attempt, extErr)
			if attempt == 10 {
				s.db.LogExec(id, "extend_failed", fmt.Sprintf("extension to %dmin failed after 10 attempts",
					totalMinutes+15), extErr.Error())
			}
			if attempt < 10 {
				time.Sleep(5 * time.Second)
			}
		}
		if extErr != nil {
			log.Printf("booking %s: extension to %d min failed after 10 attempts, stopping", id, totalMinutes+15)
			break
		}
		end = newEnd
		totalMinutes += 15
		extensionCount++
		log.Printf("booking %s: extension successful, total duration now %d min", id, totalMinutes)
		s.db.LogExec(id, "extend_ok", fmt.Sprintf("extension #%d successful, total %dmin", extensionCount, totalMinutes), "")
	}

	status := "booked"
	if totalMinutes < desiredMinutes {
		status = "partially_booked"
		log.Printf("booking %s: === PARTIAL === booked %d/%d min in room %s", id, totalMinutes, desiredMinutes, bookedRoom)
		s.db.LogExec(id, "result", fmt.Sprintf("partial: %d/%dmin in %s", totalMinutes, desiredMinutes, bookedRoom), "")
	} else {
		log.Printf("booking %s: === SUCCESS === booked full %d min in room %s", id, totalMinutes, bookedRoom)
		s.db.LogExec(id, "result", fmt.Sprintf("success: %dmin in %s", totalMinutes, bookedRoom), "")
	}

	resultDuration := totalMinutes
	_ = s.db.UpdateBookingStatus(id, status, bookedRoom, &resultDuration, &eventID, "")
}

func (s *Server) resolveRoomName(roomID int) string {
	locations, err := s.asimut.GetLocations()
	if err != nil {
		return fmt.Sprintf("%d", roomID)
	}
	for _, loc := range locations {
		if loc.ID == roomID {
			return loc.Name
		}
	}
	return fmt.Sprintf("%d", roomID)
}
