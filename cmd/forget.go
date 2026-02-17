package cmd

import "github.com/spf13/cobra"

var forgetCmd = &cobra.Command{
	Use:   "forget",
	Short: "Delete a memory by ID",
	RunE: func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("id")
		result, err := memStore.Delete(id)
		if err != nil {
			return err
		}
		return outputJSON(result)
	},
}

func init() {
	forgetCmd.Flags().String("id", "", "Memory ID to delete")
	forgetCmd.MarkFlagRequired("id")
	rootCmd.AddCommand(forgetCmd)
}
