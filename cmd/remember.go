package cmd

import (
	"strings"

	"github.com/spf13/cobra"
)

var rememberCmd = &cobra.Command{
	Use:   "remember",
	Short: "Store a memory",
	RunE: func(cmd *cobra.Command, args []string) error {
		content, _ := cmd.Flags().GetString("content")
		memType, _ := cmd.Flags().GetString("type")
		tagsStr, _ := cmd.Flags().GetString("tags")

		var tags []string
		if tagsStr != "" {
			tags = strings.Split(tagsStr, ",")
			for i := range tags {
				tags[i] = strings.TrimSpace(tags[i])
			}
		}

		result, err := memStore.Store(content, tags, memType)
		if err != nil {
			return err
		}
		return outputJSON(result)
	},
}

func init() {
	rememberCmd.Flags().String("content", "", "The content to remember")
	rememberCmd.Flags().String("type", "", "Memory type")
	rememberCmd.Flags().String("tags", "", "Comma-separated tags")
	rememberCmd.MarkFlagRequired("content")
	rootCmd.AddCommand(rememberCmd)
}
