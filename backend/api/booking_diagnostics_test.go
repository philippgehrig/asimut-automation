package api

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/philippgehrig/asimuth-automation/backend/asimut"
)

func TestDescribeBlocker(t *testing.T) {
	loc := time.FixedZone("CEST", 2*3600)
	start := time.Date(2026, 10, 15, 10, 0, 0, 0, loc)
	end := start.Add(30 * time.Minute)

	ext := describeBlocker(asimut.AsimutEventInfo{ID: 7, Title: "Einzelüben",
		StartTime: "2026-10-15T09:00:00+02:00", EndTime: "2026-10-15T10:45:00+02:00"}, start, end)
	for _, want := range []string{"#7", "09:00–10:45", "started 60 min before your slot", "likely extended", "15 min after"} {
		if !strings.Contains(ext, want) {
			t.Errorf("%q missing %q", ext, want)
		}
	}

	inside := describeBlocker(asimut.AsimutEventInfo{ID: 8, Title: "X",
		StartTime: "2026-10-15T10:15:00+02:00", EndTime: "2026-10-15T10:30:00+02:00"}, start, end)
	if !strings.Contains(inside, "starts 15 min into your slot") {
		t.Errorf("unexpected: %q", inside)
	}
}

func TestIssueLines(t *testing.T) {
	be := &asimut.BookingError{Msg: "m", Issues: []asimut.BookingIssue{{Type: "location", Class: "w", Text: "a<ul><li>b</li></ul>"}}}
	lines := issueLines(be)
	if len(lines) != 1 || lines[0] != "[location/w] ab" {
		t.Errorf("unexpected: %v", lines)
	}
}

func TestAnyTimingError(t *testing.T) {
	timing := errors.New("Ihre Buchung ist vorläufig. Die Buchung kann bestätigt werden")
	overlap := errors.New("Ihre Buchung überschneidet sich mit: Einzelüben")

	if anyTimingError(nil) {
		t.Error("no failures should not retry")
	}
	if anyTimingError([]roomFailure{{err: overlap}}) {
		t.Error("overlap only should not retry")
	}
	if !anyTimingError([]roomFailure{{err: timing}, {err: overlap}}) {
		t.Error("timing error on a non-last room must trigger a retry")
	}
}
