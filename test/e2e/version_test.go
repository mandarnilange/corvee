package e2e

import (
	"encoding/json"
	"testing"
)

// TestVersion_PrintsJSONWithDevVersion is the very first failing test for the
// project. Per spec §11.1 it codifies the expected behavior of `corvee version`
// before the implementation exists.
//
// Contract (from spec §15.1):
//   - stdout is a single JSON object: {"ok": true, "data": {"version": "<v>"}}
//   - exit code 0
//   - stderr is empty (no log spew at default verbosity)
//
// The version string defaults to "dev" when no -ldflags override is supplied,
// or whatever the Makefile injects via git describe. The test accepts any
// non-empty string for `version` so CI builds (where git describe yields a
// SHA) don't have to match a hard-coded literal.
func TestVersion_PrintsJSONWithDevVersion(t *testing.T) {
	res := runTask(t, "version")

	if res.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0; stdout=%q stderr=%q", res.ExitCode, res.Stdout, res.Stderr)
	}
	if res.Stderr != "" {
		t.Errorf("stderr = %q, want empty", res.Stderr)
	}

	var got struct {
		OK   bool `json:"ok"`
		Data struct {
			Version string `json:"version"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(res.Stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\nstdout=%q", err, res.Stdout)
	}
	if !got.OK {
		t.Errorf("got.OK = false, want true (full payload=%q)", res.Stdout)
	}
	if got.Data.Version == "" {
		t.Errorf("got.Data.Version is empty, want a non-empty version string (full payload=%q)", res.Stdout)
	}
}

func TestVersion_PrettyEmitsIndentedJSON(t *testing.T) {
	res := runTask(t, "--pretty", "version")
	if res.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%q", res.ExitCode, res.Stderr)
	}
	// Indented JSON contains a newline. Compact JSON does not.
	if !containsByte(res.Stdout, '\n') {
		t.Errorf("--pretty did not emit indented JSON; stdout=%q", res.Stdout)
	}
}

func containsByte(s string, b byte) bool {
	for i := range s {
		if s[i] == b {
			return true
		}
	}
	return false
}
