# qoder-proxy

[简体中文](README.md) · [English](README_EN.md)

将 Qoder 账号转换为本地 OpenAI / Anthropic 兼容 API 的 Go 代理，提供原生命令行与桌面客户端。

## 免责声明

> 本项目是由 **steamwo** 独立维护的非官方、非盈利技术研究与兼容性项目，与 Qoder 及其运营方、关联公司不存在隶属、合作、赞助、授权或认可关系。

- Qoder 及相关名称、商标、服务和产品的权利归其各自权利人所有；本项目对相关名称的使用仅用于说明兼容对象。
- 本项目按“现状”提供，不承诺可用性、稳定性、持续兼容性、安全性或适用于任何特定目的。上游接口、账号策略、服务条款和风控规则可能随时变化。
- 使用者应自行阅读并遵守 Qoder、模型提供方及所在地区适用的服务条款、法律法规和账号政策，并自行判断使用本项目是否被允许。
- 使用本项目产生的账号限制、封禁、额度损失、数据丢失、业务中断、第三方索赔或其他直接/间接损失，由使用者自行承担风险；在适用法律允许的最大范围内，作者不承担由使用或无法使用本项目产生的责任。
- 本项目不鼓励也不应被用于绕过付费、配额、访问控制、风控措施或从事滥用、攻击、欺诈等行为。
- 若官方或相关权利人认为本项目的某些内容存在问题，可通过项目仓库联系作者沟通处理。

**作者：steamwo**

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

## 桌面客户端

桌面客户端提供：

- Qoder 登录与退出登录
- 账号身份、套餐和实时额度
- 可搜索的模型能力列表
- 代理启动、停止、运行时间与监听地址
- 可搜索的结构化请求日志和事件详情
- 代理、队列、本地鉴权和托盘设置
- “数据与隐私”页面，展示账号、设置和日志的实际存储位置

Windows 托盘支持：

- 单击或双击打开主窗口
- 右键启动/停止代理、刷新额度、退出程序
- 关闭主窗口后继续驻留并保持代理运行

### 下载自动构建

每次推送到 `main`，GitHub Actions 都会生成：

- `qoder-proxy-windows-amd64`
- `qoder-proxy-linux-amd64`

