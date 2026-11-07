# Ask4Me

> English | [中文](README.zh-CN.md)


https://github.com/user-attachments/assets/13c395e3-8be6-4a44-a460-a966de29b2d9

Ask4Me is a self-hosted service: you send a “request” via API, the server pushes an interaction link to your notification channel (ServerChan / Apprise). You open the link on your phone or in a browser, click a button or type text to submit, and the original HTTP long-poll request receives the final result (JSON or SSE).

Conceptually, this is Human-in-the-Loop: it might be one of the simplest Human-in-the-Loop setups you can self-host.

## One-request demo (curl)

Provide either `mcd` or `jsonforms.schema`. Without any controls, the user will receive a notification but won’t have anything to submit.

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
- `ASK4ME_API_KEY`: API auth key (required in single-user mode; ignored when `multi_user.enabled` is on — add the old key to `multi_user.auth.keys`; see “Multi-user / trial mode” below)
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

### 3) Docker (amd64 / arm64)

Images are published to Docker Hub and GHCR on every release:

```bash
# Docker Hub
docker pull easychen/ask4me:latest

# GitHub Container Registry
docker pull ghcr.io/easychen/ask4me:latest
```

Build the minimal image yourself (`scratch`, no shell, static binary + CA + zoneinfo):

```bash
docker build --target slim -t ask4me:slim .
```

Run with an env file:

```bash
docker run -d \
  --name ask4me \
  -p 8080:8080 \
  -v $(pwd)/ask4me.db:/data/ask4me.db \
  --env-file .env \
  easychen/ask4me:latest
```

Or pass variables directly:

```bash
docker run -d \
  --name ask4me \
  -p 8080:8080 \
  -v $(pwd)/ask4me.db:/data/ask4me.db \
  -e ASK4ME_BASE_URL=https://your-domain.com \
  -e ASK4ME_API_KEY=your-key \
  -e ASK4ME_SERVERCHAN_SENDKEY=your-sendkey \
  easychen/ask4me:latest
```

The SQLite database is stored at `/data/ask4me.db` inside the container — mount a host volume to persist it across restarts.

Ubuntu deployment guide (Chinese; Docker Compose + Caddy, standard and `scratch` slim images): [docs/ubuntu-deploy.md](docs/ubuntu-deploy.md).

## Multi-user / trial mode (off by default)

The default is still single-user: one global `api_key` and one global notification channel. Set `multi_user.enabled: true` to run an **accountless multi-tenant** instance: each API key is a tenant, used only for request isolation and quotas — no registration or login.

### 1) Config

```yaml
multi_user:
  enabled: true
  auth:
    mode: allowlist          # allowlist | open
    keys:
      - key: "a4m_replace_me_123456"
        name: "alice"
        requests_per_day: 500
        max_pending: 10
    # open: users self-generate keys as a4m_<invite>_<random>
    invite_codes: ["trial2026"]
  defaults:
    requests_per_day: 200
    max_pending: 5
    max_expires_in_seconds: 86400
  limits:
    per_ip_requests_per_hour: 60
    per_ip_new_keys_per_day: 10
    global_requests_per_day: 5000
    trust_proxy: false
  allowed_apprise_schemes: ["schan","tgram","discord","slack","ntfy","gotify"]
```

- `allowlist`: only explicitly listed keys are accepted; `api_key` is **not** automatically accepted in multi-user mode.
- `open`: listed keys still work; keys matching `a4m_<invite>_<random>` are also accepted, where `invite` is in `invite_codes` and `random` is at least 16 chars. Remove an invite to revoke a batch.
- With `enabled: false`, behavior is unchanged and `api_key` is still required.
- In multi-user mode `api_key` is ignored; to keep the old key, add it explicitly to `multi_user.auth.keys`. Pre-migration requests without an owner are not readable via the API and will expire.

**Upgrade compatibility**: old single-user configs need no changes. Without `multi_user` (or with `enabled: false`), `api_key`, the global notification channel, `?key=`, and the no-channel `notify.failed` behavior are all unchanged; only explicitly enabling `multi_user` switches to the new rules. If the old `api_key` is still present when multi-user is enabled: `open` mode prints a warning, while `allowlist` mode fails with a config error (no `auth.keys`); add it to `auth.keys` as instructed.

Generate a self-service key for `open` mode:

```bash
printf 'a4m_%s_%s\n' 'trial2026' "$(openssl rand -hex 16)"
```

### 2) Per-request notification channel

In multi-user mode every key **must** send `notify`; the global `serverchan_sendkey` / `apprise_urls` only apply in single-user mode, so no user request can push to the operator's phone. A per-request channel looks like this:

