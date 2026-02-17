package cmd

import (
	"github.com/spf13/cobra"

	"github.com/yuri-bondarenko/memo/internal/model"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List memories",
	RunE: func(cmd *cobra.Command, args []string) error {
		limit, _ := cmd.Flags().GetInt("limit")
		memType, _ := cmd.Flags().GetString("type")

		var typeFilter *string
		if cmd.Flags().Changed("type") {
			typeFilter = &memType
		}

		results, err := memStore.List(limit, typeFilter)
		if err != nil {
			return err
		}
		if results == nil {
			results = []model.Memory{}
		}
		return outputJSON(results)
	},
}

func init() {
	listCmd.Flags().Int("limit", 50, "Maximum results")
	listCmd.Flags().String("type", "", "Filter by type")
	rootCmd.AddCommand(listCmd)
}
