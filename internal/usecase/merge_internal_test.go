package usecase

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mandarnilange/corvee/internal/domain"
	"github.com/mandarnilange/corvee/internal/usecase/testfakes"
)

func TestParseItem_Roundtrips(t *testing.T) {
	t.Parallel()
	body := []byte(`{"id":"X","type":"story","title":"hi","status":"ready","version":1}`)
	got, err := parseItem(body)
	if err != nil {
		t.Fatalf("parseItem: %v", err)
	}
	if got.ID != "X" {
		t.Errorf("id = %q", got.ID)
	}
}

func TestParseItem_RejectsMalformedJSON(t *testing.T) {
	t.Parallel()
	_, err := parseItem([]byte(`{not-json`))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBuildConflictBlob_DescriptionDiff(t *testing.T) {
	t.Parallel()
	ours := domain.Item{Description: "ours"}
	theirs := domain.Item{Description: "theirs"}
	body := buildConflictBlob(ours, theirs)
	s := string(body)
	if !strings.Contains(s, "## description") {
		t.Errorf("missing description section: %s", s)
	}
	if !strings.Contains(s, "ours") || !strings.Contains(s, "theirs") {
		t.Errorf("missing both sides: %s", s)
	}
}

func TestBuildConflictBlob_AcceptanceDiff(t *testing.T) {
	t.Parallel()
	ours := domain.Item{AcceptanceCriteria: []string{"a", "b"}}
	theirs := domain.Item{AcceptanceCriteria: []string{"a", "c"}}
	body := buildConflictBlob(ours, theirs)
	if !strings.Contains(string(body), "## acceptance_criteria") {
		t.Errorf("missing acceptance section: %s", body)
	}
}

func TestMergeEventsShard_DedupesAndSorts(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	old := filepath.Join(tmp, ".tasks", "events")
	if err := os.MkdirAll(old, 0o755); err != nil {
		t.Fatal(err)
	}
	shardPath := filepath.Join(old, "2026-05-06.jsonl")
	body := `{"event_id":"e1","ts":"2026-05-06T10:00:00Z","type":"item_added"}
{"event_id":"e2","ts":"2026-05-06T11:00:00Z","type":"claim"}
<<<<<<< HEAD
{"event_id":"e3","ts":"2026-05-06T12:00:00Z","type":"completed"}
=======
{"event_id":"e1","ts":"2026-05-06T10:00:00Z","type":"item_added"}
>>>>>>> branch
`
	if err := os.WriteFile(shardPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	// mergeEventsShard resolves repo-relative paths via
	// GitClient.RepoRoot. Wire a fake whose RepoRoot returns tmp.
	g := testfakes.NewGitClient()
	g.RepoRootValue = tmp
	if err := mergeEventsShard(Deps{GitClient: g}, ".tasks/events/2026-05-06.jsonl"); err != nil {
		t.Fatalf("mergeEventsShard: %v", err)
	}
	merged, err := os.ReadFile(shardPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(merged)), "\n")
	if len(lines) != 3 {
		t.Errorf("merged lines = %d, want 3 (dedup); body=%s", len(lines), merged)
	}
	if !strings.Contains(lines[0], "e1") {
		t.Errorf("first line should be earliest: %s", lines[0])
	}
}
