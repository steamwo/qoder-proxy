# qoder-proxy

[简体中文](README.md) · [English](README_EN.md)

A Go proxy that connects a Qoder account to local OpenAI- and Anthropic-compatible APIs. The project ships three front ends over the same credential, settings, model registry, and proxy core: a lightweight browser-managed service, a native Gio desktop app, and a headless service.

> The default listen address is `127.0.0.1:9000`. The `/admin/*` management surface is restricted to loopback clients even if you intentionally expose the compatible API on a LAN address.

## Disclaimer

> This project is independently maintained by **steamwo** as an unofficial, non-commercial interoperability and technical research project. It is not affiliated with, sponsored by, authorized by, or endorsed by Qoder or its operators or related companies.

- Qoder names, trademarks, services, and products belong to their respective owners. Names are used only to identify the compatibility target.
- The software is provided as-is, without guarantees of availability, stability, continued compatibility, security, or fitness for a particular purpose. Upstream APIs, account policies, terms, and risk controls may change at any time.
- Users are responsible for reading and complying with applicable Qoder/model-provider terms, account policies, and local laws, and for deciding whether their use is permitted.
- Account restrictions, bans, quota loss, data loss, outages, third-party claims, and other direct or indirect losses arising from use are the user's responsibility to assess. To the maximum extent permitted by applicable law, the author is not liable for use or inability to use this project.
- The project is not intended to bypass payment, quota, access-control, or anti-abuse systems, or to facilitate abuse, attacks, or fraud.
- Rights holders may contact the maintainer through the repository if they believe project content needs attention.

**Author: steamwo**

## Feature overview

### Compatible APIs

- OpenAI Chat Completions: `POST /v1/chat/completions`
- OpenAI Responses: `POST /v1/responses`
- Anthropic Messages: `POST /v1/messages`
- OpenAI-style model list: `GET /v1/models`, with `POST /v1/models` compatibility
- Streaming and non-streaming responses
- Function Calling / Tool Use
- OpenAI- and Anthropic-shaped errors
- Qoder free-account queue retries, streaming heartbeats, and quota-error mapping

### Live model capabilities

Model behavior is discovered from the current Qoder account instead of being hard-coded locally:

- merge model collections from multiple top-level server scenes instead of reading only `chat`
- retain each model's original `server_scene`
- keep Qoder `display_name` as the public model ID returned by `/v1/models`
- keep internal Qoder `key/model_id` values private to upstream requests
- preserve deterministic public-name deduplication
- read context-window choices from live `context_config`
- read reasoning levels from live `thinking_config`
- treat `price_factor` as optional model metadata
- when an active promotion provides `promotion.discount_factor`, show that as the current multiplier in the desktop/browser model table
- show `—` when no factor is supplied, while preserving a valid `0x` factor

### Three runtime modes

| Entry point | Best for | Login / management |
| --- | --- | --- |
| `qoder-proxy-web-*` | Lightweight local service and browser management | Built-in `/admin/` UI for login, quota, models, logs, and settings |
| `qoder-proxy-desktop-*` | Daily desktop use | Native Gio UI, no WebView, tray integration and full local controls |
| `qoder-proxy-headless-*` | Servers and low-overhead environments | No login UI; expects credentials created by the Web or Desktop app |

All three modes share:

- the Qoder credential file
- `desktop.json`
- per-model reasoning and context defaults
- the optional local API key
- queue policy
- the same proxy backend

## Downloads

Formal GitHub Releases contain six binaries:

```text
qoder-proxy-desktop-windows-amd64.exe
qoder-proxy-web-windows-amd64.exe
qoder-proxy-headless-windows-amd64.exe
qoder-proxy-desktop-linux-amd64
qoder-proxy-web-linux-amd64
qoder-proxy-headless-linux-amd64
```

Release page:

https://github.com/steamwo/qoder-proxy/releases

## Quick start

### Option A: Web-managed service

Windows:

```powershell
.\qoder-proxy-web-windows-amd64.exe
```

Linux:

```bash
chmod +x ./qoder-proxy-web-linux-amd64
./qoder-proxy-web-linux-amd64
```

By default it opens:

```text
http://127.0.0.1:9000/admin/
```

The compatible API and admin UI share one listener:

```text
http://127.0.0.1:9000/v1/...
http://127.0.0.1:9000/admin/...
```

Options:

```text
--no-browser   do not open the browser on startup
--shutdown     ask an already running local Web service to exit
```

