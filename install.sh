#!/usr/bin/env bash
#
# Firebell Installer
# Usage: curl -fsSL https://raw.githubusercontent.com/meeksoft/Firebell/main/install.sh | bash
#

set -euo pipefail

REPO="https://github.com/meeksoft/Firebell.git"
INSTALL_DIR="${HOME}/.firebell"
BIN_DIR="${INSTALL_DIR}/bin"
VERSION="1.4.4"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

info() { echo -e "${BLUE}==>${NC} $1"; }
success() { echo -e "${GREEN}==>${NC} $1"; }
warn() { echo -e "${YELLOW}==>${NC} $1"; }
error() { echo -e "${RED}==>${NC} $1" >&2; exit 1; }

# Check OS
check_os() {
    case "$(uname -s)" in
        Linux*)  OS="linux" ;;
        Darwin*) OS="darwin" ;;
        *)       error "Unsupported OS: $(uname -s). Firebell supports Linux (full features) and macOS/Windows (log monitoring only)." ;;
    esac

    case "$(uname -m)" in
        x86_64|amd64) ARCH="amd64" ;;
        arm64|aarch64) ARCH="arm64" ;;
        *)            error "Unsupported architecture: $(uname -m)" ;;
    esac

    info "Detected: ${OS}/${ARCH}"
}

# Check for Go
check_go() {
    if ! command -v go &> /dev/null; then
        error "Go is not installed. Please install Go 1.21+ first: https://go.dev/dl/"
    fi

    GO_VERSION=$(go version | grep -oE 'go[0-9]+\.[0-9]+' | head -1)
    info "Found: ${GO_VERSION}"
}

# Install from source
install_from_source() {
    info "Installing Firebell v${VERSION}..."

    # Create temp directory
    TMP_DIR=$(mktemp -d)
    trap "rm -rf ${TMP_DIR}" EXIT

    # Clone repo
    info "Cloning repository..."
    git clone --depth 1 "${REPO}" "${TMP_DIR}/firebell" 2>/dev/null

    # Build
    info "Building..."
    cd "${TMP_DIR}/firebell"
    mkdir -p "${BIN_DIR}"
    go build -ldflags "-X firebell/internal/config.Version=${VERSION}" -o "${BIN_DIR}/firebell" ./cmd/firebell

    success "Built: ${BIN_DIR}/firebell"
}

# Check PATH
check_path() {
    if [[ ":$PATH:" != *":${BIN_DIR}:"* ]]; then
        echo ""
        warn "Add Firebell to your PATH by adding this to your shell config:"
        echo ""
        echo "  export PATH=\"${BIN_DIR}:\$PATH\""
        echo ""

        # Detect shell config file
        SHELL_NAME=$(basename "$SHELL")
        case "$SHELL_NAME" in
            bash)
                if [[ -f "${HOME}/.bashrc" ]]; then
                    echo "  # For bash, add to ~/.bashrc:"
                    echo "  echo 'export PATH=\"${BIN_DIR}:\$PATH\"' >> ~/.bashrc"
                fi
                ;;
            zsh)
                echo "  # For zsh, add to ~/.zshrc:"
                echo "  echo 'export PATH=\"${BIN_DIR}:\$PATH\"' >> ~/.zshrc"
                ;;
        esac
        echo ""
    fi
}

# Main
main() {
    echo ""
    echo "  ______ _          _          _ _ "
    echo " |  ____(_)        | |        | | |"
    echo " | |__   _ _ __ ___| |__   ___| | |"
    echo " |  __| | | '__/ _ \ '_ \ / _ \ | |"
    echo " | |    | | | |  __/ |_) |  __/ | |"
    echo " |_|    |_|_|  \___|_.__/ \___|_|_|"
    echo ""
    echo " Real-time AI CLI activity monitor"
    echo ""

    check_os
    check_go
    install_from_source
    check_path

    echo ""
    success "Firebell v${VERSION} installed successfully!"
    echo ""
    echo "  Get started:"
    echo "    firebell --setup    # Configure Slack webhook"
    echo "    firebell --check    # Check detected AI agents"
    echo "    firebell start      # Start monitoring daemon"
    echo ""
    echo "  Documentation: https://github.com/meeksoft/Firebell"
    echo ""
}

main "$@"
