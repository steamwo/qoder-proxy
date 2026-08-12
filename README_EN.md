# qoder-proxy

[简体中文](README.md) · [English](README_EN.md)

A standalone local Go proxy that exposes a Qoder account through OpenAI- and Anthropic-compatible HTTP APIs.

## Supported endpoints

- `GET /v1/models` (standard; `POST /v1/models` is accepted as a compatibility alias)
- `POST /v1/chat/completions`
- `POST /v1/responses`
- `POST /v1/messages` (Anthropic Messages API)

All three generation endpoints support streaming and non-streaming responses. Text generation and function/tool calls are translated through a shared internal event model.

Per-request Qoder thinking effort is also supported and is validated against the selected model's live `thinking_config` metadata before the request is sent upstream.

## Model name mapping

Qoder's model catalogue contains an internal model identifier (`key` / `model_id`) and a user-facing `display_name`.

`qoder-proxy` deliberately keeps them separate:

- `/v1/models` exposes **only `display_name`** as the OpenAI model `id`.
- `/v1/chat/completions` accepts `display_name`, resolves it locally, and sends the real internal model ID to Qoder.
- `/v1/responses` uses the same mapping.
- `/v1/messages` uses the same `display_name` mapping for Anthropic clients.
- The Qoder request headers (`X-Model-Key`) and `model_config.key` always use the internal model ID.

For debugging/backward compatibility, an internal model ID can also be accepted as request input when it exists in the current catalogue, but it is never returned by `/v1/models`.

## Build

Requires Go 1.23 or newer.

```bash
go build -o qoder-proxy ./cmd/qoder-proxy
```

## Login

```bash
./qoder-proxy login
```

The command starts Qoder's PKCE device login flow, opens the authorization URL when possible, polls for completion, fetches the user identity, and stores the resulting credential locally.

Use this in headless environments:

```bash
./qoder-proxy login --no-browser
```

Credential location:

- macOS: `~/Library/Application Support/qoder-proxy/credentials.json`
- Linux: `${XDG_CONFIG_HOME:-~/.config}/qoder-proxy/credentials.json`
- Windows: `%AppData%\\qoder-proxy\\credentials.json`

Override it with `QODER_PROXY_CREDENTIALS`.

The current MVP stores the credential as a local JSON file with `0600` permissions where the OS supports Unix file modes. Do not share this file.

## Run

```bash
./qoder-proxy serve
```

Default address:

```text
127.0.0.1:8080
```

Override it with either:

```bash
./qoder-proxy serve --listen 127.0.0.1:9000
```

or:

```bash
QODER_PROXY_LISTEN=127.0.0.1:9000 ./qoder-proxy serve
```

The default loopback bind prevents accidental LAN exposure.

### HTTP compatibility

The local server handles browser CORS preflight (`OPTIONS`) for `/v1/*`. `GET /v1/models` is the OpenAI-standard method; `POST /v1/models` is also accepted for compatibility with clients that probe the model catalogue using POST.

## Logging

Server logs are written to stderr. The default level is `info` and includes server startup, completed HTTP requests, Qoder model refreshes, and Qoder upstream response status.

Use debug logging when diagnosing model mapping or upstream calls:

```bash
./qoder-proxy serve --log-level debug
```

Or set it with an environment variable:

```bash
QODER_PROXY_LOG_LEVEL=debug ./qoder-proxy serve
```

Supported levels are `debug`, `info`, `warn`, `error`, and `off`. Logs intentionally omit credentials, Authorization headers, and full request bodies.

Example:

```text
time=2026-08-11T14:25:00.000+08:00 level=INFO msg="server started" listen=127.0.0.1:8080 api_key_required=false
time=2026-08-11T14:25:03.000+08:00 level=INFO msg="qoder models refreshed" models=8 upstream_models=8 duration_ms=241
time=2026-08-11T14:25:08.000+08:00 level=INFO msg="qoder response" operation=chat model="Claude Sonnet 4" upstream_model=abc123 status=200 duration_ms=312
time=2026-08-11T14:25:09.000+08:00 level=INFO msg="request completed" method=POST path=/v1/responses status=200 bytes=4210 duration_ms=1276
```

### Optional local API key

Set `QODER_PROXY_API_KEY` to require a local API key on generation/model endpoints:

```bash
QODER_PROXY_API_KEY=local-secret ./qoder-proxy serve
```

OpenAI-compatible clients can use:

```text
Authorization: Bearer local-secret
```

Anthropic-compatible clients can use their normal header:

```text
x-api-key: local-secret
```

## Models

CLI:

```bash
./qoder-proxy models
```

HTTP:

```bash
curl http://127.0.0.1:8080/v1/models
```

Example response:

```json
{
  "object": "list",
  "data": [
    {
      "id": "Claude Sonnet 4",
      "object": "model",
      "created": 1780000000,
      "owned_by": "qoder"
    }
  ]
}
```