可在 [Actions](https://github.com/steamwo/qoder-proxy/actions) 页面下载。推送 `v*` 标签时会自动创建 GitHub Release，并附带两个平台的构建产物。

## 快速开始：命令行

需要 Go 1.23 或更高版本。

```bash
go build -o qoder-proxy ./cmd/qoder-proxy
```

### 登录

```bash
./qoder-proxy login
```

无图形界面环境：

```bash
./qoder-proxy login --no-browser
```

### 启动代理

```bash
./qoder-proxy serve
```

默认监听：

```text
127.0.0.1:8080
```

指定监听地址：

```bash
./qoder-proxy serve --listen 127.0.0.1:9000
```

默认只监听回环地址，避免无意中向局域网暴露服务。

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

Linux 需要 Gio 所使用的 EGL、Vulkan、Wayland 和 X11 开发依赖。详细说明见 [桌面客户端文档](docs/desktop.md)。

## API 使用示例

### 模型列表

```bash
curl http://127.0.0.1:8080/v1/models
```

`/v1/models` 返回 Qoder 的 `display_name` 作为公开模型 ID。内部 `key` / `model_id` 只用于请求 Qoder，不会通过模型列表暴露。

### Chat Completions

非流式：

```bash
curl http://127.0.0.1:8080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "Claude Sonnet 4",
    "messages": [{"role":"user","content":"你好"}]
  }'
```

流式：

```bash
curl -N http://127.0.0.1:8080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "Claude Sonnet 4",
    "stream": true,
    "stream_options": {"include_usage": true},
    "messages": [{"role":"user","content":"你好"}]
  }'
```

### Responses

```bash
curl http://127.0.0.1:8080/v1/responses \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "Claude Sonnet 4",
    "input": "你好"
  }'
```

### Anthropic Messages

```bash
curl http://127.0.0.1:8080/v1/messages \
  -H 'Content-Type: application/json' \
  -H 'anthropic-version: 2023-06-01' \
  -d '{
    "model": "Claude Sonnet 4",
    "max_tokens": 1024,
    "messages": [{"role":"user","content":"你好"}]
  }'
```

## 推理强度

项目不会硬编码哪些模型支持推理强度，而是读取 Qoder 模型列表中的实时 `thinking_config`。

常见值包括：

```text
low, medium, high, xhigh, max
```

`auto` / `default` 表示不覆盖默认行为；当模型提供禁用思考模式时，`off` 会映射为 Qoder 的 `none`。

OpenAI Chat Completions：

```json
{
  "model": "DeepSeek-V4-Flash",
  "reasoning_effort": "high",
  "messages": [{"role":"user","content":"解决这个问题"}]
}
```

OpenAI Responses：

```json
{
  "model": "DeepSeek-V4-Flash",
  "reasoning": {"effort":"high"},
  "input": "解决这个问题"
}
```

Anthropic Messages：

```json
{
  "model": "DeepSeek-V4-Flash",
  "max_tokens": 4096,
  "output_config": {"effort":"high"},
  "messages": [{"role":"user","content":"解决这个问题"}]
}
```

不支持的推理级别会返回 HTTP 400，不会静默降级。

## 本地 API Key

通过环境变量启用本地鉴权：

```bash
QODER_PROXY_API_KEY=local-secret ./qoder-proxy serve
```

OpenAI 客户端：

```text
Authorization: Bearer local-secret
```

Anthropic 客户端：

```text
x-api-key: local-secret
```

## 数据存储位置

### 账号凭据

- Windows：`%AppData%\qoder-proxy\credentials.json`
- Linux：`${XDG_CONFIG_HOME:-~/.config}/qoder-proxy/credentials.json`
- macOS：`~/Library/Application Support/qoder-proxy/credentials.json`

通过 `QODER_PROXY_CREDENTIALS` 可以覆盖默认路径。账号文件包含访问令牌，请勿共享。

### 桌面配置

- Windows：`%AppData%\qoder-proxy\desktop.json`
- Linux：`${XDG_CONFIG_HOME:-~/.config}/qoder-proxy/desktop.json`

### 桌面日志

- Windows：`%LocalAppData%\qoder-proxy\logs\desktop.log`
- Linux：`${XDG_CACHE_HOME:-~/.cache}/qoder-proxy/logs/desktop.log`

桌面日志自动限制为 4 MB。“清空日志”会同时清除内存和磁盘内容。日志不会记录凭据、Authorization 头或完整请求正文。

## Qoder 排队和额度错误

### 免费账号排队

Qoder 可能返回业务码 `10605`、`isQueued: true` 和建议的 `retryAfterSeconds`。代理会：

- 等待服务端建议的时间
- 重新签名并重试请求
- 对流式 Chat Completions 发送 SSE 注释心跳，避免客户端空闲超时

默认配置：

```text
--queue-retries 20
--queue-max-wait 10m
```

### 额度不足

Qoder 业务码 `112` 会映射为 HTTP 429，并返回 OpenAI / Anthropic 对应格式的额度错误。额度不足不会进入排队重试。

## 日志

命令行日志默认输出到 stderr，支持以下级别：

```text
debug, info, warn, error, off
```

启用调试日志：

```bash
./qoder-proxy serve --log-level debug
```

或：

```bash
QODER_PROXY_LOG_LEVEL=debug ./qoder-proxy serve
```

## 环境变量

| 变量 | 作用 |
| --- | --- |
| `QODER_PROXY_LISTEN` | HTTP 监听地址，默认 `127.0.0.1:8080` |
| `QODER_PROXY_API_KEY` | 可选的本地 Bearer API Key |
| `QODER_PROXY_CREDENTIALS` | 覆盖账号凭据 JSON 路径 |
| `QODER_PROXY_LOG_LEVEL` | 日志级别：`debug`、`info`、`warn`、`error`、`off` |
| `QODER_PROXY_INFER_ENDPOINT` | 覆盖 Qoder 推理端点；默认使用已验证协议基线的 `https://api2.qoder.sh` |
| `QODER_PROXY_QUEUE_RETRIES` | 免费账号排队重试次数 |
| `QODER_PROXY_QUEUE_MAX_WAIT` | 排队总等待上限 |
| `HTTP_PROXY` / `HTTPS_PROXY` / `NO_PROXY` | Go 标准 HTTP 代理变量 |

## 其他命令

```bash
./qoder-proxy models
./qoder-proxy status
./qoder-proxy logout
```

## 自动构建与发布

[GitHub Actions 工作流](.github/workflows/build.yml)会在以下情况运行：

- 推送到 `main`
- 创建或更新面向 `main` 的 Pull Request
- 手动触发 `workflow_dispatch`
- 推送 `v*` 标签

工作流执行：

1. `go test ./...`
2. `go vet ./...`
3. 构建无控制台窗口、带品牌图标的 Windows GUI 程序
4. 安装 Gio 原生依赖并构建 Linux 程序
5. 上传两个平台的 Artifact
6. 对 `v*` 标签自动创建 GitHub Release

## 项目结构

```text
cmd/qoder-proxy            命令行程序
cmd/qoder-proxy-desktop    Gio 桌面客户端
internal/qoder             Qoder 鉴权、签名、模型、额度与流式协议
internal/openai            OpenAI Chat Completions / Responses 适配
internal/anthropic         Anthropic Messages 适配
internal/server            HTTP 服务
internal/desktop           桌面端代理、设置、日志与托盘
assets                     桌面品牌图标
```

更多资料：

- [桌面端构建和数据路径](docs/desktop.md)
- [项目架构](docs/architecture.md)
- [设计说明](docs/design.md)

## 当前范围

当前版本聚焦本地单账号使用：

- 单个 Qoder 凭据
- Qoder PKCE 设备登录
- COSY 请求签名
- 模型发现与缓存
- 文本输入输出
- Function Calling / Tool Use
- OpenAI 与 Anthropic 流式/非流式兼容

多账号池、Cloudflare Workers、D1、KV、网关额度和多供应商路由暂不属于当前范围。
