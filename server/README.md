# DEFCON-Game HTTP server

Small Go HTTP service used alongside the game client. It exposes a plaintext root endpoint (demo / CTF-style) and a JSON heartbeat under `/api/`.

## Requirements

- [Go](https://go.dev/dl/) 1.22 or newer

## Run

From this directory:

```bash
go run .
```

The server listens on **port 8080** by default.

## Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET`, `HEAD` | `/api/` or `/api` | JSON heartbeat: server is online. |
| `GET` | `/` | Returns a welcome message and flag in plain text. |
| * | any other path | `404 Not Found` |

### Examples

Root (plain text):

```bash
curl -s http://localhost:8080/
```

Response body:

```text
Welcome to the hidden world where the flag is grey{h1dd3n_3nDP01N7}
```

API heartbeat (JSON):

```bash
curl -s http://localhost:8080/api/
```

Response body:

```json
{"status":"online","message":"heartbeat"}
```

`GET /api` (no trailing slash) returns the same JSON. Other methods to `/api` or `/api/` receive `405` with a small JSON error body.

## Build

Produce a binary in the current directory:

```bash
go build -o defcon-server .
./defcon-server
```

On Windows, the binary is named `defcon-server.exe` if you use that output name.

## Project layout

| File | Purpose |
|------|---------|
| `go.mod` | Go module definition |
| `main.go` | HTTP server and route handlers |

## Changing the port

The listen address is set in `main.go` as the `addr` variable (currently `":8080"`). Change it and rebuild or rerun.
