package domain

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

// fixtureSection5 is the canonical Item example from spec §5. The
// round-trip test asserts that parsing this JSON, re-marshalling, and
// parsing again yields identical Item values.
const fixtureSection5 = `{
  "schema_version": 1,
  "id": "RKN-E01-S03",
  "type": "story",
  "kind": "feature",
  "project_id": "RKN",
  "parent_id": "RKN-E01",
  "title": "Build emotion classifier pipeline",
  "description": "Train and deploy the emotion classifier MVP.",
  "status": "in_progress",
  "priority": "high",
  "tags": ["ml", "nlp"],
  "claim": {
    "agent": "agent-claude-1",
    "lease_id": "01HX9K3QT8FSXR2M5J7N",
    "claimed_at": "2026-05-02T10:00:00Z",
    "expires_at": "2026-05-02T11:00:00Z"
  },
  "required_capabilities": ["python", "ml"],
  "created_at": "2026-05-01T08:00:00Z",
  "created_by": "agent-planner-1",
  "created_by_role": "planner",
  "updated_at": "2026-05-02T10:15:00Z",
  "version": 7,
  "impact": {
    "files": ["src/ml/classifier.py", "tests/test_classifier.py"],
    "modules": ["ml.classifier"],
    "estimated_hours": 6,
    "estimated_loc": 200,
    "risk": "medium"
  },
  "dependencies": ["RKN-E01-S01"],
  "acceptance_criteria": [
    "Classifier achieves >85% accuracy on validation set",
    "Unit tests cover happy path + 3 failure modes",
    "Inference latency <100ms p95"
  ],
  "journal": [
    { "ts": "2026-05-01T08:00:00Z", "actor": "mandar", "event": "created" },
    { "ts": "2026-05-02T10:00:00Z", "actor": "agent-claude-1", "event": "claimed" }
  ]
}`

func TestItem_RoundTrip_Section5Fixture(t *testing.T) {
	t.Parallel()

	var first Item
	if err := json.Unmarshal([]byte(fixtureSection5), &first); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}

	encoded, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var second Item
	if err := json.Unmarshal(encoded, &second); err != nil {
		t.Fatalf("re-unmarshal: %v", err)
	}

	if !reflect.DeepEqual(first, second) {
		t.Fatalf("round-trip mismatch:\nbefore=%+v\nafter =%+v", first, second)
	}

	// Spot-check a handful of fields against the spec text so a
	// silent JSON-tag rename can't hide.
	if first.ID != "RKN-E01-S03" {
		t.Errorf("ID = %q, want RKN-E01-S03", first.ID)
	}
	if first.Type != TypeStory {
		t.Errorf("Type = %q, want %q", first.Type, TypeStory)
	}
	if first.Kind != KindFeature {
		t.Errorf("Kind = %q, want %q", first.Kind, KindFeature)
	}
	if first.Status != StatusInProgress {
		t.Errorf("Status = %q, want %q", first.Status, StatusInProgress)
	}
	if first.Priority != PriorityHigh {
		t.Errorf("Priority = %q, want %q", first.Priority, PriorityHigh)
	}
	if first.Version != 7 {
		t.Errorf("Version = %d, want 7", first.Version)
	}
	if first.Claim == nil || first.Claim.LeaseID != "01HX9K3QT8FSXR2M5J7N" {
		t.Errorf("Claim mismatch: %+v", first.Claim)
	}
	if first.Impact.EstimatedHours != 6 || first.Impact.Risk != RiskMedium {
		t.Errorf("Impact mismatch: %+v", first.Impact)
	}
	if got, want := len(first.Journal), 2; got != want {
		t.Errorf("len(Journal) = %d, want %d", got, want)
	}
	if first.CreatedByRole != RolePlanner {
		t.Errorf("CreatedByRole = %q, want %q", first.CreatedByRole, RolePlanner)
	}
}

func TestItem_RoundTrip_RequiredOnly(t *testing.T) {
	t.Parallel()

	created := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	updated := time.Date(2026, 5, 1, 9, 30, 0, 0, time.UTC)

	in := Item{
		SchemaVersion: 1,
		ID:            "RKN",
		Type:          TypeProject,
		Title:         "Project Rikonect",
		Status:        StatusBacklog,
		CreatedAt:     created,
		UpdatedAt:     updated,
		Version:       1,
	}

	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var out Item
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !reflect.DeepEqual(in, out) {
		t.Fatalf("round-trip mismatch:\nin =%+v\nout=%+v\njson=%s", in, out, data)
	}

	// Required field JSON keys must appear in the encoded output.
	requiredKeys := []string{
		`"schema_version":1`,
		`"id":"RKN"`,
		`"type":"project"`,
		`"title":"Project Rikonect"`,
		`"status":"backlog"`,
		`"created_at":"2026-05-01T08:00:00Z"`,
		`"updated_at":"2026-05-01T09:30:00Z"`,
		`"version":1`,
	}
	for _, key := range requiredKeys {
		if !contains(string(data), key) {
			t.Errorf("encoded JSON missing required substring %s\nfull=%s", key, data)
		}
	}
}

// contains is a tiny strings.Contains stand-in to keep the test file
// dependency-free at the domain layer.
func contains(haystack, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
