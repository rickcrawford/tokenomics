# TLS / Certificate Management

Tokenomics runs an HTTPS server by default. It can auto-generate certificates on first startup or use certificates you provide.

## Auto-Generated Certificates

When `server.tls.enabled` and `server.tls.auto_gen` are both `true` (the defaults), Tokenomics generates a full certificate chain on first run:

1. A **root CA** (`ca.crt` / `ca.key`) -- valid for 10 years
2. A **server certificate** (`server.crt` / `server.key`) -- valid for 1 year, signed by the root CA

Certificates are stored in the directory specified by `server.tls.cert_dir` (default: `./certs`). On subsequent starts, existing certificates are reused without regeneration.

The server certificate covers:

- `localhost`
- `127.0.0.1`
- `::1`

### Certificate Files

| File | Description |
|------|-------------|
| `certs/ca.crt` | Root CA certificate (share this for trust installation) |
| `certs/ca.key` | Root CA private key (keep this secret) |
| `certs/server.crt` | Server certificate |
| `certs/server.key` | Server private key |

## Trusting the CA

Since the auto-generated CA is not trusted by default, clients will reject the proxy's certificate. You have three options:

### Option 1: Install the CA Certificate

#### macOS

```bash
sudo security add-trusted-cert -d -r trustRoot \
  -k /Library/Keychains/System.keychain ./certs/ca.crt
```

#### Linux (Debian/Ubuntu)

```bash
sudo cp ./certs/ca.crt /usr/local/share/ca-certificates/tokenomics-ca.crt
sudo update-ca-certificates
```

#### Linux (RHEL/Fedora)

```bash
sudo cp ./certs/ca.crt /etc/pki/ca-trust/source/anchors/tokenomics-ca.crt
sudo update-ca-trust
```

### Option 2: Use --insecure

The `init` command's `--insecure` flag adds `NODE_TLS_REJECT_UNAUTHORIZED=0` to the environment, which disables certificate verification for Node.js-based agents:

```bash
eval $(./bin/tokenomics init --token tkn_abc123 --insecure)
```

This is convenient for development but should not be used in production.

### Option 3: Provide Your Own Certificates

See the next section.

## Custom Certificates

To use your own certificate and key, set `cert_file` and `key_file` in the config:

```yaml
server:
  tls:
    enabled: true
    cert_file: "/path/to/server.crt"
    key_file: "/path/to/server.key"
```

When `cert_file` and `key_file` are both set, auto-generation is skipped entirely. The certificate must be valid for the hostname your agents connect to.

You can also set these via environment variables:

```bash
export TOKENOMICS_SERVER_TLS_CERT_FILE="/path/to/server.crt"
export TOKENOMICS_SERVER_TLS_KEY_FILE="/path/to/server.key"
```

## Disabling TLS

To run the proxy over plain HTTP only:

```yaml
server:
  tls:
    enabled: false
  http_port: 8080
```

Or via environment:

```bash
export TOKENOMICS_SERVER_TLS_ENABLED=false
```

When TLS is disabled, the proxy only listens on the HTTP port. Connect agents using `--tls=false`:

```bash
eval $(./bin/tokenomics init --token tkn_abc123 --port 8080 --tls=false)
```
