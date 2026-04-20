package cmd

import (
	"fmt"
	"os"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/ybonda/memo/internal/store"
)

var reconcileCmd = &cobra.Command{
	Use:   "reconcile",
	Short: "Reflect Obsidian deletes and type-folder moves back into the DB",
	Long: `Walks the vault and diffs it against the DB:
- files missing from the vault cause the matching memories to be deleted
- files moved to a different type folder cause the memory's type to be updated
- file-body edits are ignored (the vault is a one-way projection for content)

Runs as a dry-run by default. Pass --apply to commit the changes. User-
authored .md files (those not starting with an 8-hex short-id) are ignored
throughout, as are files nested more than one level below the vault root.

Deletes are permanent. Type changes do not regenerate the embedding, since
content is unchanged.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		apply, _ := cmd.Flags().GetBool("apply")

		result, err := memStore.ReconcileVault(store.ReconcileOptions{Apply: apply})
		if err != nil {
			return err
		}

		if useJSON() {
			return outputJSON(result)
		}
		printReconcileSummary(result, apply)
		return nil
	},
}

func init() {
	reconcileCmd.Flags().Bool("apply", false, "Commit the diff to the DB (otherwise dry-run)")
	reconcileCmd.Flags().Bool("dry-run", true, "Preview without mutating (default)")
	rootCmd.AddCommand(reconcileCmd)
}

func printReconcileSummary(r *store.ReconcileResult, apply bool) {
	bold := color.New(color.Bold).SprintFunc()
	dim := color.New(color.FgHiBlack).SprintFunc()
	green := color.New(color.FgGreen).SprintFunc()
	yellow := color.New(color.FgYellow).SprintFunc()
	red := color.New(color.FgRed).SprintFunc()

	prefix := yellow("[dry-run] ")
	if apply {
		if r.Applied {
			prefix = green("[applied] ")
		} else {
			prefix = red("[failed] ")
		}
	}

	fmt.Fprintf(os.Stdout, "%s%s\n", prefix, bold("vault reconcile"))
	fmt.Fprintf(os.Stdout, "  %s %d  %s %d  %s %d\n",
		red("to delete"), len(r.ToDelete),
		yellow("type changes"), len(r.TypeChanges),
		dim("unknown"), len(r.Unknown),
	)

	if len(r.ToDelete) > 0 {
		fmt.Fprintf(os.Stdout, "\n%s\n", bold("Deletions:"))
		for _, item := range r.ToDelete {
			fmt.Fprintf(os.Stdout, "  %s %s  %s\n",
				red("-"), dim(shortID(item.ID)), item.Title)
		}
	}
	if len(r.TypeChanges) > 0 {
		fmt.Fprintf(os.Stdout, "\n%s\n", bold("Type changes:"))
		for _, tc := range r.TypeChanges {
			fmt.Fprintf(os.Stdout, "  %s %s  %s → %s  %s\n",
				yellow("~"), dim(shortID(tc.ID)),
				dim(tc.OldType), tc.NewType,
				tc.Title)
		}
	}
	if len(r.Unknown) > 0 {
		fmt.Fprintf(os.Stdout, "\n%s (files with memo-shape basename but no DB row; not deleted)\n",
			bold("Unknown short-ids:"))
		for _, s := range r.Unknown {
			fmt.Fprintf(os.Stdout, "  %s %s\n", dim("?"), s)
		}
	}

	if !apply && (len(r.ToDelete) > 0 || len(r.TypeChanges) > 0) {
		fmt.Fprintf(os.Stdout, "\n%s\n", dim("Run with --apply to commit."))
	}
}

func shortID(id string) string {
	if len(id) >= 8 {
		return id[:8]
	}
	return id
}
