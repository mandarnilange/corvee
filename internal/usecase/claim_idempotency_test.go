package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
	"github.com/mandarnilange/corvee/internal/usecase/testfakes"
)

func TestClaim_OperationID_ReplayReturnsPriorResult(t *testing.T) {
	t.Parallel()
	store := testfakes.NewStore()
	mustPut(t, store, domain.Item{
		ID: "RKN-E01-S01", Type: domain.TypeStory, Status: domain.StatusReady, Version: 1,
	})
	d := Deps{
		Store: store, Clock: testfakes.NewClock(time.Now()),
		IDGen: testfakes.NewIDGen("L"), Locker: testfakes.NewLocker(),
		OpResults: testfakes.NewOpResults(),
	}
	first, err := Claim(context.Background(), d, ClaimInput{
		ID: "RKN-E01-S01", Agent: "alice", OperationID: "op-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Claim(context.Background(), d, ClaimInput{
		ID: "RKN-E01-S01", Agent: "alice", OperationID: "op-1",
	})
	if err != nil {
		t.Fatalf("replay returned error: %v", err)
	}
	if second.LeaseID != first.LeaseID {
		t.Errorf("replay should return cached LeaseID; first=%s second=%s", first.LeaseID, second.LeaseID)
	}
}
