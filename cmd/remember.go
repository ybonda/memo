package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ybonda/memo/internal/capture"
)

var rememberCmd = &cobra.Command{
	Use:   "remember",
	Short: "Store a memory",
	RunE: func(cmd *cobra.Command, args []string) error {
		content, _ := cmd.Flags().GetString("content")
		memType, _ := cmd.Flags().GetString("type")
		tagsStr, _ := cmd.Flags().GetString("tags")
		ticket, _ := cmd.Flags().GetString("ticket")
		pr, _ := cmd.Flags().GetString("pr")
		related, _ := cmd.Flags().GetStringArray("related")
		contextPairs, _ := cmd.Flags().GetStringArray("context")

		var tags []string
		if tagsStr != "" {
			tags = strings.Split(tagsStr, ",")
			for i := range tags {
				tags[i] = strings.TrimSpace(tags[i])
			}
		}

		ctx := buildContextMap(ticket, pr, related, contextPairs)
		result, err := memStore.Store(content, tags, memType, ctx)
		if err != nil {
			return err
		}
		return outputJSON(result)
	},
}

// buildContextMap merges capture-time context from three sources, in order
// of increasing precedence:
//  1. Ambient git context (branch, commit, project, cwd_name) if enabled.
//  2. Dedicated flags (--ticket, --pr, --related).
//  3. Free-form --context key=value pairs (user-supplied escape hatch).
//
// Later sources overwrite earlier ones on the same key. Returns nil when
// nothing was populated so the store layer can skip serialisation.
func buildContextMap(ticket, pr string, related, contextPairs []string) map[string]string {
	out := map[string]string{}

	if cfg != nil && cfg.CaptureContext() {
		if cwd, err := os.Getwd(); err == nil {
			for k, v := range capture.Git(cwd) {
				out[k] = v
			}
		}
	}

	if ticket != "" {
		out["ticket"] = ticket
	}
	if pr != "" {
		out["pr"] = pr
	}
	if len(related) > 0 {
		out["related"] = strings.Join(related, ", ")
	}

	for _, pair := range contextPairs {
		key, value, ok := strings.Cut(pair, "=")
		if !ok || key == "" {
			fmt.Fprintf(os.Stderr, "[memo] ignoring malformed --context %q; expected key=value\n", pair)
			continue
		}
		out[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}

	if len(out) == 0 {
		return nil
	}
	return out
}

func init() {
	rememberCmd.Flags().String("content", "", "The content to remember")
	rememberCmd.Flags().String("type", "", "Memory type")
	rememberCmd.Flags().String("tags", "", "Comma-separated tags")
	rememberCmd.Flags().String("ticket", "", "Linked ticket id (e.g. OPS-43243)")
	rememberCmd.Flags().String("pr", "", "Linked PR identifier")
	rememberCmd.Flags().StringArray("related", nil, "Related memory short-id or ticket id (repeatable)")
	rememberCmd.Flags().StringArray("context", nil, "Extra context as key=value (repeatable)")
	rememberCmd.MarkFlagRequired("content")
	rootCmd.AddCommand(rememberCmd)
}
