package cmd

import (
	"github.com/spf13/cobra"

	"github.com/yuri-bondarenko/memo/internal/model"
)

var searchCmd = &cobra.Command{
	Use:   "search",
	Short: "Semantic search for memories",
	RunE: func(cmd *cobra.Command, args []string) error {
		query, _ := cmd.Flags().GetString("query")
		limit, _ := cmd.Flags().GetInt("limit")
		memType, _ := cmd.Flags().GetString("type")

		var typeFilter *string
		if cmd.Flags().Changed("type") {
			typeFilter = &memType
		}

		results, err := memStore.Search(query, limit, typeFilter)
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
	searchCmd.Flags().String("query", "", "Search query")
	searchCmd.Flags().Int("limit", 5, "Maximum results")
	searchCmd.Flags().String("type", "", "Filter by type")
	searchCmd.MarkFlagRequired("query")
	rootCmd.AddCommand(searchCmd)
}
