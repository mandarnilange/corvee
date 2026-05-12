package domain

import "time"

// Event is one append-only audit record persisted to the sharded
// JSONL log under .tasks/events/. Every mutation usecase emits an
// Event so the workspace history is reconstructable from the log
// alone.
//
// Schema is intentionally narrow — fields that vary by event type are
// encoded in Metadata rather than typed.
type Event struct {
	// EventID is the ULID minted at append time. Sortable; serves as
	// a primary key for replay.
	EventID string `json:"event_id"`
	// Timestamp is the wall-clock time the event was recorded.
	Timestamp time.Time `json:"ts"`
	// Type is the verb describing the event (e.g. "item_added",
	// "status_changed", "claimed", "released"). Schema is open-ended
	// per spec §6.
	Type string `json:"type"`
	// ItemID is the ID of the item the event references. Empty for
	// workspace-level events (e.g. "init").
	ItemID string `json:"item_id,omitempty"`
	// Actor is the agent ID that produced the event.
	Actor string `json:"actor,omitempty"`
	// ActorRole is the role the actor was acting in.
	ActorRole Role `json:"actor_role,omitempty"`
	// OperationID is the idempotency key (when present) so replays of
	// a multi-file operation can be deduplicated by EventLog readers.
	OperationID string `json:"operation_id,omitempty"`
	// LeaseID is the lease this event is scoped to, when applicable.
	LeaseID string `json:"lease_id,omitempty"`
	// Metadata is arbitrary structured payload (per-type-specific
	// fields). The log stores it verbatim.
	Metadata map[string]any `json:"metadata,omitempty"`
}
