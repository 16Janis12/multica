package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/daemon"
)

var mcpServerCmd = &cobra.Command{
	Use:   "mcp-server",
	Short: "Start a local MCP server exposing CLI setup tools (GitHub, GitLab, Gitea)",
	Long: `Start a local Model Context Protocol (MCP) server that provides the 'setup_cli' tool.
The tool automatically authenticates CLI tools (GitHub CLI 'gh', GitLab CLI 'glab', Gitea CLI 'tea')
using credentials stored in your Multica workspace.

Supports stdio transport (default) for integration with Claude Desktop, Cursor, and other MCP clients,
as well as HTTP transport via --http.`,
	RunE: runMCPServer,
}

var (
	mcpHttpAddr string
)

func init() {
	mcpServerCmd.Flags().StringVar(&mcpHttpAddr, "http", "", "HTTP listen address (e.g. 127.0.0.1:8080) instead of stdio")
}

func runMCPServer(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	tokenFetcher := func(ctx context.Context, cliName string) (*daemon.CLITokenResponse, error) {
		resp, err := client.FetchCLIToken(ctx, cliName)
		if err != nil {
			return nil, err
		}
		return &daemon.CLITokenResponse{
			CLI:          resp.CLI,
			Token:        resp.Token,
			InstanceURL:  resp.InstanceURL,
			AccountLogin: resp.AccountLogin,
		}, nil
	}

	mcpServer := daemon.NewCLISetupMCPServer(tokenFetcher, nil, "", nil)

	ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if mcpHttpAddr != "" {
		ln, err := net.Listen("tcp", mcpHttpAddr)
		if err != nil {
			return fmt.Errorf("listen on %s: %w", mcpHttpAddr, err)
		}
		server := &http.Server{
			Handler:      mcpServer,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 60 * time.Second,
		}
		go func() {
			<-ctx.Done()
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer shutdownCancel()
			_ = server.Shutdown(shutdownCtx)
		}()
		fmt.Fprintf(os.Stderr, "MCP server listening on http://%s\n", ln.Addr().String())
		if err := server.Serve(ln); err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
	}

	return mcpServer.ServeStdio(ctx, os.Stdin, os.Stdout)
}
