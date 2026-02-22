package cmd

import (
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"

	"github.com/ybonda/memo/internal/config"
	mcpserver "github.com/ybonda/memo/internal/mcp"
	"github.com/ybonda/memo/internal/store"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start MCP server over stdio",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return nil // bypass root's store init — serve manages its own lifecycle
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		// Redirect fd 1 (stdout) → fd 2 (stderr) at the OS level so that
		// C libraries (GoMLX/hugot via CGO) cannot corrupt the JSON-RPC
		// stream. Go-level os.Stdout swap alone is insufficient because
		// C code writes directly to POSIX fd 1.
		mcpFd, err := syscall.Dup(1) // save original stdout fd
		if err != nil {
			return fmt.Errorf("dup stdout: %w", err)
		}
		if err := unix.Dup2(2, 1); err != nil { // fd 1 → stderr
			return fmt.Errorf("dup2 stderr→stdout: %w", err)
		}
		mcpOut := os.NewFile(uintptr(mcpFd), "mcp-stdout")
		defer mcpOut.Close()
		os.Stdout = os.Stderr // also redirect Go-level for safety

		log := func(msg string, args ...any) {
			fmt.Fprintf(os.Stderr, "[memo-serve] "+msg+"\n", args...)
		}

		t0 := time.Now()
		cfg, err := config.Load()
		if err != nil {
			log("config load failed: %v", err)
			return err
		}
		log("config loaded in %s", time.Since(t0))

		t1 := time.Now()
		s, err := store.New(cfg)
		if err != nil {
			log("store init failed: %v", err)
			return err
		}
		defer s.Close()
		log("store ready in %s (db + embedder + warmup)", time.Since(t1))
		log("total startup: %s, serving MCP over stdio", time.Since(t0))

		return mcpserver.Serve(s, cfg, mcpOut)
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)
}
