#!/bin/bash
set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
REPO="rickcrawford/tokenomics"
INSTALL_DIR="${1:-.}"
BINARY_NAME="tokenomics"

# Detect OS and architecture
detect_system() {
    local os arch

    case "$(uname -s)" in
        Linux*)
            os="linux"
            ;;
        Darwin*)
            os="darwin"
            ;;
        MINGW*|MSYS*|CYGWIN*)
            os="windows"
            ;;
        *)
            echo -e "${RED}Error: Unsupported operating system${NC}"
            exit 1
            ;;
    esac

    case "$(uname -m)" in
        x86_64|amd64)
            arch="amd64"
            ;;
        aarch64|arm64)
            arch="arm64"
            ;;
        *)
            echo -e "${RED}Error: Unsupported architecture: $(uname -m)${NC}"
            exit 1
            ;;
    esac

    echo "$os" "$arch"
}

main() {
    echo -e "${GREEN}Tokenomics Installer${NC}"
    echo ""

    # Detect system
    read -r os arch < <(detect_system)
    echo "Detected system: $os/$arch"

    # Get latest release
    echo "Fetching latest release..."
    RELEASE=$(curl -s "https://api.github.com/repos/$REPO/releases/latest" | grep -o '"tag_name": "[^"]*' | cut -d'"' -f4)

    if [ -z "$RELEASE" ]; then
        echo -e "${RED}Error: Could not fetch latest release${NC}"
        exit 1
    fi

    echo "Latest release: $RELEASE"

    # Determine filename
    if [ "$os" = "windows" ]; then
        FILENAME="tokenomics-windows-$arch.zip"
        EXTRACT_CMD="unzip -q"
        BINARY_FILE="tokenomics.exe"
    else
        FILENAME="tokenomics-$os-$arch.tar.gz"
        EXTRACT_CMD="tar -xzf"
        BINARY_FILE="tokenomics"
    fi

    # Download
    DOWNLOAD_URL="https://github.com/$REPO/releases/download/$RELEASE/$FILENAME"
    echo "Downloading from $DOWNLOAD_URL..."

    if ! curl -fsSL -o "$FILENAME" "$DOWNLOAD_URL"; then
        echo -e "${RED}Error: Failed to download $FILENAME${NC}"
        exit 1
    fi

    # Extract
    echo "Extracting..."
    if ! $EXTRACT_CMD "$FILENAME"; then
        echo -e "${RED}Error: Failed to extract archive${NC}"
        rm -f "$FILENAME"
        exit 1
    fi

    # Verify binary exists
    if [ ! -f "$BINARY_FILE" ]; then
        echo -e "${RED}Error: Binary not found after extraction${NC}"
        rm -f "$FILENAME"
        exit 1
    fi

    # Make executable
    chmod +x "$BINARY_FILE"

    # Move to install directory
    if [ "$INSTALL_DIR" != "." ]; then
        mkdir -p "$INSTALL_DIR"
        mv "$BINARY_FILE" "$INSTALL_DIR/$BINARY_NAME"
        INSTALL_PATH="$INSTALL_DIR/$BINARY_NAME"
    else
        INSTALL_PATH="./$BINARY_FILE"
    fi

    # Cleanup
    rm -f "$FILENAME"

    echo ""
    echo -e "${GREEN}Installation successful!${NC}"
    echo ""
    echo "Binary location: $INSTALL_PATH"
    echo ""
    echo "Next steps:"

    if [ "$INSTALL_DIR" = "." ]; then
        echo "  1. Make it accessible: sudo cp ./$BINARY_NAME /usr/local/bin/"
        echo "  2. Verify: ./$BINARY_NAME --help"
    elif echo "$INSTALL_DIR" | grep -q "/usr/local/bin\|/usr/bin\|/bin\|/opt"; then
        echo "  1. Verify installation: $BINARY_NAME --help"
        echo "  2. Start using: $BINARY_NAME serve"
    else
        echo "  1. Add to PATH: export PATH=\"$INSTALL_DIR:\$PATH\""
        echo "  2. Verify: $BINARY_NAME --help"
    fi
}

main "$@"
