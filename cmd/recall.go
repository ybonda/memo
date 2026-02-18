package cmd

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/yuri-bondarenko/memo/internal/format"
)

var recallCmd = &cobra.Command{
	Use:   "recall",
	Short: "Recall context for a query",
	RunE: func(cmd *cobra.Command, args []string) error {
		query, _ := cmd.Flags().GetString("query")
		limit, _ := cmd.Flags().GetInt("limit")

		result, err := memStore.Recall(query, limit)
		if err != nil {
			return err
		}
		if useJSON() {
			return outputJSON(result)
		}
		format.PrintRecallResult(os.Stdout, *result)
		return nil
	},
}

func init() {
	recallCmd.Flags().String("query", "", "Recall query")
	recallCmd.Flags().Int("limit", 5, "Maximum memories")
	recallCmd.MarkFlagRequired("query")
	rootCmd.AddCommand(recallCmd)
}
