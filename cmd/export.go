package cmd

import (
	"fmt"
	"os"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/ybonda/memo/internal/vault"
)

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Sync all memories to the Obsidian vault",
	Long: `Rewrites every memory into the configured vault directory as Markdown files.

The vault is a one-way projection of the DB: memo remember / update / forget
already sync single files automatically. This command is for full rebuilds,
first-time setup, or recovery after pointing vault_path at a new location.

Orphan .md files (those whose short-id does not match any memory in the DB)
are pruned. User-authored files whose names do not start with an 8-hex
short-id are left untouched.

With --rename, slugs that were frozen at first export are regenerated from
current content — useful after heavy content rewrites. Without it, filenames
remain stable so Obsidian wikilinks are not invalidated.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		rename, _ := cmd.Flags().GetBool("rename")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		result, err := memStore.ExportVault(vault.ExportOptions{
			Rename: rename,
			DryRun: dryRun,
		})
		if err != nil {
			return err
		}

		if useJSON() {
			return outputJSON(result)
		}
		printExportSummary(result)
		return nil
	},
}

func init() {
	exportCmd.Flags().Bool("rename", false, "Regenerate slugs from current content (may rename files)")
	exportCmd.Flags().Bool("dry-run", false, "Print actions without touching the filesystem")
	rootCmd.AddCommand(exportCmd)
}

func printExportSummary(r *vault.ExportResult) {
	bold := color.New(color.Bold).SprintFunc()
	dim := color.New(color.FgHiBlack).SprintFunc()
	green := color.New(color.FgGreen).SprintFunc()
	yellow := color.New(color.FgYellow).SprintFunc()
	red := color.New(color.FgRed).SprintFunc()

	prefix := ""
	if r.DryRun {
		prefix = yellow("[dry-run] ")
	}

	fmt.Fprintf(os.Stdout, "%s%s %s\n",
		prefix,
		bold("vault:"),
		r.Path,
	)
	fmt.Fprintf(os.Stdout, "  %s %d  %s %d  %s %d  %s %d  %s %d  %s %d\n",
		green("wrote"), r.Wrote,
		green("updated"), r.Updated,
		yellow("renamed"), r.Renamed,
		yellow("moved"), r.Moved,
		red("deleted"), r.Deleted,
		dim("unchanged"), r.Unchanged,
	)
}
