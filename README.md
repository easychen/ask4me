# Ask4Me

> English | [中文](README.zh-CN.md)

Ask4Me is a self-hosted service: you send a “request” via API, the server pushes an interaction link to your notification channel (ServerChan / Apprise). You open the link on your phone or in a browser, click a button or type text to submit, and the original HTTP long-poll request receives the final result (JSON or SSE).

Conceptually, this is Human-in-the-Loop: it might be one of the simplest Human-in-the-Loop setups you can self-host.

## One-request demo (curl)

`mcd` is required. Without `mcd`, the user will receive a notification but won’t have anything to click/type, so you won’t get meaningful data back.

```bash
curl -sS --max-time 120 \
  -X POST 'http://localhost:8080/v1/ask' \
  -H 'Authorization: Bearer change-me' \
  -H 'Content-Type: application/json' \
  -d '{"title":"Ask4Me Demo","body":"Click a button to respond.","mcd":":::buttons\n- [OK](ok)\n- [Later](later)\n:::"}'
```

You will receive a notification with an interaction link. Open it and click one of the buttons, then this curl request returns:

```json
{
  "request_id": "req_xxx",
  "last_event_type": "user.submitted",
  "data": { "action": "ok", "text": "" },
  "last_event_id": "evt_xxx"
}
```

This repository includes:

- Go server (binary name: `ask4me`)
- JavaScript SDK: `ask4me-sdk` (directory: `sdk-js/`)
- JavaScript CLI: `ask4me-cli` (directory: `packages/cli/`)
- Node server launcher: `ask4me-server` (directory: `packages/server/`, used to download/start the binary)

## Multi-platform binaries

Recommended: download the binary for your platform from GitHub Releases (built by GoReleaser), or use `ask4me-server` to download and start it automatically.

## Start the server

### 1) Prepare config (.env)

Copy the example config:

```bash
cp .env.example .env
```

At minimum you need:

- `ASK4ME_BASE_URL`: externally accessible base URL used to build interaction links (sent via notifications)
- `ASK4ME_API_KEY`: API auth key
- One of the notification channels (otherwise requests will quickly end with `notify.failed`):
  - `ASK4ME_SERVERCHAN_SENDKEY`
  - or `ASK4ME_APPRISE_URLS`
- `ASK4ME_TERMINAL_CACHE_SECONDS`: in-memory cache TTL (seconds) for the terminal result. If the client nonStream/SSE connection drops mid-way, you can reconnect with the same `request_id` within this TTL to fetch the terminal result; also used for short-term SSE lookups after subscribers disconnect.

### 2) Start

Recommended: use `ask4me-server` to download/start automatically:

```bash
npm install -g ask4me-server
ask4me-server --config ./.env
```

Or run the downloaded/built `ask4me` directly:

```bash
./ask4me -config ./.env
```

By default it listens on `:8080` (override with `ASK4ME_LISTEN_ADDR`).

## Quickstart: nonStream mode + raw requests (curl)

nonStream is the default: without `stream=true`, `/v1/ask` blocks until the user submits in the web UI or the request expires, then returns a single JSON response.

### 1) POST (recommended)

```bash
curl -sS --max-time 120 \
  -X POST 'http://localhost:8080/v1/ask' \
  -H 'Authorization: Bearer change-me' \
  -H 'Content-Type: application/json' \
  -d '{"title":"Ask4Me Demo","body":"Click a button to respond.","mcd":":::buttons\n- [OK](ok)\n- [Later](later)\n:::"}'
```

Notes:

- `mcd` is required. Without it, the user receives a notification but has no controls to submit anything.
- `--max-time 120` is just an example to avoid waiting forever in your terminal. If you don’t open the notification and submit within 120 seconds, curl exits with a timeout. You can resume waiting with `request_id` (see below).
- In nonStream mode the response does not include `interaction_url`; the interaction link is delivered via your notification channel.

Example response (returned after terminal state):

