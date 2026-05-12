package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// TestPrintJSON_FormatsByMode covers the matrix of supported output formats.
// The success envelope shape is locked by spec §15.1; this test fails if a
// future change accidentally drops the "ok" wrapper or stops trailing
// compact output with a newline.
func TestPrintJSON_FormatsByMode(t *testing.T) {
	type payload struct {
		Greeting string `json:"greeting"`
	}
	in := payload{Greeting: "hi"}

	cases := []struct {
		name   string
		format outputFormat
		// containsAll lists substrings the output must contain.
		containsAll []string
		// indented signals the output should include a newline character.
		indented bool
	}{
		{
			name:        "compact has ok envelope and trailing newline",
			format:      outputCompact,
			containsAll: []string{`"ok":true`, `"greeting":"hi"`, "\n"},
			indented:    false,
		},
		{
			name:        "pretty produces indented JSON",
			format:      outputPretty,
			containsAll: []string{`"ok": true`, `"greeting": "hi"`},
			indented:    true,
		},
		{
			name:        "text falls through to compact (Phase 0 has no text renderer)",
			format:      outputText,
			containsAll: []string{`"ok":true`, `"greeting":"hi"`},
			indented:    false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			if err := printJSON(&buf, tc.format, in); err != nil {
				t.Fatalf("printJSON: %v", err)
			}

			out := buf.String()
			for _, want := range tc.containsAll {
				if !strings.Contains(out, want) {
					t.Errorf("output %q missing substring %q", out, want)
				}
			}

			// Compact mode: exactly one trailing newline, no other
			// whitespace lines.
			if !tc.indented {
				if !strings.HasSuffix(out, "\n") {
					t.Errorf("compact output missing trailing newline: %q", out)
				}
				if strings.Count(out, "\n") != 1 {
					t.Errorf("compact output has multiple newlines: %q", out)
				}
			}

			// Both modes must be valid JSON.
			var got struct {
				OK   bool    `json:"ok"`
				Data payload `json:"data"`
			}
			if err := json.Unmarshal([]byte(out), &got); err != nil {
				t.Fatalf("output not valid JSON: %v\noutput=%q", err, out)
			}
			if !got.OK {
				t.Errorf("envelope ok=false; want true")
			}
			if got.Data.Greeting != "hi" {
				t.Errorf("data.greeting=%q; want %q", got.Data.Greeting, "hi")
			}
		})
	}
}

// TestPrintJSON_PropagatesWriterError ensures write failures aren't swallowed.
func TestPrintJSON_PropagatesWriterError(t *testing.T) {
	t.Parallel()

	err := printJSON(&failingWriter{}, outputCompact, struct{}{})
	if err == nil {
		t.Fatal("expected error from failing writer")
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errFailWrite }

var errFailWrite = errIOFail("write failed")

type errIOFail string

func (e errIOFail) Error() string { return string(e) }