```bash
curl -sS --max-time 120 -X POST 'https://your-domain.com/v1/ask' \
  -H 'Authorization: Bearer a4m_trial2026_xxxxxxxxxxxxxxxx' \
  -H 'Content-Type: application/json' \
  -d '{
    "title": "Approve deploy",
    "body": "Please confirm.",
    "notify": { "serverchan_sendkey": "SCT..." },
    "mcd": ":::buttons\n- [OK](ok)\n- [Later](later)\n:::"
  }'
```

- Use `notify.serverchan_sendkey` or `notify.apprise_urls`; both may be set, and ServerChan wins.
- Credentials are kept in memory for this request only — never stored in the DB or in events. In multi-user mode, `notify.failed` / `notify.sent` events are redacted.
- In multi-user mode the API key is read from the `Authorization` header only; `?key=` is rejected to avoid leaking keys into URL logs.
- In multi-user mode a request without `notify` fails with `400` before it is created; single-user mode without a channel still ends in `notify.failed`.

### 3) Quotas

- Per key: `requests_per_day`, `max_pending`
- Per IP: `per_ip_requests_per_hour`, `per_ip_new_keys_per_day`
- Global: `global_requests_per_day`
- `expires_in_seconds` is clamped to `max_expires_in_seconds`
- Over-limit responses are `429` with `Retry-After` and JSON `{"error":"quota_exceeded",...}`

Note: in `open` mode a per-key quota can be bypassed by generating a new key; the per-IP and global limits are the real backstop. For a public trial, add Cloudflare or gateway rate limiting.

### 4) Request isolation

In multi-user mode `requests` stores `api_key_hash`, and every `request_id` continuation and SSE replay checks the owner. A different key gets `404` for the same `request_id` and cannot read another tenant's answer. Pre-migration requests without an owner are not readable and will expire.

### 5) Environment variables

`ASK4ME_MULTI_USER_ENABLED`, `ASK4ME_MULTI_USER_MODE`, `ASK4ME_MULTI_USER_KEYS`, `ASK4ME_MULTI_USER_INVITE_CODES`, `ASK4ME_MULTI_USER_REQUESTS_PER_DAY`, `ASK4ME_MULTI_USER_MAX_PENDING`, `ASK4ME_MULTI_USER_MAX_EXPIRES_IN_SECONDS`, `ASK4ME_MULTI_USER_PER_IP_REQUESTS_PER_HOUR`, `ASK4ME_MULTI_USER_PER_IP_NEW_KEYS_PER_DAY`, `ASK4ME_MULTI_USER_GLOBAL_REQUESTS_PER_DAY`, `ASK4ME_MULTI_USER_TRUST_PROXY`, `ASK4ME_ALLOWED_APPRISE_SCHEMES`.

> E2EE is the next phase and is not implemented yet; there is no `e2ee` config section in this version.

## End-to-end encryption (E2EE, off by default)

When E2EE is enabled, the caller encrypts locally in the SDK/CLI and the server stores only ciphertext. Answers are encrypted in the browser and decrypted by the caller. The server never sees the password and never performs cryptography. The full format, threat model, and test vectors are in [E2EE.md](E2EE.md).

### Config

```yaml
e2ee:
  enabled: true
  kdf_iterations: 310000
  encrypt_answers_default: true
```

- `enabled: true` only allows E2EE on this server; it **never forces** a request. A request opts in with `e2ee.enabled: true`, and requests without it keep working in plaintext.
- On one multi-user instance, some users can encrypt and others can stay plaintext.
- HTTPS is required (`http://localhost` / `127.0.0.1` is allowed for development).
- Off by default; when the server disables it, requests carrying `e2ee` get `400`.

### Request shape

```json
{
  "title": "enc:v1:...",
  "body": "enc:v1:...",
  "jsonforms": {
    "schema": { "type": "object", "properties": { "name": { "type": "string", "title": "enc:v1:..." } } },
    "data": "enc:v1:..."
  },
  "e2ee": {
    "enabled": true,
    "salt": "base64url-16B",
    "iterations": 310000,
    "answers": true
  },
  "notify": { "serverchan_sendkey": "SCT..." }
}
```

- Structure (`schema` / `uischema` / MCD blocks) stays plaintext so JSON Forms can still render.
- Body, title, form display strings, initial `data`, and answers are ciphertext.
- Button `action` and MCD values stay plaintext (ServerChan Action Links depend on them).
- Notifications only contain “encrypted message + link”, never the body.

### SDK / CLI

