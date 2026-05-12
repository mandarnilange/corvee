package oplog

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
)

// Journal implements domain.OpJournal backed by
// .tasks/operations/op-<op-id>.json files. Each file stores the full
// Operation JSON including its Plan and per-step Done flags. The
// directory is gitignored — operations are transient per-VM WAL records,
// not shared state.
type Journal struct {
	dir string
}

// NewJournal returns a Journal rooted at dir. Auto-creates dir if
// missing.
func NewJournal(dir string) (*Journal, error) {
	info, err := os.Stat(dir)
	if os.IsNotExist(err) {
		if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
			return nil, fmt.Errorf("opjournal: mkdir %q: %w", dir, mkErr)
		}
		return &Journal{dir: dir}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("opjournal: stat %q: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("opjournal: %q is not a directory", dir)
	}
	return &Journal{dir: dir}, nil
}

// journalPathFor returns the path for op-<opID>.json, rejecting unsafe IDs.
func (j *Journal) journalPathFor(opID string) (string, error) {
	if !validOpID(opID) {
		return "", fmt.Errorf("opjournal: invalid op_id %q: %w", opID, domain.ErrUsage)
	}
	return filepath.Join(j.dir, "op-"+opID+".json"), nil
}

// Begin writes the operation intent atomically. op.OpID must be pre-set by
// the caller. Status is forced to OpStatusExecuting.
func (j *Journal) Begin(op domain.Operation) error {
	p, err := j.journalPathFor(op.OpID)
	if err != nil {
		return err
	}
	op.Status = domain.OpStatusExecuting
	return journalWrite(p, op)
}

// MarkStep records that stepNum is done within the operation. stepNum must
// be a valid index into the operation's Plan.
func (j *Journal) MarkStep(opID string, stepNum int) error {
	p, err := j.journalPathFor(opID)
	if err != nil {
		return err
	}
	op, err := journalRead(p)
	if err != nil {
		return err
	}
	if stepNum < 0 || stepNum >= len(op.Plan) {
		return fmt.Errorf("opjournal: mark step %d: out of range [0,%d): %w",
			stepNum, len(op.Plan), domain.ErrUsage)
	}
	op.Plan[stepNum].Done = true
	return journalWrite(p, op)
}

// Complete flips status to OpStatusCompleted.
func (j *Journal) Complete(opID string) error {
	p, err := j.journalPathFor(opID)
	if err != nil {
		return err
	}
	op, err := journalRead(p)
	if err != nil {
		return err
	}
	op.Status = domain.OpStatusCompleted
	return journalWrite(p, op)
}

// Pending returns all operations in pending or executing state.
func (j *Journal) Pending() ([]domain.Operation, error) {
	entries, err := os.ReadDir(j.dir)
	if err != nil {
		return nil, fmt.Errorf("opjournal: readdir %q: %w", j.dir, err)
	}
	var ops []domain.Operation
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, "op-") || !strings.HasSuffix(name, ".json") {
			continue
		}
		path := filepath.Join(j.dir, name)
		op, err := journalRead(path)
		if err != nil {
			// Surface corrupt journal entries via slog so operators can
			// see them in CI logs / agent output. validate also reports
			// the integrity warning, but a noisy log here helps diagnose
			// recovery anomalies in real time.
			slog.Warn("opjournal: skipping corrupt journal file",
				"path", path, "error", err)
			continue
		}
		if op.Status == domain.OpStatusPending || op.Status == domain.OpStatusExecuting {
			ops = append(ops, op)
		}
	}
	return ops, nil
}

// journalRead reads and parses an operation file.
func journalRead(path string) (domain.Operation, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return domain.Operation{}, fmt.Errorf("opjournal: read %s: %w", path, err)
	}
	var op domain.Operation
	if err := json.Unmarshal(data, &op); err != nil {
		return domain.Operation{}, fmt.Errorf("opjournal: parse %s: %w", path, err)
	}
	return op, nil
}

// journalWrite atomically writes op to path using tmp+fsync+rename so a
// crash mid-write leaves the previous content intact.
func journalWrite(path string, op domain.Operation) error {
	data, err := json.Marshal(op)
	if err != nil {
		return fmt.Errorf("opjournal: marshal: %w", err)
	}
	tmp := fmt.Sprintf("%s.tmp.%d.%d", path, os.Getpid(), time.Now().UnixNano())
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("opjournal: open tmp %s: %w", tmp, err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("opjournal: write %s: %w", path, err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("opjournal: fsync %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("opjournal: close %s: %w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("opjournal: rename %s: %w", path, err)
	}
	return nil
}
