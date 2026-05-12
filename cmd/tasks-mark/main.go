// Command tasks-mark updates the status (and optionally completed_at)
// of a phase or a story in .spec/tasks.json. It exists so the dogfood
// progress tracker can be kept in sync as commits land — until Phase 7's
// `corvee import` ships, this is the only writer for tasks.json status.
//
// The schema lives in
// github.com/mandarnilange/corvee/internal/tasksdoc so
// the validator and this writer cannot drift.
//
// Usage:
//
//	tasks-mark --init <path>                            # set initial status
//	                                                      on every phase and
//	                                                      story per the
//	                                                      bootstrap rules.
//	tasks-mark <path> <id> <status> [--completed-at <iso>]
//
// Bootstrap rules (used by --init): TTR-E00 + its 10 stories are
// 'done' with completed_at = the project's bootstrap date. TTR-E01 +
// its 19 stories are 'ready' (next up). TTR-E02..TTR-E08 + their
// stories are 'backlog'.
//
// Single-id mode: when status=done, completed_at defaults to time.Now()
// in UTC unless --completed-at provides an explicit RFC3339 value. For
// any other status, --completed-at must NOT be set; the validator
// rejects orphaned timestamps.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mandarnilange/corvee/internal/tasksdoc"
)

const bootstrapCompletedAt = "2026-05-05T00:00:00Z"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	if os.Args[1] == "--init" {
		if len(os.Args) != 3 {
			usage()
			os.Exit(2)
		}
		if err := runInit(os.Args[2]); err != nil {
			die("init: %v", err)
		}
		return
	}

	if len(os.Args) < 4 {
		usage()
		os.Exit(2)
	}
	path, id, status := os.Args[1], os.Args[2], os.Args[3]

	fs := flag.NewFlagSet("tasks-mark", flag.ContinueOnError)
	completedAt := fs.String("completed-at", "", "RFC3339 timestamp; defaults to now if status=done")
	if err := fs.Parse(os.Args[4:]); err != nil {
		die("flags: %v", err)
	}
	if err := runMark(path, id, status, *completedAt); err != nil {
		die("mark: %v", err)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage:")
	fmt.Fprintln(os.Stderr, "  tasks-mark --init <path>")
	fmt.Fprintln(os.Stderr, "  tasks-mark <path> <id> <status> [--completed-at <iso>]")
}

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "tasks-mark: "+format+"\n", args...)
	os.Exit(1)
}

func runInit(path string) error {
	doc, err := tasksdoc.Load(path)
	if err != nil {
		return err
	}
	for i, p := range doc.Phases {
		applyBootstrapPhase(&p)
		for j, t := range p.Tasks {
			applyBootstrapTask(&t)
			p.Tasks[j] = t
		}
		doc.Phases[i] = p
	}
	if err := doc.Validate(); err != nil {
		return fmt.Errorf("post-init validate: %w", err)
	}
	return tasksdoc.Save(path, doc)
}

func applyBootstrapPhase(p *tasksdoc.Phase) {
	switch {
	case p.ID == "TTR-E00":
		p.Status, p.CompletedAt = "done", bootstrapCompletedAt
	case p.ID == "TTR-E01":
		p.Status, p.CompletedAt = "ready", ""
	default:
		p.Status, p.CompletedAt = "backlog", ""
	}
}

func applyBootstrapTask(t *tasksdoc.Task) {
	switch {
	case strings.HasPrefix(t.ID, "TTR-E00-"):
		t.Status, t.CompletedAt = "done", bootstrapCompletedAt
	case strings.HasPrefix(t.ID, "TTR-E01-"):
		t.Status, t.CompletedAt = "ready", ""
	default:
		t.Status, t.CompletedAt = "backlog", ""
	}
}

func runMark(path, id, status, completedAt string) error {
	if !tasksdoc.AllowedStatuses[status] {
		return fmt.Errorf("status %q is not in spec §5 enum", status)
	}
	if status == "done" && completedAt == "" {
		completedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if status != "done" && completedAt != "" {
		return fmt.Errorf("--completed-at is only valid when status=done")
	}

	doc, err := tasksdoc.Load(path)
	if err != nil {
		return err
	}

	updated := false
	for i := range doc.Phases {
		p := &doc.Phases[i]
		if p.ID == id {
			p.Status, p.CompletedAt = status, ""
			if status == "done" {
				p.CompletedAt = completedAt
			}
			updated = true
			break
		}
		for j := range p.Tasks {
			t := &p.Tasks[j]
			if t.ID == id {
				t.Status, t.CompletedAt = status, ""
				if status == "done" {
					t.CompletedAt = completedAt
				}
				updated = true
				break
			}
		}
		if updated {
			break
		}
	}
	if !updated {
		return fmt.Errorf("id %q not found", id)
	}
	if err := doc.Validate(); err != nil {
		return fmt.Errorf("post-mark validate: %w", err)
	}
	if err := tasksdoc.Save(path, doc); err != nil {
		return err
	}
	fmt.Printf("tasks-mark: %s -> %s\n", id, status)
	return nil
}
