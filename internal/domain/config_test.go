package domain

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestWorkspace_RoundTrip_AllFields(t *testing.T) {
	t.Parallel()
	autoSync := 30
	in := Workspace{
		SchemaVersion:            1,
		WorkspaceName:            "rikonect-backlog",
		DefaultProject:           "RKN",
		ClaimTTLMinutes:          60,
		HeartbeatIntervalMinutes: 30,
		CriticalPath:             []string{"RKN-E01-S01", "RKN-E01-S02"},
		Git:                      GitConfig{AutoCommit: false, AutoSyncSeconds: &autoSync},
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out Workspace
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("round-trip mismatch:\nin =%+v\nout=%+v", in, out)
	}
}

func TestWorkspace_AutoSyncNullPreserved(t *testing.T) {
	t.Parallel()
	in := Workspace{
		SchemaVersion:            1,
		WorkspaceName:            "ws",
		ClaimTTLMinutes:          60,
		HeartbeatIntervalMinutes: 30,
		CriticalPath:             []string{},
		Git:                      GitConfig{AutoCommit: true, AutoSyncSeconds: nil},
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"auto_sync_seconds":null`) {
		t.Errorf("expected null auto_sync_seconds in: %s", data)
	}
	var out Workspace
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.Git.AutoSyncSeconds != nil {
		t.Errorf("AutoSyncSeconds should round-trip as nil")
	}
}

func TestLocal_RoundTrip(t *testing.T) {
	t.Parallel()
	in := Local{
		DefaultAgent:      "mandar",
		AgentRole:         RoleExecutor,
		AgentCapabilities: []string{"go", "python"},
	}
	data, _ := json.Marshal(in)
	var out Local
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("round-trip mismatch:\nin =%+v\nout=%+v", in, out)
	}
}

func TestLocal_EmptyOmitsOptionalFields(t *testing.T) {
	t.Parallel()
	in := Local{}
	data, _ := json.Marshal(in)
	// All fields are omitempty; expect "{}".
	if string(data) != "{}" {
		t.Errorf("empty Local marshal = %s, want {}", data)
	}
}
