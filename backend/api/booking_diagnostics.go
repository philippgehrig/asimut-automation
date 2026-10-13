package api

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/philippgehrig/asimuth-automation/backend/asimut"
)

// blockerFollowUpDelay is how long after a failed booking the blocking events are
// re-checked. Asimut extends bookings in 15 minute steps, so one step plus a margin.
const blockerFollowUpDelay = 16 * time.Minute

func clock(ts string, loc *time.Location) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ts
	}
	return t.In(loc).Format("15:04")
}

// describeBlocker explains an event that occupies the requested slot.
func describeBlocker(ev asimut.AsimutEventInfo, slotStart, slotEnd time.Time) string {
	st, err1 := time.Parse(time.RFC3339, ev.StartTime)
	en, err2 := time.Parse(time.RFC3339, ev.EndTime)
	base := fmt.Sprintf("'%s' (event #%d) %s–%s", ev.Title, ev.ID, clock(ev.StartTime, slotStart.Location()), clock(ev.EndTime, slotStart.Location()))
	if err1 != nil || err2 != nil {
		return base
	}

	var hints []string
	if st.Before(slotStart) {
		hints = append(hints, fmt.Sprintf("started %d min before your slot, so it was already running and was likely extended into it",
			int(slotStart.Sub(st).Minutes())))
	} else {
		hints = append(hints, fmt.Sprintf("starts %d min into your slot", int(st.Sub(slotStart).Minutes())))
	}
	if en.After(slotEnd) {
		hints = append(hints, fmt.Sprintf("ends %d min after your requested end", int(en.Sub(slotEnd).Minutes())))
	}
	return base + " — " + strings.Join(hints, "; ")
}

// diagnoseRoomFailure builds a short summary (for the failure reason) and a detailed
// explanation (for the execution log) of why a room could not be booked.
// blockers is non-empty when the room was occupied by other events.
func (s *Server) diagnoseRoomFailure(roomID int, roomName string, start, end time.Time, err error) (summary, detail string, blockers []asimut.AsimutEventInfo) {
	var be *asimut.BookingError
	if !errors.As(err, &be) {
		return fmt.Sprintf("%s: %v", roomName, err), fmt.Sprintf("%v", err), nil
	}

	lines := issueLines(err)
	summary = fmt.Sprintf("%s: %s", roomName, asimutPlain(be))

	if be.HasIssueType(asimut.IssueTypeLocation) {
		events, lookupErr := s.asimut.GetRoomEvents(roomID, start, end)
		switch {
		case lookupErr != nil:
			lines = append(lines, "could not look up blocking events: "+lookupErr.Error())
		case len(events) == 0:
			lines = append(lines, "no overlapping event visible in the room agenda (it may have just been released or is not publicly visible)")
		default:
			var names []string
			for _, ev := range events {
				lines = append(lines, "blocked by "+describeBlocker(ev, start, end))
				names = append(names, fmt.Sprintf("'%s' %s–%s", ev.Title, clock(ev.StartTime, start.Location()), clock(ev.EndTime, start.Location())))
			}
			summary = fmt.Sprintf("%s: occupied by %s", roomName, strings.Join(names, ", "))
			blockers = events
		}
	}
	return summary, strings.Join(lines, "\n"), blockers
}

// issueLines lists every booking-rule issue of an error without any network calls.
func issueLines(err error) []string {
	var be *asimut.BookingError
	if !errors.As(err, &be) {
		return []string{err.Error()}
	}
	var lines []string
	for _, issue := range be.Issues {
		lines = append(lines, fmt.Sprintf("[%s/%s] %s", issue.Type, issue.Class, issue.PlainText()))
	}
	if len(lines) == 0 {
		lines = append(lines, be.Msg)
	}
	return lines
}

func asimutPlain(be *asimut.BookingError) string {
	if len(be.Issues) > 0 {
		return be.Issues[0].PlainText()
	}
	return be.Msg
}

// followUpBlockers re-reads the events that blocked a failed booking after a delay and logs
// whether they changed, which shows if the blocking booking is being extended continuously.
func (s *Server) followUpBlockers(id string, start, end time.Time, blocked map[int][]asimut.AsimutEventInfo) {
	time.Sleep(blockerFollowUpDelay)

	if err := s.asimut.Login(); err != nil {
		s.db.LogExec(id, "blocker_followup", "follow-up skipped: login failed", err.Error())
		return
	}

	for roomID, before := range blocked {
		roomName := s.resolveRoomName(roomID)
		after, err := s.asimut.GetRoomEvents(roomID, start, end.Add(6*time.Hour))
		if err != nil {
			s.db.LogExec(id, "blocker_followup", fmt.Sprintf("follow-up for %s failed", roomName), err.Error())
			continue
		}
		byID := make(map[int]asimut.AsimutEventInfo, len(after))
		for _, ev := range after {
			byID[ev.ID] = ev
		}

		var lines []string
		extended := false
		for _, b := range before {
			now, ok := byID[b.ID]
			switch {
			case !ok:
				lines = append(lines, fmt.Sprintf("event #%d no longer overlaps the slot (cancelled, shortened or moved)", b.ID))
			case now.EndTime != b.EndTime:
				extended = true
				lines = append(lines, fmt.Sprintf("event #%d changed its end: %s → %s (continuous extension)",
					b.ID, clock(b.EndTime, start.Location()), clock(now.EndTime, start.Location())))
			default:
				lines = append(lines, fmt.Sprintf("event #%d unchanged (still ends %s)", b.ID, clock(b.EndTime, start.Location())))
			}
		}
		msg := fmt.Sprintf("%s: blocking booking still held", roomName)
		if extended {
			msg = fmt.Sprintf("%s: blocking booking was extended", roomName)
		}
		log.Printf("booking %s: follow-up %s", id, msg)
		s.db.LogExec(id, "blocker_followup", fmt.Sprintf("%s (checked %v after failure)", msg, blockerFollowUpDelay), strings.Join(lines, "\n"))
	}
}
