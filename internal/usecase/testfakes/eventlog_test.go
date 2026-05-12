package testfakes

import (
	"testing"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
)

func TestEventLog_AppendAndRead_RoundTrip(t *testing.T) {
	t.Parallel()
	log := NewEventLog()
	if err := log.Append(domain.Event{
		EventID: "01", Timestamp: time.Now(), Type: "x",
	}); err != nil {
		t.Fatal(err)
	}
	got, err := log.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].EventID != "01" {
		t.Errorf("read = %+v", got)
	}
}
