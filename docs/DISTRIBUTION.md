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
3. Attach `install.sh` and `install-latest.sh` to the release
4. Create a GitHub Release with all assets

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
curl -fsSL https://github.com/rickcrawford/tokenomics/releases/latest/download/install.sh | bash
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

# Optional: copy an example config as a starting point
cp examples/config.yaml ~/.tokenomics/config.yaml

# Set API keys
export OPENAI_PAT="sk-..."
export ANTHROPIC_PAT="sk-ant-..."
export TOKENOMICS_HASH_KEY="your-secret-hash-key"
```

## Using Pre-built Binaries in CI/CD

### GitHub Actions

```yaml
- name: Install Tokenomics
  run: |
    curl -fsSL https://github.com/rickcrawford/tokenomics/releases/latest/download/install.sh | bash
    export PATH="$PWD:$PATH"
    tokenomics --help
```

### Docker

```dockerfile
FROM ubuntu:22.04

RUN apt-get update && apt-get install -y curl

RUN curl -fsSL https://github.com/rickcrawford/tokenomics/releases/latest/download/install.sh | bash

ENTRYPOINT ["tokenomics"]
```

### Ansible

```yaml
- name: Install Tokenomics
  shell: curl -fsSL https://github.com/rickcrawford/tokenomics/releases/latest/download/install.sh | bash
```

## Build Workflow Details

The `.github/workflows/build-release.yml` workflow:

1. **Triggered by:**
   - Push to a tag matching `v*` (e.g., `v1.0.0`)
   - Manual trigger via `workflow_dispatch`

2. **Build strategy:**
   - Uses cross-compilation from Linux for all supported platforms
   - Produces platform-specific archives in one job

3. **Artifacts:**
   - Linux/macOS: `.tar.gz` archives with `tokenomics` binary
   - Windows: `.zip` archive with `tokenomics.exe`
   - `checksums.txt` with SHA256 hashes
   - `install.sh` and `install-latest.sh`

4. **Release creation:**
   - Creates GitHub Release on version tags
   - Attaches binaries, checksums, and install scripts
   - Generates release notes from commits

## Checksums

Verify downloaded binaries using checksums:

```bash
# Download archive and checksums
curl -fsSL -O https://github.com/rickcrawford/tokenomics/releases/download/v1.2.3/tokenomics-linux-amd64.tar.gz
curl -fsSL -O https://github.com/rickcrawford/tokenomics/releases/download/v1.2.3/checksums.txt

# Verify
grep "tokenomics-linux-amd64.tar.gz" checksums.txt | shasum -a 256 -c
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
curl -fsSL https://github.com/rickcrawford/tokenomics/releases/latest/download/install.sh | bash
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
RUN curl -fsSL https://github.com/rickcrawford/tokenomics/releases/latest/download/install.sh | bash
```

### Approach 3: Package Managers

Homebrew, APT, and Chocolatey packages are not currently published. Use GitHub Releases or internal packaging workflows.

## Release Notes

Each release includes:
- Commit history since last release
- Breaking changes (if any)
- New features
- Bug fixes
- Platform-specific notes

Check the [Releases page](https://github.com/rickcrawford/tokenomics/releases) for details on each version.
