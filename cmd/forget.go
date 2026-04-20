package cmd

import "github.com/spf13/cobra"

var forgetCmd = &cobra.Command{
	Use:   "forget",
	Short: "Delete a memory by full UUID or 8-hex short-id",
	RunE: func(cmd *cobra.Command, args []string) error {
		idOrShort, _ := cmd.Flags().GetString("id")
		id, err := memStore.ResolveID(idOrShort)
		if err != nil {
			return err
		}
		result, err := memStore.Delete(id)
		if err != nil {
			return err
		}
		return outputJSON(result)
	},
}

func init() {
	forgetCmd.Flags().String("id", "", "Memory ID (full UUID or 8-hex short-id prefix)")
	forgetCmd.MarkFlagRequired("id")
	rootCmd.AddCommand(forgetCmd)
}
