package sync

import (
	"fmt"
	"log"
	"time"

	"github.com/philippgehrig/asimuth-automation/backend/asimut"
	"github.com/philippgehrig/asimuth-automation/backend/db"
)

func StartEventSync(database *db.DB, client *asimut.Client) {
	go func() {
		for {
			syncEvents(database, client)
			detectMovedBookings(database, client)
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
	to := now.AddDate(0, 0, 28)

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

func detectMovedBookings(database *db.DB, client *asimut.Client) {
	bookings, err := database.ListBookings()
	if err != nil {
		log.Printf("[sync] failed to list bookings for move detection: %v", err)
		return
	}

	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		return
	}
	now := time.Now().In(loc)

	moved := 0
	for _, bk := range bookings {
		if bk.Status != "booked" && bk.Status != "partially_booked" {
			continue
		}
		if bk.ResultEventID == nil {
			continue
		}

		// Only check bookings that haven't passed yet
		bookingDate, err := time.ParseInLocation("2006-01-02", bk.Date, loc)
		if err != nil {
			continue
		}
		if bookingDate.Before(now.Truncate(24 * time.Hour)) {
			continue
		}

		// Fetch the event from Asimut and check if it still matches
		event, err := client.GetEvent(*bk.ResultEventID)
		if err != nil {
			log.Printf("[sync] event %d not found (booking %s), marking as moved: %v", *bk.ResultEventID, bk.ID, err)
			_ = database.UpdateBookingStatus(bk.ID, "moved", bk.ResultRoom, bk.ResultDuration, bk.ResultEventID,
				fmt.Sprintf("Booking was modified or cancelled on Asimut"))
			moved++
			continue
		}

		// Check if room or time changed
		if event.RoomName != "" && event.RoomName != bk.ResultRoom {
			log.Printf("[sync] booking %s: room changed from %s to %s", bk.ID, bk.ResultRoom, event.RoomName)
			_ = database.UpdateBookingStatus(bk.ID, "moved", bk.ResultRoom, bk.ResultDuration, bk.ResultEventID,
				fmt.Sprintf("Room changed to %s on Asimut", event.RoomName))
			moved++
		}
	}

	if moved > 0 {
		log.Printf("[sync] detected %d moved/modified bookings", moved)
	}
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
