package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
	"github.com/mandarnilange/corvee/internal/usecase"
	"github.com/mandarnilange/corvee/internal/usecase/testfakes"
)

func mkCLIWatchEvent(id, typ string, ts time.Time) domain.Event {
	return domain.Event{
		EventID: id, Timestamp: ts, Type: typ, ItemID: "RKN-E01", Actor: "alice",
	}
}

func runWatchCmd(t *testing.T, deps Deps, args ...string) string {
	t.Helper()
	root := NewRootCmd(deps)
	root.SetOut(deps.Stdout)
	root.SetErr(deps.Stderr)
	root.SetArgs(append([]string{"watch"}, args...))
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("watch: %v", err)
	}
	return deps.Stdout.(*bytes.Buffer).String()
}

func TestCLIWatch_JSONLDefaultFormatLimit(t *testing.T) {
	t.Parallel()
	tail := testfakes.NewTailer()
	log := testfakes.NewEventLog()
	t0 := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	tail.Send(mkCLIWatchEvent("01J0A", "item_added", t0))
	tail.Send(mkCLIWatchEvent("01J0B", "claimed", t0.Add(time.Second)))
	tail.Send(mkCLIWatchEvent("01J0C", "released", t0.Add(2*time.Second)))

	deps := Deps{
		Globals: &Globals{},
		Stdout:  &bytes.Buffer{},
		Stderr:  &bytes.Buffer{},
		Usecase: usecase.Deps{
			Clock:       testfakes.NewClock(t0),
			EventLog:    log,
			EventTailer: tail,
		},
	}
	out := runWatchCmd(t, deps, "--limit", "2")
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines: %q; want 2", len(lines), out)
	}
	for i, line := range lines {
		var ev domain.Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("line %d not valid JSON: %v; payload=%q", i, err, line)
		}
		if strings.Contains(line, `"ok":`) {
			t.Errorf("line %d has envelope: %q", i, line)
		}
	}
}

func TestCLIWatch_TextFormat(t *testing.T) {
	t.Parallel()
	tail := testfakes.NewTailer()
	t0 := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	tail.Send(mkCLIWatchEvent("01J0A", "item_added", t0))

	deps := Deps{
		Globals: &Globals{},
		Stdout:  &bytes.Buffer{},
		Stderr:  &bytes.Buffer{},
		Usecase: usecase.Deps{
			Clock:       testfakes.NewClock(t0),
			EventLog:    testfakes.NewEventLog(),
			EventTailer: tail,
		},
	}
	out := runWatchCmd(t, deps, "--format", "text", "--limit", "1")
	if !strings.Contains(out, "item_added") || !strings.Contains(out, "RKN-E01") {
		t.Errorf("text output missing fields: %q", out)
	}
}

func TestCLIWatch_RejectsUnknownFormat(t *testing.T) {
	t.Parallel()
	deps := Deps{
		Globals: &Globals{},
		Stdout:  &bytes.Buffer{},
		Stderr:  &bytes.Buffer{},
		Usecase: usecase.Deps{
			Clock:       testfakes.NewClock(time.Now()),
			EventLog:    testfakes.NewEventLog(),
			EventTailer: testfakes.NewTailer(),
		},
	}
	root := NewRootCmd(deps)
	root.SetOut(deps.Stdout)
	root.SetErr(deps.Stderr)
	root.SetArgs([]string{"watch", "--format", "yaml"})
	err := root.ExecuteContext(context.Background())
	if err == nil {
		t.Fatalf("expected error for unknown format")
	}
}

func TestCLIWatch_ExitOnTerminates(t *testing.T) {
	t.Parallel()
	tail := testfakes.NewTailer()
	t0 := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	tail.Send(mkCLIWatchEvent("01J0A", "item_added", t0))
	tail.Send(mkCLIWatchEvent("01J0B", "completed", t0.Add(time.Second)))
	tail.Send(mkCLIWatchEvent("01J0C", "released", t0.Add(2*time.Second)))

	deps := Deps{
		Globals: &Globals{},
		Stdout:  &bytes.Buffer{},
		Stderr:  &bytes.Buffer{},
		Usecase: usecase.Deps{
			Clock:       testfakes.NewClock(t0),
			EventLog:    testfakes.NewEventLog(),
			EventTailer: tail,
		},
	}
	out := runWatchCmd(t, deps, "--exit-on", "completed", "--format", "jsonl")
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines: %q; want 2 stopping at completed", len(lines), out)
	}
}

