package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mandarnilange/corvee/internal/domain"
	"github.com/mandarnilange/corvee/internal/usecase"
)

// newWatchCmd builds `corvee watch`. Streams events from the audit log
// per spec §15.2 — one event per line, no top-level envelope. The
// stream terminates on Ctrl+C, --limit, --exit-on, or upstream channel
// close.
func newWatchCmd(deps Deps) *cobra.Command {
	var (
		typesFlag  []string
		actorsFlag []string
		idsFlag    []string
		fromFlag   []string
		toFlag     []string
		opIDsFlag  []string
		metaFlag   []string
		sinceFlag  string
		limitFlag  int
		exitOnFlag []string
		formatFlag string
	)
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Stream events from the audit log (one event per line)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			format, err := parseWatchFormat(formatFlag)
			if err != nil {
				return err
			}

			metaMatch, err := parseMetaFlag(metaFlag)
			if err != nil {
				return err
			}

			in := usecase.WatchInput{
				Filter: domain.EventFilter{
					Types:         typesFlag,
					Actors:        actorsFlag,
					IDs:           idsFlag,
					From:          fromFlag,
					To:            toFlag,
					OperationIDs:  opIDsFlag,
					MetadataMatch: metaMatch,
				},
				Limit:  limitFlag,
				ExitOn: exitOnFlag,
			}
			if sinceFlag != "" {
				now := deps.Usecase.Clock.Now()
				since, perr := ParseSince(sinceFlag, now)
				if perr != nil {
					return perr
				}
				in.Since = since
				in.HasSince = true
			}

			ch, err := usecase.Watch(cmd.Context(), deps.Usecase, in)
			if err != nil {
				return err
			}
			for ev := range ch {
				if werr := writeWatchEvent(deps.Stdout, format, ev); werr != nil {
					return werr
				}
			}
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&typesFlag, "event", nil, "Filter by event type (repeatable, CSV OR)")
	cmd.Flags().StringSliceVar(&actorsFlag, "actor", nil, "Filter by actor (repeatable, CSV OR)")
	cmd.Flags().StringSliceVar(&idsFlag, "id", nil, "Filter by item id (prefix match includes descendants)")
	cmd.Flags().StringSliceVar(&fromFlag, "from", nil, "Filter by status_changed.from (CSV OR)")
	cmd.Flags().StringSliceVar(&toFlag, "to", nil, "Filter by status_changed.to (CSV OR)")
	cmd.Flags().StringSliceVar(&opIDsFlag, "op-id", nil, "Filter by operation_id (CSV OR)")
	cmd.Flags().StringArrayVar(&metaFlag, "meta", nil, "Filter by arbitrary metadata key (--meta key=value, repeatable; multiple values for the same key OR)")
	cmd.Flags().StringVar(&sinceFlag, "since", "", "Replay from this point: ISO timestamp, duration (5m), or today/yesterday")
	cmd.Flags().IntVar(&limitFlag, "limit", 0, "Stop after N events (0 = unlimited)")
	cmd.Flags().StringSliceVar(&exitOnFlag, "exit-on", nil, "Terminate after seeing one of these event types (CSV OR)")
	cmd.Flags().StringVar(&formatFlag, "format", "jsonl", "Output format: jsonl|text|compact")
	return cmd
}

// parseMetaFlag turns a slice of "key=value" strings into the
// MetadataMatch map. Repeats of the same key OR their values, distinct
// keys AND. Returns ErrUsage when an entry is missing the "=" separator
// or when the key is empty.
func parseMetaFlag(entries []string) (map[string][]string, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	out := make(map[string][]string, len(entries))
	for _, raw := range entries {
		idx := strings.IndexByte(raw, '=')
		if idx <= 0 {
			return nil, fmt.Errorf("--meta=%q must be key=value with a non-empty key: %w", raw, domain.ErrUsage)
		}
		key := raw[:idx]
		val := raw[idx+1:]
		out[key] = append(out[key], val)
	}
	return out, nil
}
