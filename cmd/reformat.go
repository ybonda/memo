package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var reformatCmd = &cobra.Command{
	Use:   "reformat <id-or-short-id>",
	Short: "Re-run the LLM markdown pass on a single memory (requires llm_md_export.enabled)",
	Long: `Re-invokes the configured claude CLI on exactly one memory's raw content
and overwrites its cached rendered_body in the DB, then rewrites the vault
file. Useful after tweaking the LLM prompt, after enabling llm_md_export on
a DB that was built deterministic-only, or after switching the model.

The argument accepts any of:
  - 8-hex short-id:       memo reformat 24b9e78e
  - short-id + tail:      memo reformat 24b9e78e-gcp-pam-privileged-access-manager
  - .md-suffixed:         memo reformat 24b9e78e-gcp-pam-privileged-access-manager.md
  - full UUID:            memo reformat 24b9e78e-c741-644d-0bc6-4f05f3af640a

Bulk reformat is intentionally NOT supported. Reformat is deliberate and
per-memory because a single pass can burn significant subscription quota and
a bad prompt change could otherwise cascade across your whole knowledge base.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := memStore.ResolveID(args[0])
		if err != nil {
			return err
		}
		result, err := memStore.ReformatOne(id)
		if err != nil {
			return err
		}
		if !useJSON() {
			status := "rendered"
			if result.Skipped {
				status = "skipped (no change)"
			}
			fmt.Printf("%s  %s\n  %s\n", result.ID, status, result.Title)
			return nil
		}
		return outputJSON(result)
	},
}

func init() {
	rootCmd.AddCommand(reformatCmd)
}