```json
{
  "request_id": "req_xxx",
  "last_event_type": "user.submitted",
  "data": { "action": "ok", "text": "" },
  "last_event_id": "evt_xxx"
}
```

Possible terminal `last_event_type` values:

- `user.submitted`: user submitted successfully (button or input)
- `request.expired`: expired without submission
- `notify.failed`: notification delivery failed (usually missing config or channel error)

### 2) GET (environments without headers)

If you can’t easily set the `Authorization: Bearer ...` header, use GET with a `key` query param:

```bash
curl -sS --max-time 120 -G 'http://localhost:8080/v1/ask' \
  --data-urlencode 'key=change-me' \
  --data-urlencode 'title=Ask4Me Demo' \
  --data-urlencode 'body=Click a button to respond.' \
  --data-urlencode $'mcd=:::buttons\n- [OK](ok)\n- [Later](later)\n:::'
```

Security note: query params may be logged by proxies/servers. Prefer POST + Authorization when possible.

### 3) Resume waiting after timeout (with request_id)

After a client timeout (like `--max-time 40`), you can resume waiting with `request_id`:

```bash
curl -sS --max-time 40 -G 'http://localhost:8080/v1/ask' \
  --data-urlencode 'key=change-me' \
  --data-urlencode 'request_id=req_xxx'
```

### 4) Pre-generate request_id (recommended for non-interactive environments)

You can generate a `request_id` yourself (e.g. pre-allocate it in a job queue) and let the server create a request with that ID. Benefits:

- Even if the nonStream long connection drops, you can re-request with the same `request_id` and fetch the terminal result
- You can use `request_id` as an idempotency/correlation key in external systems

Rules:

- `request_id` must start with `req_` and only contain lowercase letters, digits, and underscores

Example (GET + pre-generated request_id):

```bash
curl -sS --max-time 40 -G 'http://localhost:8080/v1/ask' \
  --data-urlencode 'key=change-me' \
  --data-urlencode 'request_id=req_myjob_20260131_0001' \
  --data-urlencode 'title=Ask4Me Demo' \
  --data-urlencode 'body=Please confirm.' \
  --data-urlencode $'mcd=:::buttons\n- [OK](ok)\n- [Later](later)\n:::'
```

## Add parameters step by step (nonStream)

The examples below use GET to show incremental parameters and use `--data-urlencode` to avoid manual URL encoding. Note that `mcd` is what makes the request actionable; without `mcd`, the user has nothing to click/type and you won’t get meaningful data back.

### 1) Add title

```bash
curl -sS --max-time 40 -G 'http://localhost:8080/v1/ask' \
  --data-urlencode 'key=change-me' \
  --data-urlencode 'title=Ask4Me Demo'
```

### 2) Add body

```bash
curl -sS --max-time 40 -G 'http://localhost:8080/v1/ask' \
  --data-urlencode 'key=change-me' \
  --data-urlencode 'title=Ask4Me Demo' \
  --data-urlencode 'body=This is a test message. Click a button or type a reply.'
```

### 3) Add expires_in_seconds

```bash
curl -sS --max-time 40 -G 'http://localhost:8080/v1/ask' \
  --data-urlencode 'key=change-me' \
  --data-urlencode 'title=Ask4Me Demo' \
  --data-urlencode 'body=Please respond within 10 minutes.' \
  --data-urlencode 'expires_in_seconds=600'
```

### 4) Add mcd (important)

```bash
curl -sS --max-time 40 -G 'http://localhost:8080/v1/ask' \
  --data-urlencode 'key=change-me' \
  --data-urlencode 'title=Ask4Me Demo' \
  --data-urlencode 'body=Choose an action, or type some text.' \
  --data-urlencode 'expires_in_seconds=600' \
  --data-urlencode $'mcd=:::buttons\n- [OK](ok)\n- [Later](later)\n:::\n\n:::input name="note" label="Note" submit="Submit"\n:::'
```

## MCD syntax (details)

MCD is an “interaction control description”. The server stores `mcd` in the database, and the interaction page at `/r/<request_id>/?k=<token>` parses it to render buttons and inputs.

