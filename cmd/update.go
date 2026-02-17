package cmd

import (
	"strings"

	"github.com/spf13/cobra"
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update an existing memory",
	RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")

		var content *string
		if cmd.Flags().Changed("content") {
			v, _ := cmd.Flags().GetString("content")
			content = &v
		}

		var tags *[]string
		if cmd.Flags().Changed("tags") {
			v, _ := cmd.Flags().GetString("tags")
			parts := strings.Split(v, ",")
			for i := range parts {
				parts[i] = strings.TrimSpace(parts[i])
			}
			tags = &parts
		}

		var memType *string
		if cmd.Flags().Changed("type") {
			v, _ := cmd.Flags().GetString("type")
			memType = &v
		}

		result, err := memStore.Update(id, content, tags, memType)
		if err != nil {
			return err
		}
		return outputJSON(result)
	},
}

func init() {
	updateCmd.Flags().String("id", "", "Memory ID to update")
	updateCmd.Flags().String("content", "", "New content")
	updateCmd.Flags().String("tags", "", "New comma-separated tags")
	updateCmd.Flags().String("type", "", "New memory type")
	updateCmd.MarkFlagRequired("id")
	rootCmd.AddCommand(updateCmd)
}
