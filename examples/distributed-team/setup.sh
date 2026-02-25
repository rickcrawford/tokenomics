#!/usr/bin/env bash
#
# Distributed Team Setup
#
# This script initializes the token database, creates role-based tokens,
# and prints connection instructions for team members.
#
# Prerequisites:
#   - Tokenomics binary built (make build)
#   - Environment variables set (source .env)
#
# Usage:
#   chmod +x examples/distributed-team/setup.sh
#   ./examples/distributed-team/setup.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN="${BIN:-./bin/tokenomics}"
PROXY_HOST="${PROXY_HOST:-localhost}"
PROXY_PORT="${PROXY_PORT:-8443}"

# ── Colors ──────────────────────────────────────────────────────────
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
BOLD='\033[1m'
NC='\033[0m'

info()  { echo -e "${BLUE}[info]${NC}  $*"; }
ok()    { echo -e "${GREEN}[ok]${NC}    $*"; }
warn()  { echo -e "${YELLOW}[warn]${NC}  $*"; }
err()   { echo -e "${RED}[error]${NC} $*" >&2; }

# ── Preflight checks ───────────────────────────────────────────────
if [ ! -f "$BIN" ]; then
    err "Binary not found at $BIN. Run 'make build' first."
    exit 1
fi

if [ -z "${TOKENOMICS_HASH_KEY:-}" ]; then
    err "TOKENOMICS_HASH_KEY is not set. Source your .env file first."
    exit 1
fi

echo ""
echo -e "${BOLD}Tokenomics Distributed Team Setup${NC}"
echo "======================================"
echo ""

# ── Copy configs ───────────────────────────────────────────────────
info "Copying central config..."
cp "$SCRIPT_DIR/central-config.yaml" config.yaml
cp "$SCRIPT_DIR/providers.yaml" providers.yaml
ok "Config files in place."

# ── Create tokens ──────────────────────────────────────────────────
echo ""
echo -e "${BOLD}Creating role-based tokens...${NC}"
echo ""

create_token() {
    local role="$1"
    local expires="$2"
    local policy_file="$SCRIPT_DIR/policies/${role}.json"

    if [ ! -f "$policy_file" ]; then
        err "Policy file not found: $policy_file"
        return 1
    fi

    echo -e "${YELLOW}--- $role ---${NC}"
    $BIN token create --policy "$(cat "$policy_file")" --expires "$expires"
    echo ""
}

create_token "lead-engineer" "1y"
create_token "developer" "90d"
create_token "contractor" "30d"
create_token "ci-pipeline" "1y"

# ── Print instructions ─────────────────────────────────────────────
echo ""
echo -e "${BOLD}Setup complete.${NC}"
echo ""
echo "Next steps:"
echo ""
echo "  1. Save each token above. They cannot be recovered."
echo ""
echo "  2. Start the central config server:"
echo "     $BIN remote --addr :9090 --api-key \"\$TOKENOMICS_REMOTE_KEY\""
echo ""
echo "  3. On each proxy machine, copy proxy-config.yaml and start:"
echo "     cp $SCRIPT_DIR/proxy-config.yaml config.yaml"
echo "     cp $SCRIPT_DIR/providers.yaml providers.yaml"
echo "     $BIN serve"
echo ""
echo "  4. Give team members their token and connection command:"
echo "     eval \$($BIN init --token tkn_<TOKEN> --host $PROXY_HOST --port $PROXY_PORT --insecure)"
echo ""
echo "  5. List and manage tokens:"
echo "     $BIN token list"
echo "     $BIN token update --hash <HASH> --expires 180d"
echo "     $BIN token delete --hash <HASH>"
echo ""
