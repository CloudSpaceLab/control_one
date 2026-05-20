package server

import "database/sql"

func formatNullTimePtr(t sql.NullTime) *string {
	if !t.Valid {
		return nil
	}
	formatted := formatTime(t.Time.UTC())
	return &formatted
}
