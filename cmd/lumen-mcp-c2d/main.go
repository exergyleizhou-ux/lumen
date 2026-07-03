package main


import (
	"fmt"
	"os"

	scimcp "lumen/internal/science/mcp"
	"lumen/internal/science/mcp/c2d"
)

func main() {
	cfg := c2d.Config{
		BaseURL: envOr("OASIS_BASE_URL", "https://demo.oasisdata2026.xyz"),
		Token:   os.Getenv("OASIS_API_TOKEN"),
	}
	srv := scimcp.NewServer("lumen-mcp-c2d", "1.0.0", c2d.Tools(cfg))
	if err := srv.RunStdio(); err != nil {
		fmt.Fprintf(os.Stderr, "lumen-mcp-c2d: %v\n", err)
		os.Exit(1)
	}
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}