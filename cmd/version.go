package cmd

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

// Set via -ldflags at build time.
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

var versionCmd = &cobra.Command{
	Use:     "version",
	Short:   "Print the version and build information",
	Example: `  tokenomics version`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("tokenomics %s\n", Version)
		fmt.Printf("  commit:  %s\n", Commit)
		fmt.Printf("  built:   %s\n", BuildDate)
		fmt.Printf("  go:      %s\n", runtime.Version())
		fmt.Printf("  os/arch: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