func TestCLIWatch_MetaFlagFiltersByArbitraryKey(t *testing.T) {
	t.Parallel()
	tail := testfakes.NewTailer()
	t0 := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	tail.Send(domain.Event{
		EventID: "01J0A", Timestamp: t0, Type: "completed", ItemID: "RKN-E01", Actor: "alice",
		Metadata: map[string]any{"sha": "abc", "priority": float64(2)},
	})
	tail.Send(domain.Event{
		EventID: "01J0B", Timestamp: t0.Add(time.Second), Type: "completed", ItemID: "RKN-E02", Actor: "alice",
		Metadata: map[string]any{"sha": "def", "priority": float64(1)},
	})

	deps := Deps{
		Globals: &Globals{},
		Stdout:  &bytes.Buffer{},
		Stderr:  &bytes.Buffer{},
		Usecase: usecase.Deps{
			Clock:       testfakes.NewClock(t0),
			EventLog:    testfakes.NewEventLog(),
			EventTailer: tail,
		},
	}
	out := runWatchCmd(t, deps, "--meta", "sha=abc", "--meta", "priority=2", "--limit", "1")
	if !strings.Contains(out, "01J0A") {
		t.Errorf("expected 01J0A in %q", out)
	}
	if strings.Contains(out, "01J0B") {
		t.Errorf("01J0B should be filtered out: %q", out)
	}
}

func TestCLIWatch_MetaFlagRejectsBadSyntax(t *testing.T) {
	t.Parallel()
	deps := Deps{
		Globals: &Globals{},
		Stdout:  &bytes.Buffer{},
		Stderr:  &bytes.Buffer{},
		Usecase: usecase.Deps{
			Clock:       testfakes.NewClock(time.Now()),
			EventLog:    testfakes.NewEventLog(),
			EventTailer: testfakes.NewTailer(),
		},
	}
	root := NewRootCmd(deps)
	root.SetOut(deps.Stdout)
	root.SetErr(deps.Stderr)
	root.SetArgs([]string{"watch", "--meta", "no-equals-sign"})
	if err := root.ExecuteContext(context.Background()); err == nil {
		t.Fatalf("expected error for malformed --meta")
	}
}

func TestCLIWatch_SinceReplaysFromLog(t *testing.T) {
	t.Parallel()
	tail := testfakes.NewTailer()
	log := testfakes.NewEventLog()
	t0 := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	_ = log.Append(mkCLIWatchEvent("01J0OLD", "item_added", t0.Add(-2*time.Hour)))
	_ = log.Append(mkCLIWatchEvent("01J0RECENT", "item_added", t0.Add(-30*time.Minute)))
	tail.Send(mkCLIWatchEvent("01J0LIVE", "claimed", t0))

	deps := Deps{
		Globals: &Globals{},
		Stdout:  &bytes.Buffer{},
		Stderr:  &bytes.Buffer{},
		Usecase: usecase.Deps{
			Clock:       testfakes.NewClock(t0),
			EventLog:    log,
			EventTailer: tail,
		},
	}
	out := runWatchCmd(t, deps, "--since", "1h", "--limit", "2", "--format", "jsonl")
	if !strings.Contains(out, "01J0RECENT") {
		t.Errorf("expected 01J0RECENT in replay; got %q", out)
	}
	if !strings.Contains(out, "01J0LIVE") {
		t.Errorf("expected 01J0LIVE in live; got %q", out)
	}
	if strings.Contains(out, "01J0OLD") {
		t.Errorf("expected 01J0OLD to be filtered out; got %q", out)
	}
}
