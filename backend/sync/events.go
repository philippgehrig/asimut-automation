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
