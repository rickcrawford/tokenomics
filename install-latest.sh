#!/bin/bash
# One-liner installer for Tokenomics
# Usage: curl -fsSL https://raw.githubusercontent.com/rickcrawford/tokenomics/main/install-latest.sh | bash

curl -fsSL https://github.com/rickcrawford/tokenomics/releases/download/$(curl -s https://api.github.com/repos/rickcrawford/tokenomics/releases/latest | grep -o '"tag_name": "[^"]*' | cut -d'"' -f4)/tokenomics-$(uname -s | tr A-Z a-z)-$(uname -m | sed 's/x86_64/amd64/' | sed 's/aarch64/arm64/').tar.gz | tar -xz -C /usr/local/bin/ || (echo "Failed to install. Try: curl -fsSL https://raw.githubusercontent.com/rickcrawford/tokenomics/main/install.sh | bash" && exit 1)

echo "Tokenomics installed successfully!"
echo "Run 'tokenomics --help' to get started"
