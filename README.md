# qoder-proxy

[简体中文](README.md) · [English](README_EN.md)

将 Qoder 账号转换为本地 OpenAI / Anthropic 兼容 API 的 Go 代理，提供原生命令行、轻量后台服务与 Gio 桌面客户端。

## 运行方式

- `qoder-proxy-desktop-*`：现有一体化 Gio GUI，完整保留。
- `qoder-proxy-headless-*`：纯前台代理，适合服务器和内存诊断。
- `qoder-proxy-service-*`：不链接 Gio 的轻量后台服务；代理 API 与本地管理页共用一个端口。

轻量 service 默认读取 `desktop.json` 中的监听地址（默认 `127.0.0.1:9000`）：

- OpenAI / Anthropic API：`http://127.0.0.1:9000/v1/...`
- 本地管理页：`http://127.0.0.1:9000/admin/`

管理页仅允许本机访问。即使代理监听配置为 `0.0.0.0`，远程客户端访问 `/admin` 也会被拒绝。

管理页支持：Qoder 网页授权、代理启动/停止/配置、模型默认思考深度、运行时内存和日志。关闭浏览器页面不会停止后台服务；点击“显式退出后台服务”会结束进程。也可从命令行执行：

```powershell
qoder-proxy-service-windows-amd64.exe --shutdown
```

监听地址本身修改后，需要退出并重新启动 service 才会绑定到新地址；API Key、队列策略和模型默认设置可在当前进程中重载。

## 主要功能

- OpenAI Chat Completions：`POST /v1/chat/completions`
- OpenAI Responses：`POST /v1/responses`
- Anthropic Messages：`POST /v1/messages`
- 模型列表：`GET /v1/models`，兼容 `POST /v1/models`
- 流式与非流式响应
- Function Calling / Tool Use
- 按模型实时能力校验 `reasoning_effort`
- Qoder 免费账号排队重试与流式心跳
- 可选本地 API Key
- Windows / Linux 原生 Gio 桌面客户端，不使用 WebView
- Windows 系统托盘、关闭后驻留、任务栏与托盘品牌图标
- 本地结构化日志、自动限额与数据路径透明展示

## 快速开始：命令行

需要 Go 1.23 或更高版本。

```bash
go build -o qoder-proxy ./cmd/qoder-proxy
```

已使用桌面端或管理页完成 Qoder 授权后：

```bash
./qoder-proxy serve
```

## 快速开始：桌面端

Windows：

```powershell
./scripts/build-desktop.ps1
```

产物：

```text
dist/qoder-proxy-desktop-windows-amd64.exe
```

Linux：

```bash
./scripts/build-desktop.sh
```

产物：

```text
dist/qoder-proxy-desktop-linux-amd64
```

Linux 需要 Gio 所使用的 EGL、Vulkan、Wayland 和 X11 开发依赖。详细说明见 `docs/desktop.md`。

## API 使用示例

```bash
curl http://127.0.0.1:9000/v1/models
```

`/v1/models` 返回 Qoder 的 `display_name` 作为公开模型 ID。内部 `key` / `model_id` 只用于请求 Qoder，不会通过模型列表暴露。

### Chat Completions

```bash
curl -N http://127.0.0.1:9000/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "Claude Sonnet 4",
    "stream": true,
    "messages": [{"role":"user","content":"你好"}]
  }'
```

### Responses

```bash
curl http://127.0.0.1:9000/v1/responses \
  -H 'Content-Type: application/json' \
  -d '{"model":"Claude Sonnet 4","input":"你好"}'
```

### Anthropic Messages

```bash
curl http://127.0.0.1:9000/v1/messages \
  -H 'Content-Type: application/json' \
  -H 'anthropic-version: 2023-06-01' \
  -d '{
    "model": "Claude Sonnet 4",
    "max_tokens": 1024,
    "messages": [{"role":"user","content":"你好"}]
  }'
```

## 推理强度

项目不会硬编码哪些模型支持推理强度，而是读取 Qoder 模型列表中的实时 `thinking_config`。网页管理页和 Gio 桌面端都使用这一真实能力集合生成每个模型的可选项。

`auto` / `default` 表示不覆盖默认行为；当模型提供禁用思考模式时，`off` 会映射为 Qoder 的 `none`。不支持的推理级别会返回 HTTP 400，不会静默降级。
