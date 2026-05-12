package eventlog

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
)

func TestLog_AppendThenRead_RoundTripsOneEvent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log, err := New(dir)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	ev := domain.Event{
		EventID:   "01",
		Timestamp: time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC),
		Type:      "item_added",
		ItemID:    "RKN-E01-S01",
		Actor:     "alice",
	}
	if appendErr := log.Append(ev); appendErr != nil {
		t.Fatalf("append: %v", appendErr)
	}
	out, err := log.Read()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(out) != 1 || out[0].EventID != "01" || out[0].Type != "item_added" {
		t.Errorf("read = %+v", out)
	}
}

func TestLog_Append_ShardsByUTCDate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log, err := New(dir)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	t1 := time.Date(2026, 5, 5, 23, 59, 0, 0, time.UTC)
	t2 := time.Date(2026, 5, 6, 0, 1, 0, 0, time.UTC)
	for i, ts := range []time.Time{t1, t2} {
		if err := log.Append(domain.Event{EventID: string(rune('A' + i)), Timestamp: ts, Type: "x"}); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "2026-05-05.jsonl")); err != nil {
		t.Errorf("missing 2026-05-05.jsonl: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "2026-05-06.jsonl")); err != nil {
		t.Errorf("missing 2026-05-06.jsonl: %v", err)
	}
}

func TestLog_Read_ConcatenatesShardsInOrder(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log, err := New(dir)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	events := []domain.Event{
		{EventID: "01", Timestamp: time.Date(2026, 5, 6, 0, 1, 0, 0, time.UTC), Type: "second"},
		{EventID: "00", Timestamp: time.Date(2026, 5, 5, 23, 59, 0, 0, time.UTC), Type: "first"},
	}
	for _, ev := range events {
		if appendErr := log.Append(ev); appendErr != nil {
			t.Fatalf("append: %v", appendErr)
		}
	}
	out, err := log.Read()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(out) != 2 || out[0].Type != "first" || out[1].Type != "second" {
		t.Errorf("ordering wrong: %+v", out)
	}
}

func TestLog_Append_RejectsMissingFields(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log, err := New(dir)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	cases := []struct {
		name string
		ev   domain.Event
	}{
		{"no event_id", domain.Event{Timestamp: time.Now().UTC(), Type: "x"}},
		{"no timestamp", domain.Event{EventID: "01", Type: "x"}},
		{"no type", domain.Event{EventID: "01", Timestamp: time.Now().UTC()}},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			err := log.Append(c.ev)
			if !errors.Is(err, domain.ErrUsage) {
				t.Errorf("want ErrUsage, got %v", err)
			}
		})
	}
}

func TestLog_Scan_StreamsInChronologicalOrder(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log, err := New(dir)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	events := []domain.Event{
		{EventID: "01", Timestamp: time.Date(2026, 5, 6, 0, 1, 0, 0, time.UTC), Type: "second"},
		{EventID: "00", Timestamp: time.Date(2026, 5, 5, 23, 59, 0, 0, time.UTC), Type: "first"},
	}
	for _, ev := range events {
		if err := log.Append(ev); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	var seen []string
	if err := log.Scan(func(ev domain.Event) error {
		seen = append(seen, ev.Type)
		return nil
	}); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(seen) != 2 || seen[0] != "first" || seen[1] != "second" {
		t.Errorf("scan order = %v, want [first second]", seen)
	}
}

func TestLog_Scan_PropagatesAndStopsOnError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log, err := New(dir)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	for i := range 3 {
		if err := log.Append(domain.Event{
			EventID:   string(rune('A' + i)),
			Timestamp: time.Date(2026, 5, 5, 0, i, 0, 0, time.UTC),
			Type:      "x",
		}); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	stop := errors.New("halt")
	count := 0
	err = log.Scan(func(_ domain.Event) error {
		count++
		if count == 2 {
			return stop
		}
		return nil
	})
	if !errors.Is(err, stop) {
		t.Fatalf("err = %v, want %v", err, stop)
	}
	if count != 2 {
		t.Errorf("scan kept going after stop: count=%d", count)
	}
}

func TestLog_Read_SkipsCorruptLines(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "2026-05-05.jsonl")
	body := `{"event_id":"01","ts":"2026-05-05T00:00:00Z","type":"good"}` + "\n" +
		"this is not json\n" +
		`{"event_id":"02","ts":"2026-05-05T00:00:01Z","type":"good"}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	log, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	out, err := log.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Errorf("want 2 good events, got %d: %+v", len(out), out)
	}
}
