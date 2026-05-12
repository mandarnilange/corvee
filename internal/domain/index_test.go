package domain

import (
	"testing"
	"time"
)

func TestIndexEntryFromItem_CopiesProjectionFields(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	it := Item{
		ID:                   "RKN-E01-S01",
		Type:                 TypeStory,
		ProjectID:            "RKN",
		ParentID:             "RKN-E01",
		Status:               StatusReady,
		Priority:             PriorityHigh,
		Kind:                 KindFeature,
		RequiredCapabilities: []string{"go"},
		Dependencies:         []string{"RKN-E01-S00"},
		Claim:                &Claim{Agent: "alice"},
		Version:              3,
		UpdatedAt:            now,
		Title:                "story",
	}
	e := IndexEntryFromItem(it)
	if e.ID != it.ID || e.Type != it.Type || e.ProjectID != it.ProjectID || e.ParentID != it.ParentID {
		t.Errorf("hierarchy fields wrong: %+v", e)
	}
	if e.Status != it.Status || e.Priority != it.Priority || e.Kind != it.Kind {
		t.Errorf("enum fields wrong: %+v", e)
	}
	if !e.HasClaim {
		t.Errorf("HasClaim not set")
	}
	if e.Version != 3 || !e.UpdatedAt.Equal(now) || e.Title != "story" {
		t.Errorf("scalar fields wrong: %+v", e)
	}
	if len(e.RequiredCapabilities) != 1 || e.RequiredCapabilities[0] != "go" {
		t.Errorf("caps lost: %+v", e.RequiredCapabilities)
	}
	if len(e.Dependencies) != 1 || e.Dependencies[0] != "RKN-E01-S00" {
		t.Errorf("deps lost: %+v", e.Dependencies)
	}
}

func TestIndexEntryFromItem_NilClaim_HasClaimFalse(t *testing.T) {
	t.Parallel()
	e := IndexEntryFromItem(Item{ID: "X", Claim: nil})
	if e.HasClaim {
		t.Errorf("HasClaim should be false")
	}
}
