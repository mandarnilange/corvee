package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
)

// watchFormat selects how `corvee watch` renders each event line. Per
// spec §15.2, output is one event per line with no top-level envelope.
type watchFormat int

const (
	// watchFormatJSONL emits the raw JSON-encoded Event followed by '\n'.
	// Default — pairs with the JSON-only contract for agent consumers.
	watchFormatJSONL watchFormat = iota
	// watchFormatText emits a human-readable single line including the
	// timestamp, type, item id, actor, and lease. Suited for tail -f style
	// observation by humans.
	watchFormatText
	// watchFormatCompact emits a terse single line with timestamp, type,
	// item id, and actor — no metadata or lease. Optimised for dense
	// dashboards that don't care about the full payload.
	watchFormatCompact
)

// parseWatchFormat resolves the --format flag value. Empty defaults to
// jsonl. Unknown values return ErrUsage.
func parseWatchFormat(s string) (watchFormat, error) {
	switch strings.ToLower(s) {
	case "", "jsonl":
		return watchFormatJSONL, nil
	case "text":
		return watchFormatText, nil
	case "compact":
		return watchFormatCompact, nil
	}
	return 0, fmt.Errorf("--format=%q must be jsonl|text|compact: %w", s, domain.ErrUsage)
}

// writeWatchEvent renders ev to w in the chosen format, terminated by
// a single newline. Returns any underlying write error.
func writeWatchEvent(w io.Writer, f watchFormat, ev domain.Event) error {
	switch f {
	case watchFormatText:
		line := renderWatchText(ev)
		_, err := fmt.Fprintln(w, line)
		return err
	case watchFormatCompact:
		line := renderWatchCompact(ev)
		_, err := fmt.Fprintln(w, line)
		return err
	case watchFormatJSONL:
		fallthrough
	default:
		buf, err := json.Marshal(ev)
		if err != nil {
			return fmt.Errorf("watch: marshal event: %w", err)
		}
		buf = append(buf, '\n')
		_, err = w.Write(buf)
		return err
	}
}

func renderWatchText(ev domain.Event) string {
	var b strings.Builder
	b.WriteString(ev.Timestamp.UTC().Format(time.RFC3339))
	b.WriteString(" [")
	b.WriteString(ev.Type)
	b.WriteString("] ")
	if ev.ItemID != "" {
		b.WriteString(ev.ItemID)
	}
	if ev.Actor != "" {
		b.WriteString(" by ")
		b.WriteString(ev.Actor)
	}
	if ev.LeaseID != "" {
		b.WriteString(" lease=")
		b.WriteString(ev.LeaseID)
	}
	if len(ev.Metadata) > 0 {
		md, err := json.Marshal(ev.Metadata)
		if err == nil {
			b.WriteString(" metadata=")
			b.Write(md)
		}
	}
	return b.String()
}

func renderWatchCompact(ev domain.Event) string {
	var b strings.Builder
	b.WriteString(ev.Timestamp.UTC().Format(time.RFC3339))
	b.WriteByte(' ')
	b.WriteString(ev.Type)
	if ev.ItemID != "" {
		b.WriteByte(' ')
		b.WriteString(ev.ItemID)
	}
	if ev.Actor != "" {
		b.WriteByte(' ')
		b.WriteString(ev.Actor)
	}
	return b.String()
}
