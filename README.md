# Nano API

A lightweight, multi-provider API proxy server that forwards OpenAI-compatible requests to various LLM providers with built-in load balancing, rate limiting, and automatic failover.

## Features

- **Multi-Provider Support**: Connect to multiple LLM providers (NVIDIA, OpenCode, etc.) simultaneously
- **Automatic Load Balancing**: Round-robin distribution across multiple API keys within each provider
- **Rate Limiting**: Per-provider rate limiting to respect API quotas
- **Model Mapping**: Use friendly alias names (e.g., "high", "medium") that map to actual model names per provider
- **Proxy Support**: Route requests through a proxy when direct access is unavailable
- **Request Retries**: Automatic retry on failure with configurable retry count
- **Streaming Support**: Full support for streaming responses
- **CORS Enabled**: Works with browser-based clients

## Quick Start

### 1. Clone the Repository

```bash
git clone https://github.com/rankalpha2023/nano-api.git
cd nano-api
```

### 2. Configuration

Copy the example config and edit it with your API keys:

```bash
cp config.example.yaml config.yaml
```

Edit `config.yaml` with your providers and API keys.

### 3. Build and Run

```bash
go build -o nano-api main.go
./nano-api
```

The server will start on port 8080 by default.

## Configuration

### config.yaml Structure

```yaml
providers:
  - name: opencode                    # Provider name
    base-url: https://opencode.ai/zen/v1
    api-key-entries:                 # Multiple API keys for load balancing
      - your-api-key-1
      - your-api-key-2
    models:                          # Model aliases for this provider
      high: minimax-m2.5-free
      medium: nemotron-3-super-free
    rate-limit:                      # Rate limiting settings
      maxRequestsPerMinute: 40
      minIntervalMs: 100
      retryIntervalMs: 5000
    model-config:                    # Default model parameters
      defaultTemperature: 0.1
      defaultTopP: 1.0
      defaultMaxTokens: 64000
    timeout: 30                      # Request timeout in seconds
    proxy: ""                        # Optional proxy URL

  - name: nvidia
    base-url: https://integrate.api.nvidia.com/v1
    api-key-entries:
      - your-nvidia-api-key
    models:
      high: z-ai/glm-5.1
    rate-limit:
      maxRequestsPerMinute: 40
    timeout: 120
    proxy: "http://localhost:16808"  # Proxy example

port: 8080                           # Server port
request-retry: 3                     # Number of retries on failure
rate-limit:                          # Global rate limit fallback
  maxRequestsPerMinute: 40
  minIntervalMs: 100
  retryIntervalMs: 5000
```

### Configuration Priority

1. **Provider-specific settings** take precedence
2. **Global settings** are used as fallback
3. **Hard-coded defaults** are used if neither is configured

## API Usage

### OpenAI-Compatible Endpoint

```
POST http://localhost:8080/v1/chat/completions
```

### Request Example

```json
{
  "model": "high",
  "messages": [
    {
      "role": "user",
      "content": "Hello, how are you?"
    }
  ],
  "stream": false
}
```

### Response

The server returns responses in the same format as the upstream provider.

## Architecture

```
┌─────────────┐     ┌──────────────────┐     ┌─────────────────┐
│   Client    │────▶│   Nano API       │────▶│   Provider 1   │
│             │     │   (Load Balance) │     │   (Round Robin)│
└─────────────┘     │                  │     └─────────────────┘
                    │   Rate Limiter   │     ┌─────────────────┐
                    │   Per Provider   │────▶│   Provider 2   │
                    │                  │     └─────────────────┘
                    └──────────────────┘
```

### Four-Layer Architecture

- **Types**: Data schemas and request/response structures
- **Core**: Pure functions (rate limiting, request handling)
- **Engine**: State management and account pooling
- **Server**: HTTP routing and response handling

## Development

### Project Structure

```
.
├── config/          # Configuration loading
├── core/            # Core utilities (rate limiter, request handler)
├── engine/          # Account manager and load balancing
├── server/          # HTTP server and routing
├── types/           # Type definitions
├── config.yaml      # Local configuration (not committed)
├── config.example.yaml
└── main.go
```

### Running Tests

```bash
# Non-streaming test
python test.py

# Streaming test
python test_stream.py
```

## License

MIT
