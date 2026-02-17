package cmd

import (
	"github.com/spf13/cobra"

	"github.com/yuri-bondarenko/memo/internal/model"
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
		return outputJSON(results)
	},
}

func init() {
	similarCmd.Flags().String("content", "", "Content to find similar memories for")
	similarCmd.MarkFlagRequired("content")
	rootCmd.AddCommand(similarCmd)
}
