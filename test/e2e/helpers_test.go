package e2e

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

// osEnviron is split out so test helpers don't import os transitively.
func osEnviron() []string { return os.Environ() }

// binPath is resolved once per test binary. Tests that need it call binary(t)
// which builds bin/corvee on first call (relative to the repo root).
var (
	binOnce sync.Once
	binAbs  string
	binErr  error
)

// binary returns the absolute path to bin/corvee, building it once per test
// process. Subsequent calls within the same `go test` run reuse the binary.
func binary(t *testing.T) string {
	t.Helper()
	binOnce.Do(func() {
		// Walk up from this test file to the repo root (the directory holding
		// go.mod) so tests run from any working directory.
		_, here, _, _ := runtime.Caller(0)
		dir := filepath.Dir(here)
		root := dir
		for {
			if fileExists(filepath.Join(root, "go.mod")) {
				break
			}
			parent := filepath.Dir(root)
			if parent == root {
				binErr = errors.New("could not locate repo root (no go.mod found walking up)")
				return
			}
			root = parent
		}
		binAbs = filepath.Join(root, "bin", "corvee")
		// Build via `make build` so all tests use the same flags as CI.
		cmd := exec.Command("make", "build")
		cmd.Dir = root
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			binErr = errors.New("make build failed: " + err.Error() + ": " + stderr.String())
		}
	})
	if binErr != nil {
		t.Fatalf("could not build corvee binary: %v", binErr)
	}
	return binAbs
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// runResult captures stdout, stderr, and exit code from a command run.
type runResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// runTask executes the binary with args and returns stdout/stderr/exit code.
// Does not fail the test on non-zero exit; callers assert against ExitCode.
func runTask(t *testing.T, args ...string) runResult {
	t.Helper()
	return runTaskIn(t, "", nil, args...)
}

// runTaskIn executes the binary in the given working directory with
// extra environment variables (key=value strings). Used by tests that
// need an isolated .tasks/ workspace per case.
func runTaskIn(t *testing.T, workdir string, extraEnv []string, args ...string) runResult {
	t.Helper()
	bin := binary(t)
	cmd := exec.Command(bin, args...)
	if workdir != "" {
		cmd.Dir = workdir
	}
	if len(extraEnv) > 0 {
		// Inherit current env plus overrides.
		cmd.Env = append(currentEnv(), extraEnv...)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	res := runResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if err == nil {
		res.ExitCode = 0
		return res
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		res.ExitCode = exitErr.ExitCode()
		return res
	}
	t.Fatalf("running %s %v in %s: %v (stderr=%q)", bin, args, workdir, err, stderr.String())
	return res
}

// currentEnv returns the current process environment as a []string.
func currentEnv() []string {
	return append([]string{}, osEnviron()...)
}