The `id` above is Qoder's `display_name`, not its internal upstream model key.

## Chat Completions

Non-streaming:

```bash
curl http://127.0.0.1:8080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "Claude Sonnet 4",
    "messages": [{"role":"user","content":"hello"}]
  }'
```

Streaming:

```bash
curl -N http://127.0.0.1:8080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "Claude Sonnet 4",
    "stream": true,
    "stream_options": {"include_usage": true},
    "messages": [{"role":"user","content":"hello"}]
  }'
```

The proxy emits standard `chat.completion.chunk` SSE frames followed by `data: [DONE]`.

### Thinking / reasoning effort

The proxy does not hard-code which models support thinking effort. It reads each model's live Qoder `thinking_config` and rejects unsupported levels with HTTP 400 instead of silently downgrading them.

OpenAI Chat Completions:

```json
{
  "model": "DeepSeek-V4-Flash",
  "reasoning_effort": "high",
  "messages": [{"role":"user","content":"solve this problem"}]
}
```

OpenAI Responses:

```json
{
  "model": "DeepSeek-V4-Flash",
  "reasoning": {"effort":"high"},
  "input": "solve this problem"
}
```

Anthropic Messages:

```json
{
  "model": "DeepSeek-V4-Flash",
  "max_tokens": 4096,
  "output_config": {"effort":"high"},
  "messages": [{"role":"user","content":"solve this problem"}]
}
```

Accepted effort strings are determined by the Qoder model entry, commonly `low`, `medium`, `high`, `xhigh`, or `max`. `auto`/`default` means no request-level override. `off` is accepted as an alias of Qoder `none` when the model advertises a disabled-thinking mode. A model such as Kimi-K3 that does not advertise effort levels will reject `reasoning_effort`/`reasoning.effort`/`output_config.effort` rather than pretending the setting took effect.

On the Qoder wire, the validated value is sent as `parameters.reasoningEffort`. Debug logging includes the selected effort and the effort levels advertised by the model, but never logs prompt content or credentials.

## Anthropic Messages API

The proxy also exposes Anthropic-compatible Messages at `POST /v1/messages`. The request `model` is still the Qoder `display_name`; the internal Qoder model key is never exposed to the client.

Non-streaming:

```bash
curl http://127.0.0.1:8080/v1/messages \
  -H 'Content-Type: application/json' \
  -H 'anthropic-version: 2023-06-01' \
  -H 'x-api-key: local-secret' \
  -d '{
    "model": "Claude Sonnet 4",
    "max_tokens": 1024,
    "messages": [{"role":"user","content":"hello"}]
  }'
```

Streaming:

```bash
curl -N http://127.0.0.1:8080/v1/messages \
  -H 'Content-Type: application/json' \
  -H 'anthropic-version: 2023-06-01' \
  -d '{
    "model": "Claude Sonnet 4",
    "max_tokens": 1024,
    "stream": true,
    "messages": [{"role":"user","content":"hello"}]
  }'
```

Supported Anthropic message features include:

- string and text-block `system` prompts
- string and text-block user/assistant content
- `tools` with `input_schema`
- assistant `tool_use` history
- user `tool_result` history
- streaming `message_start`, `content_block_start`, `content_block_delta`, `content_block_stop`, `message_delta`, and `message_stop` events
- Anthropic-shaped JSON errors and streaming `error` events
- Qoder free-account queue retry/heartbeat behavior
- Qoder no-quota errors mapped to HTTP 429 / Anthropic `rate_limit_error`

`anthropic-version` and `anthropic-beta` headers are accepted for client compatibility but are not forwarded to Qoder. Image/document content blocks and Anthropic-native thinking *content blocks* are not currently translated to Qoder. Request-level `output_config.effort` is supported, and `thinking: {"type":"disabled"}` maps to Qoder's `none` mode when the selected model advertises it. `tool_choice`, `temperature`, `top_p`, and stop sequences are accepted by the public request schema, but are not forwarded to Qoder; the upstream request sends `max_tokens` plus the validated `reasoningEffort` override when present.

## Responses API

Non-streaming:

```bash
curl http://127.0.0.1:8080/v1/responses \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "Claude Sonnet 4",
    "input": "hello"
  }'
```

Streaming:

```bash
curl -N http://127.0.0.1:8080/v1/responses \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "Claude Sonnet 4",
    "input": "hello",
    "stream": true
  }'
```

The Responses stream emits events such as:

- `response.created`
- `response.in_progress`
- `response.output_item.added`
- `response.content_part.added`
- `response.output_text.delta`
- `response.output_text.done`
- `response.function_call_arguments.delta`
- `response.function_call_arguments.done`
- `response.output_item.done`
- `response.completed`

The external Responses event shape follows the OpenAI Responses streaming contract:

- https://platform.openai.com/docs/api-reference/responses-streaming
- https://platform.openai.com/docs/api-reference/models

