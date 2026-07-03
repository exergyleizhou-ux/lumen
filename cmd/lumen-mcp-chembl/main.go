package main


import (
	"fmt"
	"os"

	scimcp "lumen/internal/science/mcp"
	"lumen/internal/science/mcp/chembl"
)

func main() {
	srv := scimcp.NewServer("lumen-mcp-chembl", "1.0.0", chembl.Tools(nil))
	if err := srv.RunStdio(); err != nil {
		fmt.Fprintf(os.Stderr, "lumen-mcp-chembl: %v\n", err)
		os.Exit(1)
	}
}