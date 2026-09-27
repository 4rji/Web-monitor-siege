# k6 Web Runner

A small Go web app for running [k6](https://k6.io) load tests from the browser.

```
Browser → Go server → k6 run (local binary) → output streamed back to the browser
```

The server executes the local `k6` binary with `os/exec`. Docker is optional and
only packages the server and k6 together; it is never started by the app.

## Run locally

Requires only Go 1.24+.

```bash
go run ./cmd/server
```

You don't need to install k6. If `k6` is not in your `PATH`, the server
downloads the official k6 v2.3.0 release on first start, verifies its SHA-256
checksum, and caches it in your user cache directory (`~/.cache/k6-web/` on
Linux, `~/Library/Caches/k6-web/` on macOS). Later starts reuse the cached copy.
A k6 that is already installed is used instead.

Open http://localhost:8080, fill in the form and press **Run Test**.

## Run with Docker

```bash
docker build -t k6-web .
docker run --rm -p 8080:8080 k6-web
```

To only accept connections from your own machine:

```bash
docker run --rm -p 127.0.0.1:8080:8080 k6-web
```

## Configuration

Environment variables, all optional:

| Variable       | Default | Description                         |
|----------------|---------|-------------------------------------|
| `ADDR`         | `:8080` | Listen address                      |
| `K6_BIN`       | —       | Use this k6 binary (skips download) |
| `MAX_VUS`      | `200`   | Maximum virtual users per test      |
| `MAX_DURATION` | `10m`   | Maximum test duration               |

Only one test runs at a time; a second request gets `409 Conflict`.

## API

`POST /api/run`

```json
{
  "url": "https://example.com",
  "vus": 10,
  "duration": "30s",
  "p95_ms": 500,
  "max_failure_rate": 0.02
}
```

- `url`: `http` or `https`. If no scheme is given, `http://` is added.
- `duration`: `30s`, `2m`, `1m30s`, or a number of seconds.
- `max_failure_rate`: fraction greater than 0 and at most 1 (`0.02` = 2%).

Invalid input returns `400` with `{"error": "..."}`.

A valid request returns a stream of newline-delimited JSON (`application/x-ndjson`):
`output` events with k6's output as it runs, then one final `result` event.

```json
{"type":"output","text":"running (05.0s), 10/10 VUs, 812 complete ...\n"}
{"type":"result","result":{"success":false,"thresholds_failed":true,"exit_code":99,"error":""}}
```

| Outcome                           | `success` | `thresholds_failed` | `error`   |
|-----------------------------------|-----------|---------------------|-----------|
| Ran, all thresholds passed        | `true`    | `false`             | `""`      |
| Ran, p95 or failure rate exceeded | `false`   | `true`              | `""`      |
| k6 failed, timed out or cancelled | `false`   | `false`             | message   |

Closing the connection (the **Stop** button) stops k6.

```bash
curl -N -X POST localhost:8080/api/run \
  -d '{"url":"https://example.com","vus":5,"duration":"10s","p95_ms":500,"max_failure_rate":0.02}'
```

## How it works

- `internal/k6runner/script.js` is a fixed k6 script, embedded in the binary.
  Your settings are passed with `k6 run -e KEY=value` and read through `__ENV`,
  so nothing you type is inserted into JavaScript code.
- For each test the script is written to its own temp file (`os.CreateTemp`),
  and the file is deleted when the test ends.
- k6 runs through `exec.CommandContext` without a shell, with a timeout of
  duration + 60s. On timeout or cancel it gets `SIGINT` so it can print its
  summary.

## Layout

```
cmd/server/main.go          HTTP server, streaming, config
internal/k6runner/          validation, k6 script, k6 execution
internal/k6bin/             finds k6 or downloads it on first run
web/index.html              UI (embedded via web/embed.go)
Dockerfile                  packages the server + k6 binary
```

## Tests

```bash
go test ./...
```