Use the admin page for the initial Qoder login. The default setting is `auto_start=true`, so the proxy starts automatically when usable credentials are available.

### Option B: Native desktop app

Windows:

```text
qoder-proxy-desktop-windows-amd64.exe
```

Linux:

```bash
chmod +x ./qoder-proxy-desktop-linux-amd64
./qoder-proxy-desktop-linux-amd64
```

The desktop app provides:

- Qoder login/logout
- account identity, plan, quota, and reset information
- searchable model catalog
- per-model default context window
- live model multiplier
- per-model default reasoning effort
- proxy start/stop, listen address, and uptime
- optional local API key
- free-account queue settings
- structured logs and event details
- resolved local data paths
- Windows notification-area integration / Linux StatusNotifierItem

### Option C: Headless

Headless mode intentionally has no login flow. It is intended for machines that already have a valid local credential:

Windows:

```powershell
.\qoder-proxy-headless-windows-amd64.exe
```

Linux:

```bash
chmod +x ./qoder-proxy-headless-linux-amd64
./qoder-proxy-headless-linux-amd64
```

The explicit form is also accepted:

```bash
./qoder-proxy-headless-linux-amd64 serve
```

It reads the same credentials and settings as the Web/Desktop apps and starts the compatible API directly.

## Build from source

Go 1.23+ is required.

Headless:

```bash
go build -trimpath -o dist/qoder-proxy-headless ./cmd/qoder-proxy
```

Web-managed service:

```bash
go build -trimpath -o dist/qoder-proxy-web ./cmd/qoder-proxy-service
```

Native Windows desktop:

```powershell
./scripts/build-desktop.ps1
```

Native Linux desktop:

```bash
./scripts/build-desktop.sh
```

The Linux desktop build requires the EGL, Vulkan, Wayland, X11 and related development packages used by Gio. See [docs/desktop.md](docs/desktop.md).

## API examples

The examples below use the default `127.0.0.1:9000` address with no local API key configured.

### Models

```bash
curl http://127.0.0.1:9000/v1/models
```

The public model ID is Qoder's `display_name`. Internal upstream IDs are not exposed in the public model catalog.

### OpenAI Chat Completions

Non-streaming:

```bash
curl http://127.0.0.1:9000/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "Claude Sonnet 4",
    "messages": [{"role":"user","content":"hello"}]
  }'
```

Streaming:

```bash
curl -N http://127.0.0.1:9000/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "Claude Sonnet 4",
    "stream": true,
    "stream_options": {"include_usage": true},
    "messages": [{"role":"user","content":"hello"}]
  }'
```

### OpenAI Responses

```bash
curl http://127.0.0.1:9000/v1/responses \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "Claude Sonnet 4",
    "input": "hello"
  }'
```

### Anthropic Messages

```bash
curl http://127.0.0.1:9000/v1/messages \
  -H 'Content-Type: application/json' \
  -H 'anthropic-version: 2023-06-01' \
  -d '{
    "model": "Claude Sonnet 4",
    "max_tokens": 1024,
    "messages": [{"role":"user","content":"hello"}]
  }'
```

## Reasoning effort

The proxy validates reasoning settings from each model's live `thinking_config`; it does not maintain a static model whitelist.

Common values include:

```text
none, low, medium, high, xhigh, max
```

`auto` / `default` means no request-level override. When the model explicitly supports disabling thinking, `off` maps to Qoder `none`.

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

An explicitly unsupported effort returns HTTP 400. If a model exposes no configurable reasoning depth, generic client hints are not incorrectly forwarded upstream.

## Context windows

Available context tiers come from live `context_config`. The Desktop and Web UIs can persist a default context window for a specific upstream model while still allowing request-level behavior to take precedence where supported.

Capabilities are revalidated against the latest server model metadata instead of relying on a long-lived static table.

## Model multipliers

The displayed multiplier follows current server model metadata:

1. the base multiplier comes from `price_factor`
2. a missing `price_factor` is unknown, not zero
3. `0` is a valid factor and is rendered as `0x`
4. when `promotion.active=true` and `promotion.discount_factor` exists, the UI prefers the promotional multiplier
5. multiplier display is informational; qoder-proxy does not modify Qoder billing or routing based on it

Models, multipliers, promotions, and parameters can all change server-side; use the live values returned for the current account.

## Local API key

Configure the optional local API key from Web/Desktop settings. When enabled:

OpenAI clients:

```text
Authorization: Bearer <local-api-key>
```

Anthropic clients:

