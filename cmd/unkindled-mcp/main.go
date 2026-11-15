// Command unkindled-mcp runs the Unkindled MCP server over stdio.
package main

import (
	"context"
	"log"
	"os"

	"github.com/arthuRHD/unkindled-mcp/internal/api"
	"github.com/arthuRHD/unkindled-mcp/internal/config"
	"github.com/arthuRHD/unkindled-mcp/internal/mcp"
)

func main() {
	log.SetOutput(os.Stderr)
	log.SetFlags(0)

	cfg, warnings := config.Load()
	for _, warning := range warnings {
		log.Printf("warning: %s", warning)
	}
	if cfg.Token == "" {
		log.Print("warning: UNKINDLED_API_TOKEN is not set; the server will initialize, but tool calls will fail until it is configured")
	}

	client := api.New(cfg.APIURL, cfg.Token, cfg.Timeout)
	server := mcp.NewServer(client, cfg.Token != "")

	if err := server.Serve(context.Background(), os.Stdin, os.Stdout); err != nil {
		log.Printf("fatal: %v", err)
		os.Exit(1)
	}
}
