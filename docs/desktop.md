# Desktop application

`qoder-proxy-desktop` is a Gio-based desktop shell around the same Go packages used by the CLI. It does **not** embed a WebView and it does not spawn the CLI as a child process.

## Features

- Qoder PKCE login/logout from the desktop window
- account identity and quota display
- dedicated Dashboard, account quota, model, structured log, and settings views
- quota refresh via `GET https://openapi.qoder.sh/api/v2/quota/usage`
- in-process start/stop of the OpenAI/Anthropic compatible HTTP proxy
- model list, including advertised reasoning-effort levels
- searchable structured logs with selectable raw-event details, bounded disk persistence, and credential/request-body redaction inherited from the proxy logger
- persisted listen/API-key/queue settings
- Windows native notification-area icon implemented with `Shell_NotifyIconW`, including open/start-stop/refresh/quit actions
- Linux StatusNotifierItem implemented over the session D-Bus; activate opens the main window and secondary activate toggles the proxy

The desktop app uses the same credential file as the CLI unless `QODER_PROXY_CREDENTIALS` overrides it. The Settings > Data & Privacy view shows every resolved absolute path and provides shortcuts to the containing folders.

The Windows build embeds the same multi-resolution Qoder Proxy icon for the executable, window, taskbar, Alt+Tab, and notification area. `scripts/generate-windows-icon.ps1` regenerates the architecture-specific resource from `assets/qoder-proxy.ico`; the normal Windows build runs it automatically.

## Windows build

Requirements:

- Go 1.23+
- a working Go module download path on the build machine

```powershell
./scripts/build-desktop.ps1
```

Equivalent command:

```powershell
go mod download
go build -tags desktop -trimpath -ldflags "-H=windowsgui -s -w" -o dist/qoder-proxy-desktop-windows-amd64.exe ./cmd/qoder-proxy-desktop
```

For arm64 set `$env:GOARCH = "arm64"` before running the build command.

## Continuous integration

`.github/workflows/build.yml` runs tests and static analysis, then builds Windows and Linux desktop artifacts for pushes and pull requests targeting `main`. Both executables are downloadable from the workflow run. Pushing a tag such as `v0.3.3-desktop-ui` additionally creates a GitHub Release and attaches both platform builds.

## Linux build

Gio needs the normal Linux windowing/OpenGL development libraries. On Debian/Ubuntu, install the Gio-documented X11/Wayland/EGL development packages, then run:

```bash
./scripts/build-desktop.sh
```

The Linux tray implementation uses `org.kde.StatusNotifierItem` on the user's session D-Bus. Desktop environments without a StatusNotifier watcher can still use the main Gio window normally.

## Desktop settings

Settings are stored in the OS user config directory under:

```text
qoder-proxy/desktop.json
```

Defaults:

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

The local API key is optional. If set, both OpenAI `Authorization: Bearer ...` and Anthropic `x-api-key: ...` clients can authenticate to the local proxy.

## Local data storage

The application separates durable user configuration from disposable runtime diagnostics:

| Data | Windows | Linux | Retention |
| --- | --- | --- | --- |
| Account credential | `%AppData%\\qoder-proxy\\credentials.json` | `${XDG_CONFIG_HOME:-~/.config}/qoder-proxy/credentials.json` | Removed on logout; contains sensitive access tokens |
| Desktop settings | `%AppData%\\qoder-proxy\\desktop.json` | `${XDG_CONFIG_HOME:-~/.config}/qoder-proxy/desktop.json` | Persists until manually deleted; may contain a local API key |
| Runtime log | `%LocalAppData%\\qoder-proxy\\logs\\desktop.log` | `${XDG_CACHE_HOME:-~/.cache}/qoder-proxy/logs/desktop.log` | Automatically bounded to 4 MB; Clear Logs removes memory and disk content |

All files and their parent directories are created with restrictive user-only permissions where the operating system supports POSIX modes. Logs contain diagnostic metadata such as paths, status codes, models, and durations; request bodies and credentials are not logged. `QODER_PROXY_CREDENTIALS` may replace the credential path, and the resolved value is shown in the desktop UI.
