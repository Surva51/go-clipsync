# ClipSync

A cross-machine clipboard synchronization tool that allows you to share clipboard content between different computers in real-time.

## Features

- Real-time clipboard synchronization
- Support for text and image (PNG) formats
- Dual transport options: HTTP polling and WebSocket
- Secure shared-key authentication
- Windows support with native Win32 clipboard API

## Project Structure

```
clipsync/
├── cmd/clipsync/         # Main application entry point
├── internal/
│   ├── clip/             # Windows clipboard handling
│   └── net/              # Network communication (HTTP/WebSocket)
├── go.mod                # Go module definition
└── go.sum                # Dependency checksums
```

## Building

```bash
go build -o clipsync.exe cmd/clipsync/main.go
```

## Usage

```bash
# Start with HTTP polling (default)
./clipsync -http "http://your-server:5002/clip" -key "your-secret-key"

# Start with WebSocket transport
./clipsync -http "ws://your-server:5003/ws" -key "your-secret-key" -transport ws

# Adjust polling interval (milliseconds)
./clipsync -interval 500
```

## Configuration Flags

- `-http`: Server endpoint URL (default: `http://localhost:5002/clip`)
- `-key`: Shared secret key for authentication (default: `your-secret-key-here`)
- `-transport`: Transport type: "poll" or "ws" (default: `poll`)
- `-interval`: Polling interval in milliseconds (default: `200`)
- `-timeout`: HTTP POST timeout (default: `15s`)

## Security Notes

1. **Always change the default secret key** before deployment
2. Use HTTPS/WSS in production environments
3. The shared key is used for authentication token generation

## Requirements

- Go 1.16 or higher
- Windows OS (for clipboard functionality)
- A compatible server endpoint

## License

This project is provided as-is for educational and reference purposes.