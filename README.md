# HydraVPN Protocol

> 🚀 Next-generation VPN protocol with multi-transport architecture, traffic obfuscation, and resistance to blocking.

## Features

- **Multi-Transport**: Automatically switch between QUIC, WebSocket, and Obfuscated transports
- **Traffic Obfuscation**: VPN traffic looks like regular HTTPS/TLS
- **Modern Cryptography**: ChaCha20-Poly1305 + X25519 key exchange
- **Cross-Platform**: macOS and Linux support
- **Zero Config**: Single command to start

## Quick Start

### Prerequisites

- Go 1.21+
- Root/sudo access (for TUN interface)

### Install Dependencies

```bash
cd /Users/olegavdeev/Desktop/My_Projects/VPN_tests
go mod tidy
```

### Run Server

```bash
# Start server on port 8443
sudo go run ./cmd/hydra server --listen :8443
```

### Run Client

```bash
# Connect to server
sudo go run ./cmd/hydra client --server 127.0.0.1:8443
```

## Usage

```
╦ ╦╦ ╦╔╦╗╦═╗╔═╗  ╦  ╦╔═╗╔╗╔
╠═╣╚╦╝ ║║╠╦╝╠═╣  ╚╗╔╝╠═╝║║║
╩ ╩ ╩ ═╩╝╩╚═╩ ╩   ╚╝ ╩  ╝╚╝

Usage: hydra <command> [options]

Commands:
  server    Start VPN server
  client    Connect to VPN server
  version   Show version
  help      Show this help

Server options:
  --listen <addr>     Listen address (default: :8443)
  --transport <type>  Transport: websocket, quic, obfs

Client options:
  --server <addr>     Server address (default: 127.0.0.1:8443)
  --transport <type>  Transport: websocket, quic, obfs
```

## Transport Types

| Transport | Port | Best For |
|-----------|------|----------|
| `websocket` | 443/8443 | Bypassing firewalls, works through proxies |
| `quic` | UDP | Lowest latency, best performance |
| `obfs` | 443 | Maximum stealth, looks like HTTPS |

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│  CLIENT                                                      │
│    │                                                         │
│    ├──► [QUIC/UDP] ──► Direct connection (fastest)          │
│    ├──► [WebSocket] ──► CDN fronting (most reliable)        │
│    └──► [Obfuscated] ──► TLS tunnel (most stealthy)         │
│                                                              │
│  Automatic transport selection based on network conditions   │
└─────────────────────────────────────────────────────────────┘
```

## Project Structure

```
VPN_tests/
├── cmd/hydra/main.go      # CLI entry point
├── pkg/
│   ├── crypto/            # ChaCha20-Poly1305, X25519
│   ├── protocol/          # Packet format, handshake
│   ├── transport/         # QUIC, WebSocket, Obfuscated
│   ├── tun/               # TUN device (macOS/Linux)
│   ├── server/            # VPN server
│   └── client/            # VPN client
└── README.md
```

## Security

- **Key Exchange**: X25519 (Curve25519)
- **Encryption**: XChaCha20-Poly1305 (AEAD)
- **Key Derivation**: HKDF-SHA256
- **Perfect Forward Secrecy**: New keys per session

## Building

```bash
# Build binary
go build -o hydra ./cmd/hydra

# Run
sudo ./hydra server --listen :8443
```

## License

MIT
