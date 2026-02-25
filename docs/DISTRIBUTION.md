# Distribution and Installation

Tokenomics is distributed via GitHub Releases with pre-built binaries for multiple platforms. This document explains how to install and distribute Tokenomics.

## Automated Release Process

Tokenomics uses GitHub Actions to automatically build and release binaries whenever a new version is tagged.

### Creating a Release

To create a new release:

```bash
# Tag the current commit
git tag v1.2.3

# Push the tag (this triggers the build workflow)
git push origin v1.2.3
```

The GitHub Actions workflow will:
1. Build binaries for all supported platforms
2. Create checksums for verification
3. Create a GitHub Release with all binaries

### Supported Platforms

Binary releases are built for:
- Linux x86_64
- Linux ARM64
- macOS x86_64 (Intel)
- macOS ARM64 (Apple Silicon)
- Windows x86_64

## Installation Methods

### Quick Install (Recommended)

```bash
curl -fsSL https://raw.githubusercontent.com/rickcrawford/tokenomics/main/install.sh | bash
```

This script:
- Detects your OS and architecture
- Downloads the latest release
- Extracts the binary
- Guides you on next steps

### Install to Specific Directory

```bash
bash install.sh /usr/local/bin
```

### Manual Download

1. Go to [GitHub Releases](https://github.com/rickcrawford/tokenomics/releases)
2. Download the binary for your platform
3. Extract: `tar -xzf tokenomics-linux-amd64.tar.gz`
4. Make executable: `chmod +x tokenomics`
5. Move to PATH: `sudo mv tokenomics /usr/local/bin/`

## Build Locally

If you prefer to build from source:

```bash
git clone https://github.com/rickcrawford/tokenomics.git
cd tokenomics
make build
sudo cp bin/tokenomics /usr/local/bin/
```

## Verifying Installation

```bash
# Check version
tokenomics --version

# Test functionality
tokenomics --help
```

## Environment Setup

After installation, configure environment variables:

```bash
# Create config directory
mkdir -p ~/.tokenomics

# Copy example config (if available)
cp docs/config.yaml.example ~/.tokenomics/config.yaml

# Set API keys
export OPENAI_API_KEY="sk-..."
export ANTHROPIC_API_KEY="sk-ant-..."
export TOKENOMICS_HASH_KEY="your-secret-hash-key"
```

## Using Pre-built Binaries in CI/CD

### GitHub Actions

```yaml
- name: Install Tokenomics
  run: |
    curl -fsSL https://raw.githubusercontent.com/rickcrawford/tokenomics/main/install.sh | bash
    export PATH="$PWD:$PATH"
    tokenomics --help
```

### Docker

```dockerfile
FROM ubuntu:22.04

RUN apt-get update && apt-get install -y curl

RUN curl -fsSL https://raw.githubusercontent.com/rickcrawford/tokenomics/main/install.sh | bash

ENTRYPOINT ["tokenomics"]
```

### Ansible

```yaml
- name: Install Tokenomics
  shell: curl -fsSL https://raw.githubusercontent.com/rickcrawford/tokenomics/main/install.sh | bash
```

## Build Workflow Details

The `.github/workflows/build-release.yml` workflow:

1. **Triggered by:**
   - Push to a tag matching `v*` (e.g., `v1.0.0`)
   - Manual trigger via `workflow_dispatch`

2. **Build matrix:**
   - Runs builds in parallel for all platform combinations
   - Each build produces a platform-specific archive

3. **Artifacts:**
   - Linux/macOS: `.tar.gz` archives with `tokenomics` binary
   - Windows: `.zip` archive with `tokenomics.exe`
   - SHA256 checksums for each archive

4. **Release creation:**
   - Collects all artifacts
   - Creates GitHub Release
   - Attaches binaries and checksums
   - Generates release notes from commits

## Checksums

Verify downloaded binaries using checksums:

```bash
# Download archive and checksum
curl -fsSL -O https://github.com/rickcrawford/tokenomics/releases/download/v1.2.3/tokenomics-linux-amd64.tar.gz
curl -fsSL -O https://github.com/rickcrawford/tokenomics/releases/download/v1.2.3/tokenomics-linux-amd64.tar.gz.sha256

# Verify
sha256sum -c tokenomics-linux-amd64.tar.gz.sha256
```

## Troubleshooting

### "Command not found: tokenomics"

Ensure the installation directory is in your PATH:

```bash
# Check PATH
echo $PATH

# Add to PATH permanently (macOS/Linux)
echo 'export PATH="/usr/local/bin:$PATH"' >> ~/.bashrc
source ~/.bashrc
```

### "Permission denied" when running tokenomics

Make the binary executable:

```bash
chmod +x /usr/local/bin/tokenomics
```

### Architecture mismatch error during install

The install script failed to detect your architecture. Check:

```bash
uname -s     # Should show: Linux, Darwin, or MINGW
uname -m     # Should show: x86_64 or aarch64
```

If your architecture is not supported, build from source.

### TLS certificate installation fails

Refer to [TLS.md](TLS.md) for platform-specific certificate installation instructions.

## Distributing Tokenomics in Your Organization

### Approach 1: Shared Directory

```bash
# Build and copy to shared location
curl -fsSL https://raw.githubusercontent.com/rickcrawford/tokenomics/main/install.sh | bash
cp tokenomics /shared/bin/
chmod 755 /shared/bin/tokenomics

# Team members add to PATH
echo 'export PATH="/shared/bin:$PATH"' >> ~/.bashrc
```

### Approach 2: Docker Image

Build and push a Docker image with Tokenomics pre-installed:

```dockerfile
FROM ubuntu:22.04
RUN apt-get update && apt-get install -y curl
RUN curl -fsSL https://raw.githubusercontent.com/rickcrawford/tokenomics/main/install.sh | bash
```

### Approach 3: Package Manager

Consider creating packages for popular package managers (apt, brew, etc.) once the project is widely used.

## Release Notes

Each release includes:
- Commit history since last release
- Breaking changes (if any)
- New features
- Bug fixes
- Platform-specific notes

Check the [Releases page](https://github.com/rickcrawford/tokenomics/releases) for details on each version.
