package domain

import (
	"encoding/json"
	"testing"
	"time"
)

func TestOperation_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	op := Operation{
		OpID:      "01JXABCDEF",
		Kind:      OpKindMove,
		Agent:     "agent-1",
		StartedAt: now,
		Status:    OpStatusExecuting,
		Plan: []OpStep{
			{Step: 0, Kind: "create_items", Args: map[string]any{"items": []any{"A"}}, Done: false},
			{Step: 1, Kind: "delete_items", Args: map[string]any{"old_ids": []any{"X"}}, Done: true},
		},
	}
	data, err := json.Marshal(op)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Operation
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.OpID != op.OpID {
		t.Errorf("OpID: got %q want %q", got.OpID, op.OpID)
	}
	if got.Kind != op.Kind {
		t.Errorf("Kind: got %q want %q", got.Kind, op.Kind)
	}
	if got.Status != op.Status {
		t.Errorf("Status: got %q want %q", got.Status, op.Status)
	}
	if len(got.Plan) != 2 {
		t.Fatalf("Plan len: got %d want 2", len(got.Plan))
	}
	if got.Plan[0].Step != 0 || got.Plan[0].Kind != "create_items" || got.Plan[0].Done {
		t.Errorf("Plan[0]: %+v", got.Plan[0])
	}
	if got.Plan[1].Step != 1 || got.Plan[1].Kind != "delete_items" || !got.Plan[1].Done {
		t.Errorf("Plan[1]: %+v", got.Plan[1])
	}
}

func TestOpKind_Values(t *testing.T) {
	t.Parallel()
	cases := []struct {
		kind OpKind
		want string
	}{
		{OpKindMove, "move"},
		{OpKindRename, "rename"},
		{OpKindDeleteCascade, "delete_cascade"},
		{OpKindCloneWithChildren, "clone_with_children"},
	}
	for _, tc := range cases {
		if string(tc.kind) != tc.want {
			t.Errorf("OpKind %q: want %q", tc.kind, tc.want)
		}
	}
}

func TestOpStatus_Values(t *testing.T) {
	t.Parallel()
	cases := []struct {
		status OpStatus
		want   string
	}{
		{OpStatusPending, "pending"},
		{OpStatusExecuting, "executing"},
		{OpStatusCompleted, "completed"},
		{OpStatusFailed, "failed"},
	}
	for _, tc := range cases {
		if string(tc.status) != tc.want {
			t.Errorf("OpStatus %q: want %q", tc.status, tc.want)
		}
	}
}

func TestOpStep_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	step := OpStep{
		Step: 2,
		Kind: "rewrite_refs",
		Args: map[string]any{
			"mapping": []any{
				map[string]any{"old_id": "A-E01", "new_id": "B-E01"},
			},
		},
		Done: false,
	}
	data, err := json.Marshal(step)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got OpStep
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Step != step.Step || got.Kind != step.Kind || got.Done != step.Done {
		t.Errorf("mismatch: got %+v want %+v", got, step)
	}
	if got.Args == nil {
		t.Error("Args should not be nil after round-trip")
	}
}
