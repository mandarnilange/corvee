package domain

import (
	"embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xeipuuv/gojsonschema"
)

//go:embed schema/item.schema.json
var schemaFS embed.FS

// itemSchema is loaded once at init time so Validate doesn't reparse
// the schema on every call. Errors here are programmer mistakes
// (broken schema file shipped with the binary), so we panic — the
// only sane response is "fix the schema".
var itemSchema *gojsonschema.Schema

func init() {
	data, err := schemaFS.ReadFile("schema/item.schema.json")
	if err != nil {
		panic(fmt.Sprintf("domain: load embedded schema: %v", err))
	}
	loader := gojsonschema.NewBytesLoader(data)
	s, err := gojsonschema.NewSchema(loader)
	if err != nil {
		panic(fmt.Sprintf("domain: compile embedded schema: %v", err))
	}
	itemSchema = s
}

// Validate checks item against the embedded JSON Schema (draft-07).
// Returns nil on a valid item; otherwise wraps ErrSchemaInvalid with
// each violation's field path in the diagnostic. Forward-version
// guard: items whose schema_version exceeds CurrentSchemaVersion are
// rejected with an upgrade-prompt message before schema compilation
// runs (so older binaries never silently accept newer records).
func Validate(item Item) error {
	if item.SchemaVersion > CurrentSchemaVersion {
		return fmt.Errorf("validate %s: item written with schema_version=%d; this binary supports up to %d; please upgrade the task CLI: %w",
			item.ID, item.SchemaVersion, CurrentSchemaVersion, ErrSchemaInvalid)
	}

	data, err := json.Marshal(item)
	if err != nil {
		return fmt.Errorf("validate %s: marshal: %w", item.ID, err)
	}
	result, err := itemSchema.Validate(gojsonschema.NewBytesLoader(data))
	if err != nil {
		return fmt.Errorf("validate %s: %v: %w", item.ID, err, ErrSchemaInvalid)
	}
	if result.Valid() {
		return nil
	}

	var b strings.Builder
	for i, e := range result.Errors() {
		if i > 0 {
			b.WriteString("; ")
		}
		field := e.Field()
		if field == "" {
			field = "(root)"
		}
		fmt.Fprintf(&b, "%s: %s", field, e.Description())
	}
	return fmt.Errorf("validate %s: %s: %w", item.ID, b.String(), ErrSchemaInvalid)
}