```text
x-api-key: <local-api-key>
```

The local API key protects the compatible API. The browser admin surface is separately restricted to loopback clients.

## Qoder queue and quota errors

### Free-account queue

When Qoder returns business code `10605` with `isQueued: true` and a suggested `retryAfterSeconds`, the proxy:

- waits according to the upstream recommendation
- re-signs and retries the request
- respects the configured retry count and total wait limit
- emits heartbeats for streaming requests to reduce client idle timeouts

Default settings:

```json
{
  "queue_retries": 20,
  "queue_max_wait": "10m"
}
```

### No quota

Qoder business code `112` maps to HTTP 429 and an OpenAI- or Anthropic-compatible quota error. It is not treated as a queue retry.

## Local data

### Credentials

- Windows: `%AppData%\qoder-proxy\credentials.json`
- Linux: `${XDG_CONFIG_HOME:-~/.config}/qoder-proxy/credentials.json`
- macOS: `~/Library/Application Support/qoder-proxy/credentials.json`

Override the credential path with:

```text
QODER_PROXY_CREDENTIALS=/custom/path/credentials.json
```

The credential file contains access tokens. Do not share it or commit it to version control.

### Settings

- Windows: `%AppData%\qoder-proxy\desktop.json`
- Linux: `${XDG_CONFIG_HOME:-~/.config}/qoder-proxy/desktop.json`

Typical defaults:

```json
{
  "listen": "127.0.0.1:9000",
  "queue_retries": 20,
  "queue_max_wait": "10m",
  "auto_start": true,
  "minimize_to_tray": true,
  "tray_notifications": true
}
```

The file may also contain the local API key and per-model reasoning/context defaults.

### Logs

- Windows: `%LocalAppData%\qoder-proxy\logs\desktop.log`
- Linux: `${XDG_CACHE_HOME:-~/.cache}/qoder-proxy/logs/desktop.log`

Persistent logs are bounded to 4 MB. Clear Logs removes both in-memory and on-disk content. Logs contain diagnostic metadata such as paths, status codes, models, and durations, but not credentials, Authorization headers, or complete request bodies.

## Security boundaries

- default listener: `127.0.0.1:9000`
- if you expose the API to a LAN, configure a local API key and apply your own network controls
- `/admin/*` remains loopback-only even when the API listener is non-loopback
- credentials and settings use user-private permissions where the operating system supports them
- do not upload credential, settings, or log files to untrusted third parties

## CI and releases

`.github/workflows/build.yml` runs for:

- pushes to `main`
- pull requests targeting `main`
- manual `workflow_dispatch`
- `v*` tags

Quality gates:

```text
go test ./...
go vet ./...
```

Pull requests build Windows/Linux Web and Headless binaries for validation. Formal releases additionally build the native Desktop binaries.

A formal release can be triggered by:

- pushing a semver-compatible `v*` tag
- pushing a commit to `main` whose first commit-message line is `release: vX.Y.Z`

The workflow creates a draft GitHub Release, uploads Desktop/Web/Headless binaries for Windows and Linux, and publishes it as latest only after all required builds succeed.

## Repository layout

```text
cmd/qoder-proxy            Headless entry point
cmd/qoder-proxy-service    Browser-managed service entry point
cmd/qoder-proxy-desktop    Native Gio desktop entry point
internal/credential        Qoder credential persistence
internal/qoder             Login, refresh, signing, models, quota, streaming
internal/protocol          Provider-neutral request/event types
internal/openai            Chat Completions / Responses adapters
internal/anthropic         Anthropic Messages adapter
internal/server            Compatible API routes and local authentication
internal/desktop           Desktop/Web service, settings, logs, tray
assets                     Branding assets
scripts                    Desktop build scripts
```

More documentation:

- [Desktop build and local data](docs/desktop.md)
- [Architecture](docs/architecture.md)
- [Design notes](docs/design.md)

## Current scope

The project currently focuses on a local single-account compatibility proxy:

- one Qoder credential
- Qoder PKCE login and token refresh
- COSY request signing
- multi-server-scene model discovery and caching
- public `display_name` / private upstream model-ID mapping
- live context and reasoning capabilities
- live model multiplier display
- text input/output
- Function Calling / Tool Use
- OpenAI Chat Completions / Responses
- Anthropic Messages
- streaming and non-streaming compatibility
- local Web / Desktop / Headless runtimes

Account pools, multi-provider routing, Cloudflare Workers, D1, KV, and public gateway administration are outside the current project scope.