```js
const { result } = await ask({
  endpoint: "https://your-domain.com/v1/ask",
  apiKey: "a4m_...",
  e2ee: { password: "shared-out-of-band" },
  payload: {
    title: "Approve deploy",
    body: "Please confirm.",
    notify: { serverchan_sendkey: "SCT..." }
  }
});
// result.data.text / result.data.payload are already decrypted
```

```bash
ASK4ME_E2EE_PASSWORD='...' ask4me-cli -h https://your-domain.com -k a4m_... \
  --title 'Approve deploy' --body 'Please confirm.' --notify-sendkey 'SCT...'
```

- Share the password **out of band**, never in the same message as the notification link; otherwise encryption is pointless.
- The server does not store the password; there is no recovery.
- The page JS is served by the server, so a compromised server could replace it and steal the password. E2EE protects databases/backups/push channels/logs, not a malicious server. Metadata (who, when, size, channel) remains visible.

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

- Provide either `mcd` or `jsonforms.schema`. If both are omitted, the server falls back to a default OK button.
- `--max-time 120` is just an example to avoid waiting forever in your terminal. If you don’t open the notification and submit within 120 seconds, curl exits with a timeout. You can resume waiting with `request_id` (see below).
- In nonStream mode the response does not include `interaction_url`; the interaction link is delivered via your notification channel.
- ServerChan 3 can render clickable Action Links inside notification Markdown (no browser needed). To enable it, pass `serverchan_action_links=true` and make sure `ASK4ME_BASE_URL` is `https` (Action Link only supports converting `https` links to `sccallback://`).

Example response (returned after terminal state):

```json
{
  "request_id": "req_xxx",
  "last_event_type": "user.submitted",
  "data": { "action": "ok", "text": "" },
  "last_event_id": "evt_xxx"
}
```

Example: enable ServerChan 3 Action Links (POST):

```bash
curl -sS --max-time 120 \
  -X POST 'http://localhost:8080/v1/ask' \
  -H 'Authorization: Bearer change-me' \
  -H 'Content-Type: application/json' \
  -d '{"title":"Ask4Me Demo","body":"Click a button to respond.","serverchan_action_links":true,"mcd":":::buttons\n- [OK](ok)\n- [Later](later)\n:::"}'
```

### 1b) POST with JSON Forms (schema-driven form)

`jsonforms` is POST-only (it is not supported via GET query params). If `jsonforms.schema` is present, the interaction page will render the form and ignore `mcd`.

```bash
curl -sS --max-time 120 \
  -X POST 'http://localhost:8080/v1/ask' \
  -H 'Authorization: Bearer change-me' \
  -H 'Content-Type: application/json' \
  -d '{
    "title": "JSON Forms Demo",
    "body": "Please fill the form.",
    "jsonforms": {
      "schema": {
        "type": "object",
        "properties": {
          "name": { "type": "string", "minLength": 1 },
          "age": { "type": "integer", "minimum": 0 }
        },
        "required": ["name"]
      },
      "uischema": {
        "type": "VerticalLayout",
        "elements": [
          { "type": "Control", "scope": "#/properties/name" },
          { "type": "Control", "scope": "#/properties/age" }
        ]
      },
      "data": { "age": 18 },
      "submit_label": "Submit"
    },
    "expires_in_seconds": 600
  }'
```

When the user submits the form, the terminal event `user.submitted` contains `data` like:

```json
{
  "action": "",
  "text": "",
  "payload": { "name": "Alice", "age": 18 }
}
```

Possible terminal `last_event_type` values:

- `user.submitted`: user submitted successfully (button or input)
- `request.expired`: expired without submission
- `notify.failed`: notification delivery failed (usually missing config or channel error)

### 1c) JSON Forms UI extensions (collapsible / long text / markdown)

In addition to the built-in JSON Forms UI elements (`Control` / `Group` / `VerticalLayout` / `Label` / etc.), Ask4Me ships three custom renderers triggered purely via the `options` field — no extra `type` is introduced, so the UI schema remains valid JSON Forms. If none of the relevant options are set, behavior falls back to the default vanilla renderer.

**1. Collapsible Group** — wrap a section of fields and collapse it by default. Useful for "advanced settings" or any long form area.

```json
{
  "type": "Group",
  "label": "Advanced settings",
  "options": { "collapsible": true, "defaultOpen": false, "maxHeight": 320 },
  "elements": [
    { "type": "Control", "scope": "#/properties/foo" },
    { "type": "Control", "scope": "#/properties/bar" }
  ]
}
```

- `collapsible: true` → renders as native `<details><summary>` with `label` as the header
- `defaultOpen` (default `false`) → initial expanded state
- `maxHeight` (number or CSS string) → caps the inner content area; overflow scrolls

