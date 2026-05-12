package domain

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/xeipuuv/gojsonschema"
)

func TestValidate_Section5FixturePasses(t *testing.T) {
	t.Parallel()
	var item Item
	if err := json.Unmarshal([]byte(fixtureSection5), &item); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if err := Validate(item); err != nil {
		t.Fatalf("Validate(§5 fixture): %v", err)
	}
}

func TestValidate_RequiredFieldAbsenceFails(t *testing.T) {
	t.Parallel()
	// Each iteration drops a single required field and asserts schema
	// validation fails with ErrSchemaInvalid mentioning that field.
	required := []string{"schema_version", "id", "type", "title", "status", "created_at", "updated_at", "version"}
	var base Item
	if err := json.Unmarshal([]byte(fixtureSection5), &base); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	for _, field := range required {
		field := field
		t.Run("missing/"+field, func(t *testing.T) {
			t.Parallel()
			data, mErr := json.Marshal(base)
			if mErr != nil {
				t.Fatal(mErr)
			}
			var asMap map[string]any
			if uErr := json.Unmarshal(data, &asMap); uErr != nil {
				t.Fatal(uErr)
			}
			delete(asMap, field)
			modified, mErr2 := json.Marshal(asMap)
			if mErr2 != nil {
				t.Fatal(mErr2)
			}
			vErr := validateRawJSON(modified)
			if !errors.Is(vErr, ErrSchemaInvalid) {
				t.Fatalf("missing %s: err=%v, want ErrSchemaInvalid", field, vErr)
			}
			if !strings.Contains(vErr.Error(), field) {
				t.Errorf("error message %q should mention the missing field %q", vErr, field)
			}
		})
	}
}

func TestValidate_ForwardVersionGuardRejects(t *testing.T) {
	t.Parallel()
	var item Item
	if err := json.Unmarshal([]byte(fixtureSection5), &item); err != nil {
		t.Fatal(err)
	}
	item.SchemaVersion = CurrentSchemaVersion + 1
	err := Validate(item)
	if !errors.Is(err, ErrSchemaInvalid) {
		t.Fatalf("err=%v, want ErrSchemaInvalid", err)
	}
	if !strings.Contains(err.Error(), "upgrade the task CLI") {
		t.Errorf("error must prompt the upgrade; got %q", err)
	}
}

func TestValidate_BadStatusEnumRejected(t *testing.T) {
	t.Parallel()
	var item Item
	if err := json.Unmarshal([]byte(fixtureSection5), &item); err != nil {
		t.Fatal(err)
	}
	item.Status = "stalled" // not in the enum
	err := Validate(item)
	if !errors.Is(err, ErrSchemaInvalid) {
		t.Fatalf("err=%v, want ErrSchemaInvalid", err)
	}
}

func TestValidate_BadIDPatternRejected(t *testing.T) {
	t.Parallel()
	var item Item
	if err := json.Unmarshal([]byte(fixtureSection5), &item); err != nil {
		t.Fatal(err)
	}
	item.ID = "lower-case"
	err := Validate(item)
	if !errors.Is(err, ErrSchemaInvalid) {
		t.Fatalf("err=%v, want ErrSchemaInvalid", err)
	}
}

func TestValidate_UnknownTopLevelFieldRejected(t *testing.T) {
	t.Parallel()
	// We can't add a stray field via the typed Item, so build raw JSON.
	raw := []byte(`{
        "schema_version": 1,
        "id": "RKN",
        "type": "project",
        "title": "p",
        "status": "backlog",
        "created_at": "2026-05-01T00:00:00Z",
        "updated_at": "2026-05-01T00:00:00Z",
        "version": 1,
        "extra_unknown": "boom"
    }`)
	if err := validateRawJSON(raw); !errors.Is(err, ErrSchemaInvalid) {
		t.Fatalf("err=%v, want ErrSchemaInvalid", err)
	}
}

// validateRawJSON validates a raw JSON payload against the embedded
// schema without round-tripping through Item. Used by tests that need
// to construct intentionally-invalid documents (missing required
// fields, unknown fields).
func validateRawJSON(raw []byte) error {
	result, err := itemSchema.Validate(gojsonschema.NewBytesLoader(raw))
	if err != nil {
		return errors.Join(ErrSchemaInvalid, err)
	}
	if result.Valid() {
		return nil
	}
	var msg strings.Builder
	for i, e := range result.Errors() {
		if i > 0 {
			msg.WriteString("; ")
		}
		field := e.Field()
		if field == "" {
			field = "(root)"
		}
		msg.WriteString(field)
		msg.WriteString(": ")
		msg.WriteString(e.Description())
	}
	// Mirror Validate()'s wrapping so errors.Is(err, ErrSchemaInvalid)
	// works in test assertions.
	return errors.Join(ErrSchemaInvalid, errors.New("validate raw: "+msg.String()))
}
