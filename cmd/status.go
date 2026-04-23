package cmd

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/ybonda/memo/internal/format"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show memo stats, paths, vault drift, and recent async render errors",
	RunE: func(cmd *cobra.Command, args []string) error {
		info, err := memStore.Status()
		if err != nil {
			return err
		}
		if useJSON() {
			return outputJSON(info)
		}
		format.PrintStatus(os.Stdout, info)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
