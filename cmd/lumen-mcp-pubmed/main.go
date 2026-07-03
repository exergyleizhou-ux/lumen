// lumen-mcp-pubmed — native PubMed MCP server for Lumen Science (stdio).
package main


import (
	"fmt"
	"os"

	scimcp "lumen/internal/science/mcp"
	"lumen/internal/science/mcp/pubmed"
)

func main() {
	srv := scimcp.NewServer("lumen-mcp-pubmed", "1.0.0", pubmed.Tools(nil))
	if err := srv.RunStdio(); err != nil {
		fmt.Fprintf(os.Stderr, "lumen-mcp-pubmed: %v\n", err)
		os.Exit(1)
	}
}