**2. Long static label** — show a long block of static instructions/disclaimer text without binding to data. Add `maxHeight` for scrolling, `collapsible` to default-collapse.

```json
// scrollable only
{
  "type": "Label",
  "text": "<long instructions here>",
  "options": { "maxHeight": 240 }
}

// collapsed by default; click to expand
{
  "type": "Label",
  "text": "<long instructions here>",
  "options": {
    "collapsible": true,
    "defaultOpen": false,
    "summary": "Read the full notice",
    "maxHeight": 280
  }
}
```

- `summary` (optional) → text shown on the always-visible header. Falls back to `"Details"` when omitted.
- `markdown: true` → render `text` as Markdown (GFM: headings / lists / tables / code blocks / links / images / blockquote). Sanitized with DOMPurify so `<script>`, event handlers and `javascript:` URLs are stripped. When set, the tester triggers even without `collapsible` / `maxHeight`.

**3. Read-only long text bound to data** — render a string field as a non-editable scrollable block (e.g. echo back a long generated description). Opt-in with `readonlyBlock: true`.

```json
{
  "type": "Control",
  "scope": "#/properties/description",
  "label": "System notice",
  "options": {
    "readonlyBlock": true,
    "collapsible": true,
    "defaultOpen": false,
    "maxHeight": 200
  }
}
```

- Without `readonlyBlock: true` the control falls through to the default editor.
- When `collapsible: true`, the control's `label` is used as the summary unless `options.summary` is provided.
- `markdown: true` → render the bound string as Markdown (same parsing + sanitization as the Long Label).

**Markdown example** — render a release notice as Markdown with image, list and link:

```json
{
  "type": "Label",
  "text": "## Release v1.4.0\n\n- Add **collapsible** form sections\n- Fix [issue 42](https://example.com/issues/42)\n\n![logo](https://example.com/logo.png)",
  "options": { "markdown": true, "maxHeight": 280 }
}
```

**Combined example** — a JSON Forms request that puts a long notice up top (collapsed) and a normal form below:

```bash
curl -sS --max-time 120 \
  -X POST 'http://localhost:8080/v1/ask' \
  -H 'Authorization: Bearer change-me' \
  -H 'Content-Type: application/json' \
  -d '{
    "title": "Confirm deployment",
    "jsonforms": {
      "schema": {
        "type": "object",
        "properties": {
          "approve": { "type": "boolean" },
          "comment": { "type": "string" }
        }
      },
      "uischema": {
        "type": "VerticalLayout",
        "elements": [
          {
            "type": "Label",
            "text": "This action will roll out v1.4.0 to production.\n\nChangelog: ...(very long)...",
            "options": { "collapsible": true, "summary": "View changelog", "maxHeight": 240 }
          },
          { "type": "Control", "scope": "#/properties/approve", "label": "I approve" },
          { "type": "Control", "scope": "#/properties/comment", "label": "Comment" }
        ]
      },
      "submit_label": "Confirm"
    }
  }'
```

### 2) GET (environments without headers)

If you can’t easily set the `Authorization: Bearer ...` header, use GET with a `key` query param:

```bash
curl -sS --max-time 120 -G 'http://localhost:8080/v1/ask' \
  --data-urlencode 'key=change-me' \
  --data-urlencode 'title=Ask4Me Demo' \
  --data-urlencode 'body=Click a button to respond.' \
  --data-urlencode $'mcd=:::buttons\n- [OK](ok)\n- [Later](later)\n:::'
```

Security note: query params may be logged by proxies/servers. Prefer POST + Authorization when possible. In multi-user mode `?key=` is rejected; use the `Authorization` header.

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

The examples below use GET to show incremental parameters and use `--data-urlencode` to avoid manual URL encoding. Note that `mcd` is what makes the request actionable in GET mode (JSON Forms is POST-only).

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

In multi-user mode, put `notify` inside `payload`; the SDK passes it through (the key still goes in the Authorization header via `apiKey`).

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

In multi-user mode, pass the notification channel with the request:

```bash
ask4me-cli -h https://your-domain.com -k a4m_trial2026_xxxxxxxxxxxxxxxx \
  --title 'Approve deploy' --body 'Please confirm.' \
  --notify-sendkey 'SCT...'
# or repeatable: --apprise-url 'schan://...'
```

## Build from source (optional)

Requires Go **1.27.3+** (declared in `go.mod`; Go 1.21+ will auto-download the matching toolchain). This repository includes a GoReleaser config ([.goreleaser.yaml](./.goreleaser.yaml)). If you only want to cross-compile manually:

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
