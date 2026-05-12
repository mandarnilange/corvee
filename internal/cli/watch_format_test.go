package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
)

func mkFmtEvent() domain.Event {
	return domain.Event{
		EventID:   "01J0EVT0",
		Timestamp: time.Date(2026, 5, 6, 14, 30, 0, 0, time.UTC),
		Type:      "claimed",
		ItemID:    "RKN-E01-S03",
		Actor:     "alice",
		ActorRole: domain.RoleHuman,
		LeaseID:   "01J0LEASE",
		Metadata:  map[string]any{"from": "ready", "to": "claimed"},
	}
}

func TestWatchFormat_JSONL(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := writeWatchEvent(&buf, watchFormatJSONL, mkFmtEvent()); err != nil {
		t.Fatalf("writeWatchEvent: %v", err)
	}
	out := buf.String()
	if !strings.HasSuffix(out, "\n") {
		t.Fatalf("expected trailing newline, got %q", out)
	}
	// Round-trip: parsed JSON has the same EventID.
	var ev domain.Event
	if err := json.Unmarshal([]byte(strings.TrimRight(out, "\n")), &ev); err != nil {
		t.Fatalf("json: %v; payload=%q", err, out)
	}
	if ev.EventID != "01J0EVT0" {
		t.Errorf("event_id=%q", ev.EventID)
	}
	if ev.Type != "claimed" {
		t.Errorf("type=%q", ev.Type)
	}
}

func TestWatchFormat_Text(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := writeWatchEvent(&buf, watchFormatText, mkFmtEvent()); err != nil {
		t.Fatalf("writeWatchEvent: %v", err)
	}
	out := buf.String()
	if !strings.HasSuffix(out, "\n") {
		t.Fatalf("expected trailing newline, got %q", out)
	}
	for _, want := range []string{
		"2026-05-06T14:30:00Z",
		"claimed",
		"RKN-E01-S03",
		"alice",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("want substring %q in %q", want, out)
		}
	}
}

func TestWatchFormat_Compact(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := writeWatchEvent(&buf, watchFormatCompact, mkFmtEvent()); err != nil {
		t.Fatalf("writeWatchEvent: %v", err)
	}
	out := strings.TrimRight(buf.String(), "\n")
	// Compact omits Metadata and lease — stays terse.
	if strings.Contains(out, "lease") || strings.Contains(out, "metadata") {
		t.Errorf("compact should omit lease/metadata, got %q", out)
	}
	for _, want := range []string{"claimed", "RKN-E01-S03", "alice"} {
		if !strings.Contains(out, want) {
			t.Errorf("want substring %q in %q", want, out)
		}
	}
}

func TestWatchFormat_NoEnvelope(t *testing.T) {
	t.Parallel()
	// §15.2: one event per line, no top-level envelope. Specifically the
	// JSONL output must NOT be wrapped in {"ok": true, "data": ...}.
	var buf bytes.Buffer
	if err := writeWatchEvent(&buf, watchFormatJSONL, mkFmtEvent()); err != nil {
		t.Fatalf("writeWatchEvent: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, `"ok":`) || strings.Contains(out, `"data":`) {
		t.Errorf("watch output must not be wrapped in success envelope, got %q", out)
	}
}

func TestParseWatchFormat(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want watchFormat
		err  bool
	}{
		{"", watchFormatJSONL, false},
		{"jsonl", watchFormatJSONL, false},
		{"text", watchFormatText, false},
		{"compact", watchFormatCompact, false},
		{"yaml", 0, true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			got, err := parseWatchFormat(tc.in)
			if tc.err {
				if err == nil {
					t.Fatalf("expected error for %q", tc.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %v want %v", got, tc.want)
			}
		})
	}
}
