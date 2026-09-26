package main

import (
	"testing"
)

func TestMCPServerCmdRegistration(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"mcp-server"})
	if err != nil {
		t.Fatalf("find mcp-server: %v", err)
	}
	if cmd == nil || cmd.Name() != "mcp-server" {
		t.Fatalf("mcp-server command not registered on rootCmd")
	}

	httpFlag := cmd.Flags().Lookup("http")
	if httpFlag == nil {
		t.Fatalf("missing --http flag on mcp-server command")
	}
}
