package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the tokenomics proxy in the background",
	Long: `Starts the tokenomics proxy as a background daemon and prints the proxy URL.
The URL can be captured for use with other commands or environment variables.
Use 'tokenomics stop' to shut it down.`,
	Example: `  tokenomics start
  tokenomics start --port 9443
  tokenomics start --tls=false --port 8080
  export TOKENOMICS_PROXY_URL=$(tokenomics start)`,
	RunE: runStart,
}

var (
	startHost    string
	startPort    int
	startTLS     bool
	startInsecure bool
	startPidFile string
	startLogFile string
)

func init() {
	startCmd.Flags().StringVar(&startHost, "host", "localhost", "proxy hostname")
	startCmd.Flags().IntVar(&startPort, "port", 8443, "proxy port")
	startCmd.Flags().BoolVar(&startTLS, "tls", true, "use HTTPS")
	startCmd.Flags().BoolVar(&startInsecure, "insecure", false, "skip TLS verification for startup health checks")
	startCmd.Flags().StringVar(&startPidFile, "pid-file", "", "PID file path (default: ~/.tokenomics/tokenomics.pid)")
	startCmd.Flags().StringVar(&startLogFile, "log-file", "", "log file path (default: ~/.tokenomics/tokenomics.log)")

	rootCmd.AddCommand(startCmd)
}

func runStart(cmd *cobra.Command, args []string) error {
	scheme := "https"
	if !startTLS {
		scheme = "http"
	}
	proxyURL := fmt.Sprintf("%s://%s:%d", scheme, startHost, startPort)

	dcfg := buildStartDaemonConfig()

	alreadyRunning, err := startDaemon(proxyURL, dcfg)
	if err != nil {
		return err
	}

	if alreadyRunning {
		fmt.Fprintf(os.Stderr, "Proxy already running: %s. Use 'tokenomics stop' to stop it.\n", proxyURL)
	} else {
		fmt.Fprintf(os.Stderr, "Proxy started: %s\n", proxyURL)
	}
	fmt.Println(proxyURL)
	return nil
}

func buildStartDaemonConfig() daemonConfig {
	return daemonConfig{
		host:    startHost,
		port:    startPort,
		tls:     startTLS,
		insecure: startInsecure,
		pidFile: startPidFile,
		logFile: startLogFile,
	}
}
