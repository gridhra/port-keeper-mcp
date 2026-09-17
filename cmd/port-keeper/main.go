// Command port-keeper is a local ledger for development ports with an MCP server.
package main

import (
	"os"

	"github.com/gridhra/port-keeper-mcp/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:], os.Stdout, os.Stderr))
}
