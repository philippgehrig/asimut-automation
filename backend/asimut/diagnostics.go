package asimut

import (
	"fmt"
	"html"
	"regexp"
	"strings"
	"time"
)

// IssueTypeLocation is the booking-rule issue type reported when the room is
// already occupied during the requested time.
const IssueTypeLocation = "location"

// BookingIssue is a single booking-rule finding returned by Asimut.
type BookingIssue struct {
	Type  string `json:"type"`
	Class string `json:"class"`
	Text  string `json:"text"`
}

// BookingError is returned when Asimut rejects a booking. Error() returns the
// first issue's text so existing message-based handling keeps working.
type BookingError struct {
	Stage  string // "check" or "save"
	Msg    string
	Issues []BookingIssue
}

func (e *BookingError) Error() string { return e.Msg }

// HasIssueType reports whether any issue has the given type.
func (e *BookingError) HasIssueType(t string) bool {
	for _, i := range e.Issues {
		if i.Type == t {
			return true
		}
	}
	return false
}

var htmlTagRe = regexp.MustCompile(`<[^>]*>`)

// plainText converts Asimut's small HTML snippets (e.g. "<ul><li>x</li></ul>") to readable text.
func plainText(s string) string {
	s = strings.NewReplacer("</li>", "; ", "<br>", " ", "<br/>", " ").Replace(s)
	s = htmlTagRe.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), ";"))
}

// parseBookingIssues extracts bookingrules.issues from a check/save response.
func parseBookingIssues(response map[string]interface{}) []BookingIssue {
	br, ok := response["bookingrules"].(map[string]interface{})
	if !ok {
		return nil
	}
	raw, ok := br["issues"].([]interface{})
	if !ok {
		return nil
	}
	issues := make([]BookingIssue, 0, len(raw))
	for _, r := range raw {
		m, ok := r.(map[string]interface{})
		if !ok {
			continue
		}
		text, _ := m["text"].(string)
		if text == "" {
			continue
		}
		issues = append(issues, BookingIssue{
			Type:  stringFromInterface(m["type"]),
			Class: stringFromInterface(m["class"]),
			Text:  text,
		})
	}
	return issues
}

// PlainText returns the issue text without HTML markup.
func (i BookingIssue) PlainText() string { return plainText(i.Text) }

// parseAgendaEvents flattens the "agenda" map of an agenda response.
func parseAgendaEvents(agendaMap map[string]interface{}) []AsimutEventInfo {
	var events []AsimutEventInfo
	for _, dayGroups := range agendaMap {
		groups, ok := dayGroups.([]interface{})
		if !ok {
			continue
		}
		for _, g := range groups {
			group, ok := g.(map[string]interface{})
			if !ok {
				continue
			}
			eventsRaw, ok := group["events"].([]interface{})
			if !ok {
				continue
			}
			for _, er := range eventsRaw {
				em, ok := er.(map[string]interface{})
				if !ok {
					continue
				}

				roomName := ""
				if rs, ok := em["rs"].([]interface{}); ok && len(rs) > 0 {
					if rm, ok := rs[0].(map[string]interface{}); ok {
						roomName = stringFromInterface(rm["dn"])
						if name := stringFromInterface(rm["name"]); name != "" {
							roomName = name
						}
						if idx := strings.Index(roomName, " ("); idx > 0 {
							roomName = roomName[:idx]
						}
					}
				}

				events = append(events, AsimutEventInfo{
					ID:        intFromInterface(em["id"]),
					Title:     stringFromInterface(em["ar"]),
					RoomName:  roomName,
					StartTime: stringFromInterface(em["st"]),
					EndTime:   stringFromInterface(em["en"]),
				})
			}
		}
	}
	return events
}

// GetRoomEvents returns the events in a room that overlap [from, to).
// Only event id, title and times are returned (no participant data).
func (c *Client) GetRoomEvents(roomID int, from, to time.Time) ([]AsimutEventInfo, error) {
	dayStart := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, from.Location())
	path := fmt.Sprintf("/services/v2/agenda/;category_ids=;custom_id=0;direction=forward;event_limit=100;interval_limit=P2D;location_ids=%d;person_id=;request_config_id=0;start_at=%s/fuzzy",
		roomID, dayStart.Format(timeFormat))

	respBody, err := c.doJSON("GET", path, nil)
	if err != nil {
		return nil, fmt.Errorf("fetching room agenda: %w", err)
	}
	response, ok := respBody["response"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected room agenda response format")
	}
	agendaMap, ok := response["agenda"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("no agenda map in room agenda response")
	}

	var overlapping []AsimutEventInfo
	for _, ev := range parseAgendaEvents(agendaMap) {
		st, err1 := time.Parse(time.RFC3339, ev.StartTime)
		en, err2 := time.Parse(time.RFC3339, ev.EndTime)
		if err1 != nil || err2 != nil {
			continue
		}
		if st.Before(to) && en.After(from) {
			overlapping = append(overlapping, ev)
		}
	}
	return overlapping, nil
}
