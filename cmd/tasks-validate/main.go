// Command tasks-validate checks .spec/tasks.json for structural integrity.
//
// This is a Phase 0 stand-in for what `corvee import --dry-run` will do once
// it ships in Phase 7. The Phase 0 spec exit gate (see CLAUDE.md and the
// implementation plan) requires .spec/tasks.json to enumerate every story
// across every phase with valid dependencies and non-empty acceptance
// criteria. Running this binary as part of `make ci` enforces that.
//
// The schema definition (and its enforcement) lives in
// github.com/mandarnilange/corvee/internal/tasksdoc so
// cmd/tasks-mark cannot drift from the validator.
//
// Once Phase 7 ships, this entire command can be deleted; `corvee import`
// will subsume the checks.
package main

import (
	"fmt"
	"os"
	"sort"

	"github.com/mandarnilange/corvee/internal/tasksdoc"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: tasks-validate <path>")
		os.Exit(2)
	}

	doc, err := tasksdoc.Load(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "tasks-validate: %v\n", err)
		os.Exit(1)
	}
	if err := doc.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "tasks-validate: %v\n", err)
		os.Exit(1)
	}

	printSummary(doc)
	fmt.Println("tasks-validate: OK")
}

func printSummary(doc *tasksdoc.Doc) {
	statusCount := map[string]int{}
	phaseStatus := map[string]string{}
	for _, p := range doc.Phases {
		phaseStatus[p.ID] = p.Status
		for _, t := range p.Tasks {
			statusCount[t.Status]++
		}
	}

	keys := make([]string, 0, len(tasksdoc.ExpectedStoriesPerPhase))
	for k := range tasksdoc.ExpectedStoriesPerPhase {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	total := 0
	for _, k := range keys {
		total += tasksdoc.ExpectedStoriesPerPhase[k]
		fmt.Printf("  %s [%s]: %d stories\n", k, phaseStatus[k], tasksdoc.ExpectedStoriesPerPhase[k])
	}
	fmt.Printf("  total: %d stories across %d phases\n", total, len(tasksdoc.ExpectedStoriesPerPhase))

	statusKeys := make([]string, 0, len(statusCount))
	for k := range statusCount {
		statusKeys = append(statusKeys, k)
	}
	sort.Strings(statusKeys)
	for _, s := range statusKeys {
		fmt.Printf("  story status %-12s: %d\n", s, statusCount[s])
	}
}
