package main

import (
	"os"

	"github.com/rickcrawford/tokenomics/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
