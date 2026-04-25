package sessionrecording

import (
	"strings"
	"testing"
)

func TestParseTlogStream(t *testing.T) {
	stream := `
{"type":"session","at":"2026-04-24T10:00:00Z","out":"login: "}
{"type":"session","at":"2026-04-24T10:00:01Z","in":"alice\n"}
{"type":"session","at":"2026-04-24T10:00:02Z","out":"$ "}
{"type":"session","at":"2026-04-24T10:00:03Z","in":"sudo rm -rf /tmp/foo\n"}
{"type":"session","at":"2026-04-24T10:00:04Z","event":"resize","window":[120,40]}
`
	events, err := Parse(strings.NewReader(stream))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 5 {
		t.Fatalf("want 5 events, got %d: %+v", len(events), events)
	}

	var commands []Event
	for _, e := range events {
		if e.Kind == "command" {
			commands = append(commands, e)
		}
	}
	if len(commands) != 2 {
		t.Fatalf("want 2 commands, got %d", len(commands))
	}
	if commands[1].Command != "sudo rm -rf /tmp/foo" {
		t.Fatalf("unexpected command %q", commands[1].Command)
	}

	var resizes []Event
	for _, e := range events {
		if e.Kind == "resize" {
			resizes = append(resizes, e)
		}
	}
	if len(resizes) != 1 || resizes[0].Cols != 120 || resizes[0].Rows != 40 {
		t.Fatalf("resize decode wrong: %+v", resizes)
	}
}

func TestSearchCommands(t *testing.T) {
	events := []Event{
		{Kind: "command", Command: "sudo systemctl restart nginx"},
		{Kind: "command", Command: "ls -la /etc"},
		{Kind: "output", Payload: "sudo"},
	}
	hits := SearchCommands(events, "SUDO")
	if len(hits) != 1 {
		t.Fatalf("want 1 hit, got %d", len(hits))
	}
}

func TestTranscript(t *testing.T) {
	events := []Event{
		{Kind: "output", Payload: "line1\n"},
		{Kind: "input", Payload: "typed"},
		{Kind: "output", Payload: "line2\n"},
	}
	if got := Transcript(events); got != "line1\nline2\n" {
		t.Fatalf("unexpected transcript %q", got)
	}
}

func TestStripANSI(t *testing.T) {
	if got := stripANSI("\x1b[31mred\x1b[0m"); got != "red" {
		t.Fatalf("strip failed: %q", got)
	}
}

func TestSkipsMalformedLines(t *testing.T) {
	stream := "not json\n{\"in\":\"ok\\n\"}\n"
	events, err := Parse(strings.NewReader(stream))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("want 1 event, got %d", len(events))
	}
}
