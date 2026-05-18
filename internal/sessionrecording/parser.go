package sessionrecording

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
)

// Event is one entry inside a parsed session recording. The fields are a
// normalized subset of what tlog and linux auditx emit; the parser copes with
// either by detecting structural hints rather than requiring a strict schema.
type Event struct {
	At       time.Time `json:"at"`
	Kind     string    `json:"kind"` // input | output | resize | command | other
	Payload  string    `json:"payload"`
	Command  string    `json:"command,omitempty"`
	Cols     int       `json:"cols,omitempty"`
	Rows     int       `json:"rows,omitempty"`
	Sequence int       `json:"sequence"`
}

// Parse consumes a JSON-lines session recording stream (one record per line)
// and returns a sequence of normalized Events. Lines that fail to decode are
// skipped with no error — partial recordings are still useful.
func Parse(r io.Reader) ([]Event, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1<<20), 16<<20) // allow 16MB lines for long terminal output
	var events []Event
	seq := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		ev, ok := decodeLine(line, seq)
		if !ok {
			continue
		}
		events = append(events, ev)
		seq++
	}
	if err := scanner.Err(); err != nil {
		return events, fmt.Errorf("scan session: %w", err)
	}
	return events, nil
}

// tlogRecord is the subset of the tlog JSON-lines format we parse. tlog uses
// "in" / "out" for stdin/stdout payloads and "window" for resize events.
type tlogRecord struct {
	Type   string          `json:"type"`
	At     string          `json:"at,omitempty"`
	Ts     float64         `json:"ts,omitempty"`
	In     string          `json:"in,omitempty"`
	Out    string          `json:"out,omitempty"`
	Window []int           `json:"window,omitempty"`
	Event  string          `json:"event,omitempty"`
	Raw    json.RawMessage `json:"-"`
}

func decodeLine(line string, seq int) (Event, bool) {
	var rec tlogRecord
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		return Event{}, false
	}
	at := parseRecordTime(rec)
	ev := Event{At: at, Sequence: seq}
	switch {
	case rec.In != "":
		ev.Kind = "input"
		ev.Payload = rec.In
		if cmd := extractCommand(rec.In); cmd != "" {
			ev.Command = cmd
			ev.Kind = "command"
		}
	case rec.Out != "":
		ev.Kind = "output"
		ev.Payload = rec.Out
	case rec.Event == "resize" || len(rec.Window) == 2:
		ev.Kind = "resize"
		if len(rec.Window) == 2 {
			ev.Cols = rec.Window[0]
			ev.Rows = rec.Window[1]
		}
	default:
		ev.Kind = "other"
		ev.Payload = line
	}
	return ev, true
}

func parseRecordTime(rec tlogRecord) time.Time {
	if rec.At != "" {
		if t, err := time.Parse(time.RFC3339Nano, rec.At); err == nil {
			return t
		}
	}
	if rec.Ts > 0 {
		sec := int64(rec.Ts)
		nsec := int64((rec.Ts - float64(sec)) * 1e9)
		return time.Unix(sec, nsec).UTC()
	}
	return time.Time{}
}

// extractCommand returns a plausible shell command from an input payload. It
// looks for a trailing newline + non-empty content; anything without a newline
// is still typing, not a submitted command.
func extractCommand(in string) string {
	if !strings.Contains(in, "\n") && !strings.Contains(in, "\r") {
		return ""
	}
	// Strip trailing newline(s) and ANSI noise.
	trimmed := strings.TrimRight(in, "\r\n")
	trimmed = stripANSI(trimmed)
	if len(trimmed) == 0 || len(trimmed) > 2048 {
		return ""
	}
	return trimmed
}

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

func stripANSI(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}

// SearchCommands returns commands matching the pattern (case-insensitive
// substring or compiled regex). Useful for the replay UI's "find all sudo
// calls in this session" affordance.
func SearchCommands(events []Event, pattern string) []Event {
	pat := strings.ToLower(strings.TrimSpace(pattern))
	if pat == "" {
		return nil
	}
	var out []Event
	for _, ev := range events {
		if ev.Kind != "command" {
			continue
		}
		if strings.Contains(strings.ToLower(ev.Command), pat) {
			out = append(out, ev)
		}
	}
	return out
}

// Transcript returns the visible-output portion of a session as a single
// string, useful for plain-text download or copying.
func Transcript(events []Event) string {
	var b strings.Builder
	for _, ev := range events {
		if ev.Kind == "output" {
			b.WriteString(ev.Payload)
		}
	}
	return b.String()
}
