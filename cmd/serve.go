package cmd

import (
	"github.com/spf13/cobra"

	"github.com/yuri-bondarenko/memo/internal/config"
	mcpserver "github.com/yuri-bondarenko/memo/internal/mcp"
	"github.com/yuri-bondarenko/memo/internal/store"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start MCP server over stdio",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return nil // bypass root's store init — serve manages its own lifecycle
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		s, err := store.New(cfg)
		if err != nil {
			return err
		}
		defer s.Close()
		return mcpserver.Serve(s, cfg)
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)
}
