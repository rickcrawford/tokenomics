package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var completionCmd = &cobra.Command{
	Use:   "completion [bash|zsh|fish|powershell]",
	Short: "Generate shell completion scripts",
	Long: `Generate shell completion scripts for tokenomics.

To load completions:

Bash:
  $ source <(tokenomics completion bash)
  # To load completions for each session, execute once:
  # Linux:
  $ tokenomics completion bash > /etc/bash_completion.d/tokenomics
  # macOS:
  $ tokenomics completion bash > $(brew --prefix)/etc/bash_completion.d/tokenomics

Zsh:
  $ source <(tokenomics completion zsh)
  # To load completions for each session, execute once:
  $ tokenomics completion zsh > "${fpath[1]}/_tokenomics"

Fish:
  $ tokenomics completion fish | source
  # To load completions for each session, execute once:
  $ tokenomics completion fish > ~/.config/fish/completions/tokenomics.fish

PowerShell:
  PS> tokenomics completion powershell | Out-String | Invoke-Expression
  # To load completions for each session, execute once and source this file from your profile:
  PS> tokenomics completion powershell > tokenomics.ps1`,
	Example: `  tokenomics completion bash
  tokenomics completion zsh
  source <(tokenomics completion bash)`,
	DisableFlagsInUseLine: true,
	ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
	Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
	RunE: func(cmd *cobra.Command, args []string) error {
		switch args[0] {
		case "bash":
			return rootCmd.GenBashCompletion(os.Stdout)
		case "zsh":
			return rootCmd.GenZshCompletion(os.Stdout)
		case "fish":
			return rootCmd.GenFishCompletion(os.Stdout, true)
		case "powershell":
			return rootCmd.GenPowerShellCompletionWithDesc(os.Stdout)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(completionCmd)
}
