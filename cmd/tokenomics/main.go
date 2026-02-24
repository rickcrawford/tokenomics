package main

import (
	"os"

	"github.com/rickcrawford/tokenomics/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
