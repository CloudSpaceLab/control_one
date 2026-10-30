package patchsupport

import (
	"testing"
	"time"
)

func TestCalendarWindowStartBoundaries(t *testing.T) {
	now := time.Now().UTC()
	for _, scenario := range []string{"valid", "at open", "at close", "before open", "expired", "missing id", "invalid bounds"} {
		t.Run(scenario, func(t *testing.T) {
			w := Window{ID: "window", OpensAt: now.Add(-time.Minute), ClosesAt: now.Add(time.Minute)}
			switch scenario {
			case "at open":
				w.OpensAt = now
			case "at close":
				w.ClosesAt = now
			case "before open":
				w.OpensAt = now.Add(time.Second)
			case "expired":
				w.ClosesAt = now.Add(-time.Second)
			case "missing id":
				w.ID = ""
			case "invalid bounds":
				w.OpensAt = w.ClosesAt
			}
			err := w.Validate(now)
			if scenario == "valid" || scenario == "at open" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("unavailable window accepted")
			}
		})
	}
}
