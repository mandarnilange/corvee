package domain

import "testing"

func TestItemType_IsValid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		v    ItemType
		want bool
	}{
		{"project", TypeProject, true},
		{"epic", TypeEpic, true},
		{"story", TypeStory, true},
		{"subtask", TypeSubtask, true},
		{"empty", "", false},
		{"unknown", "feature", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.v.IsValid(); got != tc.want {
				t.Fatalf("ItemType(%q).IsValid() = %v, want %v", tc.v, got, tc.want)
			}
		})
	}
}

func TestStatus_IsValid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		v    Status
		want bool
	}{
		{"backlog", StatusBacklog, true},
		{"ready", StatusReady, true},
		{"claimed", StatusClaimed, true},
		{"in_progress", StatusInProgress, true},
		{"review", StatusReview, true},
		{"blocked", StatusBlocked, true},
		{"done", StatusDone, true},
		{"abandoned", StatusAbandoned, true},
		{"empty", "", false},
		{"unknown", "stalled", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.v.IsValid(); got != tc.want {
				t.Fatalf("Status(%q).IsValid() = %v, want %v", tc.v, got, tc.want)
			}
		})
	}
}

func TestPriority_IsValid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		v    Priority
		want bool
	}{
		{"critical", PriorityCritical, true},
		{"high", PriorityHigh, true},
		{"medium", PriorityMedium, true},
		{"low", PriorityLow, true},
		{"empty", "", false},
		{"unknown", "urgent", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.v.IsValid(); got != tc.want {
				t.Fatalf("Priority(%q).IsValid() = %v, want %v", tc.v, got, tc.want)
			}
		})
	}
}

func TestKind_IsValid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		v    Kind
		want bool
	}{
		{"feature", KindFeature, true},
		{"bug", KindBug, true},
		{"chore", KindChore, true},
		{"spike", KindSpike, true},
		{"docs", KindDocs, true},
		{"refactor", KindRefactor, true},
		{"empty", "", false},
		{"unknown", "task", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.v.IsValid(); got != tc.want {
				t.Fatalf("Kind(%q).IsValid() = %v, want %v", tc.v, got, tc.want)
			}
		})
	}
}

func TestRisk_IsValid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		v    Risk
		want bool
	}{
		{"low", RiskLow, true},
		{"medium", RiskMedium, true},
		{"high", RiskHigh, true},
		{"empty", "", false},
		{"unknown", "extreme", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.v.IsValid(); got != tc.want {
				t.Fatalf("Risk(%q).IsValid() = %v, want %v", tc.v, got, tc.want)
			}
		})
	}
}

func TestRole_IsValid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		v    Role
		want bool
	}{
		{"planner", RolePlanner, true},
		{"executor", RoleExecutor, true},
		{"reviewer", RoleReviewer, true},
		{"human", RoleHuman, true},
		{"empty", "", false},
		{"unknown", "robot", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.v.IsValid(); got != tc.want {
				t.Fatalf("Role(%q).IsValid() = %v, want %v", tc.v, got, tc.want)
			}
		})
	}
}
