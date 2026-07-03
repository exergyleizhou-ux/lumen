package main


import (
	"fmt"
	"os"

	scimcp "lumen/internal/science/mcp"
	"lumen/internal/science/mcp/geo"
)

func main() {
	srv := scimcp.NewServer("lumen-mcp-geo", "1.0.0", geo.Tools(nil))
	if err := srv.RunStdio(); err != nil {
		fmt.Fprintf(os.Stderr, "lumen-mcp-geo: %v\n", err)
		os.Exit(1)
	}
}