package cli

import (
	"encoding/json"
	"fmt"
	"io"
)

// outputFormat is the per-invocation output mode chosen via global flags.
type outputFormat int

const (
	// outputCompact is the default: a single line of JSON.
	outputCompact outputFormat = iota
	// outputPretty emits indented JSON.
	outputPretty
	// outputText emits a human-readable text rendering.
	outputText
)

// successEnvelope is the success-path payload shape from spec §15.1:
//
//	{"ok": true, "data": <command-specific>}
type successEnvelope struct {
	OK   bool `json:"ok"`
	Data any  `json:"data"`
}

// printJSON writes the success envelope to w in the configured format.
// Compact JSON is the default; --pretty produces indented JSON; --text
// is verb-specific (callers supply a textRenderer when they support it).
func printJSON(w io.Writer, format outputFormat, data any) error {
	env := successEnvelope{OK: true, Data: data}

	switch format {
	case outputPretty:
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(env)
	case outputText:
		// Default text rendering is just compact JSON. Verbs that want
		// custom text output should use printText with a renderer.
		fallthrough
	case outputCompact:
		fallthrough
	default:
		buf, err := json.Marshal(env)
		if err != nil {
			return fmt.Errorf("marshal output: %w", err)
		}
		buf = append(buf, '\n')
		_, err = w.Write(buf)
		return err
	}
}
