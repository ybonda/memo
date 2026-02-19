package cmd

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/ybonda/memo/internal/format"
	"github.com/ybonda/memo/internal/model"
)

var similarCmd = &cobra.Command{
	Use:   "similar",
	Short: "Find similar memories",
	RunE: func(cmd *cobra.Command, args []string) error {
		content, _ := cmd.Flags().GetString("content")

		results, err := memStore.FindSimilar(content)
		if err != nil {
			return err
		}
		if results == nil {
			results = []model.MemoryWithScore{}
		}
		if useJSON() {
			return outputJSON(results)
		}
		format.PrintMemoriesWithScore(os.Stdout, results)
		return nil
	},
}

func init() {
	similarCmd.Flags().String("content", "", "Content to find similar memories for")
	similarCmd.MarkFlagRequired("content")
	rootCmd.AddCommand(similarCmd)
}
