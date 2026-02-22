package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/ybonda/memo/internal/config"
	"github.com/ybonda/memo/internal/store"
	"github.com/ybonda/memo/internal/version"
)

var (
	memStore *store.MemoryStore
	cfg      *config.Config
	jsonFlag bool
)

var rootCmd = &cobra.Command{
	Use:           "memo",
	Short:         "Personal memory layer with semantic search",
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		var err error
		cfg, err = config.Load()
		if err != nil {
			return err
		}
		memStore, err = store.New(cfg)
		if err != nil {
			return err
		}
		return nil
	},
	PersistentPostRun: func(cmd *cobra.Command, args []string) {
		if memStore != nil {
			memStore.Close()
		}
	},
}

func init() {
	rootCmd.Version = version.Version
	rootCmd.PersistentFlags().BoolVar(&jsonFlag, "json", false, "Output as JSON")
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		out, _ := json.Marshal(map[string]string{"error": err.Error()})
		fmt.Fprintln(os.Stdout, string(out))
		os.Exit(1)
	}
}

// useJSON returns true if JSON output was requested or stdout is not a terminal.
func useJSON() bool {
	return jsonFlag || !isatty.IsTerminal(os.Stdout.Fd())
}

func outputJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	return enc.Encode(v)
}
