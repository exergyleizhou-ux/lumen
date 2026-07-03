// lumen-mcp-oasis — native 绿洲 marketplace MCP server for Lumen Science (stdio).
package main


import (
	"fmt"
	"os"

	scimcp "lumen/internal/science/mcp"
	"lumen/internal/science/mcp/oasis"
)

func main() {
	cfg := oasis.Config{
		BaseURL: envOr("OASIS_BASE_URL", "https://demo.oasisdata2026.xyz"),
		Token:   os.Getenv("OASIS_API_TOKEN"),
	}
	srv := scimcp.NewServer("lumen-mcp-oasis", "1.0.0", oasis.Tools(cfg))
	if err := srv.RunStdio(); err != nil {
		fmt.Fprintf(os.Stderr, "lumen-mcp-oasis: %v\n", err)
		os.Exit(1)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}