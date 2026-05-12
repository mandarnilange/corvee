// Package domain holds pure business types and rules.
//
// Per the clean-architecture layering (see CLAUDE.md and the spec §2.1),
// this package imports nothing from the rest of the codebase. It defines
// the Item schema, ID parsing, status-transition rules, lease validity,
// and the interface boundaries (ports) that outer layers implement.
package domain