## Function calling

Chat Completions tools are passed to Qoder in OpenAI Chat tool form.

Responses function tools are converted from:

```json
{
  "type": "function",
  "name": "get_weather",
  "description": "Get weather",
  "parameters": {"type":"object"}
}
```

to the Chat-style function tool shape expected by the current Qoder agent endpoint.

Qoder tool-call deltas are converted back to the corresponding Chat Completions or Responses streaming event format.

Responses input also understands `function_call` and `function_call_output` items for tool-call continuation.

## Other commands

```bash
./qoder-proxy status
./qoder-proxy logout
```

## Environment variables

| Variable | Purpose |
| --- | --- |
| `QODER_PROXY_LISTEN` | HTTP listen address, default `127.0.0.1:8080` |
| `QODER_PROXY_API_KEY` | Optional local Bearer API key |
| `QODER_PROXY_CREDENTIALS` | Override credential JSON path |
| `QODER_PROXY_LOG_LEVEL` | Log level: `debug`, `info`, `warn`, `error`, `off` |
| `HTTP_PROXY` / `HTTPS_PROXY` / `NO_PROXY` | Standard Go HTTP proxy environment variables |

## Current scope

The first version focuses on the local single-account use case:

- one Qoder credential
- Qoder PKCE device login
- COSY request signing
- Qoder model discovery with a five-minute in-memory cache
- public `display_name` / private upstream model ID mapping
- text input/output
- function calling
- OpenAI Chat Completions and Responses streaming/non-streaming output

Cloudflare Workers, D1, KV, account pools, multi-provider routing, gateway quotas, and admin UI are intentionally not part of this project.

Multimodal Responses input is not translated in the MVP; text content is supported.


## Qoder no-quota errors

Qoder business error `112` is treated as an exhausted/unavailable account quota.
The proxy does not retry it. If it is the first upstream SSE event, the proxy
returns an OpenAI-style JSON error before starting the local SSE response:

```http
HTTP/1.1 429 Too Many Requests
Content-Type: application/json
```

```json
{
  "error": {
    "message": "Qoder account has no available quota for this request. Pricing: https://qoder.com/pricing?client=qoder",
    "type": "insufficient_quota",
    "code": "insufficient_quota"
  }
}
```

This is separate from Qoder queue code `10605`, which remains automatically
retryable according to the queue settings below.

## Qoder free-account queue handling

Qoder may put free-tier requests into a slow queue and return a business-layer
403 envelope with code `10605`, `isQueued: true`, and a suggested
`retryAfterSeconds` value even though the HTTP response itself is 200.

qoder-proxy detects this condition before exposing it as an OpenAI error. It
waits for the server-provided retry interval and reissues a freshly signed
Qoder request. For streaming `/v1/chat/completions`, the proxy emits SSE comment
heartbeats while queued so browser/client idle timers do not treat the local
connection as dead.

Defaults:

```text
--queue-retries 20
--queue-max-wait 10m
```

Environment equivalents:

```text
QODER_PROXY_QUEUE_RETRIES=20
QODER_PROXY_QUEUE_MAX_WAIT=10m
```

Set `--queue-retries 0` to disable automatic queue retries.


## Desktop UI refresh

The Gio desktop client uses a light, Apple-inspired native visual system: generous spacing, cool gray surfaces, restrained blue/mint status colors, rounded elevated panels, and a status-first information hierarchy. Five dedicated areas cover Dashboard, account quota, models, structured logs, and settings. The UI remains native Gio and does not use WebView.

## Desktop app (Gio, no WebView)

Version `0.3.2-desktop` adds an optional native-rendered Gio desktop application while keeping the CLI intact.

```text
cmd/qoder-proxy            CLI
cmd/qoder-proxy-desktop    Gio desktop app
internal/...               shared proxy/Qoder implementation
```

The desktop app provides Qoder login/logout, account quota, searchable model capabilities, proxy start/stop with uptime, bounded persistent searchable logs with detail selection, grouped settings, a Data & Privacy view that reveals actual local paths, and platform tray integration. Windows uses a Win32 notification-area menu with open/start-stop/refresh/quit actions; Linux uses StatusNotifierItem over D-Bus with activate-to-open and secondary-activate-to-toggle behavior.

Qoder quota is fetched with the logged-in Bearer token from `https://openapi.qoder.sh/api/v2/quota/usage`. The UI recognizes personal quota (`userQuota`), organization resources (`orgResourcePackage`), plan/subscription labels, percentages, and quota expiry/reset timestamps.

Build instructions are in [`docs/desktop.md`](docs/desktop.md). On a normal development machine with Go module network access:

```powershell
./scripts/build-desktop.ps1

The desktop build script runs `go mod tidy` first to generate/update `go.sum`, and aborts on any failed Go command.
```

or on Linux:

```bash
./scripts/build-desktop.sh
```
