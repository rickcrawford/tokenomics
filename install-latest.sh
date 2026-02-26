#!/bin/bash
set -euo pipefail
# One-liner installer bootstrap for Tokenomics
# Usage: curl -fsSL https://raw.githubusercontent.com/rickcrawford/tokenomics/main/install-latest.sh | bash

REPO="rickcrawford/tokenomics"

curl -fsSL "https://github.com/${REPO}/releases/latest/download/install.sh" | bash
