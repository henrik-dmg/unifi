// Command unifi is the CLI entrypoint for the UniFi Network controller client.
package main

import (
	"os"

	"github.com/colindickson/unifi/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args, os.Stdout, os.Stderr))
}