The current implementation is line-based parsing. It only recognizes two structures; other content is ignored (it is not rendered as Markdown).

### 1) Buttons block

Syntax:

```text
:::buttons
- [<label>](<value>)
- [<label2>](<value2>)
:::
```

Rules:

- `label`: button text
- `value`: submitted value, appears in the terminal result as `data.action`
- The ending line must be a single line `:::`

Example:

```text
:::buttons
- [OK](ok)
- [Later](later)
:::
```

When the user clicks `OK`, the terminal event `user.submitted` contains `data` like:

```json
{ "action": "ok", "text": "" }
```

### 2) Input line

Syntax:

```text
:::input name="<name>" label="<label>" submit="<submit>"
:::
```

Rules:

- `label`: hint text above the input
- `submit`: submit button text
- `name`: parsed and stored in current version, but submission still uses `data.text` as the result field (`name` is reserved for future extensions and does not change returned JSON field names)

Example:

```text
:::input name="note" label="Note" submit="Submit"
:::
```

When the user submits input, the terminal event `user.submitted` contains `data` like:

```json
{ "action": "", "text": "user input text" }
```

### 3) Use buttons + input together

You can provide both buttons and input: clicking a button or typing text completes a submission. After submission the page shows “Submitted.”.

## SSE mode (stream=true)

If you need to receive `request.created` (includes `interaction_url`) and subsequent events in real-time, use SSE:

```bash
curl -N -sS \
  -X POST 'http://localhost:8080/v1/ask?stream=true' \
  -H 'Authorization: Bearer change-me' \
  -H 'Content-Type: application/json' \
  -d '{"title":"Ask4Me","body":"Please respond.","mcd":":::buttons\n- [OK](ok)\n:::"}'
```

SSE output format:

- One event per line: `data: <Event JSON>\n\n`
- End marker: `data: [DONE]\n\n`
- Response header includes `X-Ask4Me-Request-Id`

## JavaScript SDK (ask4me-sdk)

The SDK currently uses SSE mode by default (automatically adds `stream=true`), suitable for consuming events in real time in your program.

Install:

```bash
npm i ask4me-sdk
```

Example:

```js
import { ask } from "ask4me-sdk";

const endpoint = "http://localhost:8080/v1/ask";
const apiKey = "change-me";

const { requestId, result } = await ask({
  endpoint,
  apiKey,
  payload: {
    title: "Ask4Me Demo",
    body: "Click a button or type a reply.",
    mcd:
      ":::buttons\n" +
      "- [OK](ok)\n" +
      "- [Later](later)\n" +
      ":::\n\n" +
      ":::input name=\"note\" label=\"Note\" submit=\"Submit\"\n" +
      ":::",
    expires_in_seconds: 600
  },
  onEvent: (ev) => {
    process.stdout.write(`${JSON.stringify(ev)}\n`);
  }
});

console.log("request_id:", requestId);
console.log("final:", result);
```

## CLI (ask4me-cli)

Install:

```bash
npm i -g ask4me-cli
```

Example:

```bash
ask4me-cli -h http://localhost:8080 -k change-me --title 'Ask4Me' --body 'Please respond.'
```

## Build from source (optional)

This repository includes a GoReleaser config ([.goreleaser.yaml](./.goreleaser.yaml)). If you only want to cross-compile manually:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o dist/ask4me-linux-amd64 .
```

## Node server launcher (ask4me-server)

Downloads/starts the Go server binary automatically and helps generate config files.

Install:

```bash
npm i -g ask4me-server
```

Start (writes config to `~/.ask4me/.env` by default):

```bash
ask4me-server
```

Specify config path:

```bash
ask4me-server --config ./.env
```

Run in background:

```bash
ask4me-server -d
```

## Contributing

Issues and PRs are welcome. See [CONTRIBUTING.md](./CONTRIBUTING.md) for conventions.

## Security

If you discover a security issue, please report it privately following [SECURITY.md](./SECURITY.md).

## License

[MIT](./LICENSE)
