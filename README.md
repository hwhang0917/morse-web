# morse-web

1-on-1 Morse code over WebSocket. Open the page, get paired with the next stranger who shows up, and key at each other in real time.

## Run

```sh
go run .                    # http://localhost:8080
go run . -addr :9000        # other port
./morse-web --version
```

Or with Docker:

```sh
docker compose up -d        # pulls ghcr.io/hwhang0917/morse-web:latest
```

## How it works

- Single Go binary using [coder/websocket](https://github.com/coder/websocket) with a hub/pump architecture: one hub goroutine keeps a FIFO queue and pairs sockets as they arrive. If your partner leaves you go back into the queue.
- Every WebSocket frame is binary and 1 byte for key down/up; the partner hears the tone for exactly as long as you hold the key.
- The web UI is an embedded HTML page plus three small ES modules (`web/static/js/{audio,settings,socket}.js`): a big key button, Web Audio tones, optional vibration and screen flash, settings in the ☰ menu.

## Endpoints

| Path | Purpose |
|---|---|
| `GET /` | web UI |
| `GET /ws?nick=…` | WebSocket, auto-matched on connect; add `&bot=1` if you are a bot |
| `POST /api/register` | issue a nickname |
| `GET /api/status` | waiting / paired counts |
| `GET /version` | version string |
| `/mcp` | MCP server for LLM clients: connect, send, receive, disconnect |

Bots and agents: add `/mcp` as a remote MCP server and use the `connect`, `send` and `receive` tools; the server keys and decodes for you. The raw wire protocol, Morse timing and a Python example are in [`/llms.txt`](web/static/llms.txt).

## Release

Bump `VERSION`, then push a matching tag. CI builds `linux/amd64`, `linux/arm64` and `linux/arm/v7` images to GHCR and creates the GitHub release.

```sh
git tag v0.0.2 && git push origin v0.0.2
```
