package patchsupport

import (
	"fmt"
	"time"
)

// Window binds the calendar bounds reviewed at dispatch. It does not certify
// firewall enforcement or promise to safely abort a running package transaction.
type Window struct {
	ID       string    `json:"id"`
	OpensAt  time.Time `json:"opens_at"`
	ClosesAt time.Time `json:"closes_at"`
}

func (w *Window) Validate(now time.Time) error {
	if w == nil {
		return nil
	}
	if w.ID == "" || w.OpensAt.IsZero() || !w.ClosesAt.After(w.OpensAt) || now.Before(w.OpensAt) || !now.Before(w.ClosesAt) {
		return fmt.Errorf("patch calendar window is invalid, not started or expired; create a new reviewed request")
	}
	return nil
}
