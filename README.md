# qoder-proxy

[简体中文](README.md) · [English](README_EN.md)

将 Qoder 账号接入本地 OpenAI / Anthropic 兼容 API 的 Go 代理。项目同时提供轻量 Web 管理服务、原生 Gio 桌面客户端和无界面 Headless 服务，三种入口共享同一套凭据、设置、模型注册表与代理实现。

> 当前默认监听地址为 `127.0.0.1:9000`。管理页面只允许本机回环访问；即使你主动把代理监听到局域网地址，`/admin/*` 仍会限制为本机访问。

## 免责声明

> 本项目由 **steamwo** 独立维护，是非官方、非盈利的技术研究与兼容性项目，与 Qoder 及其运营方、关联公司不存在隶属、合作、赞助、授权或认可关系。

- Qoder 及相关名称、商标、服务和产品的权利归各自权利人所有；本项目仅为说明兼容对象而使用相关名称。
- 本项目按“现状”提供，不承诺可用性、稳定性、持续兼容性、安全性或适用于任何特定目的。上游接口、账号策略、服务条款和风控规则可能随时变化。
- 使用者应自行阅读并遵守 Qoder、模型提供方及所在地区适用的服务条款、法律法规和账号政策，并自行判断使用本项目是否被允许。
- 使用本项目产生的账号限制、封禁、额度损失、数据丢失、业务中断、第三方索赔或其他直接/间接损失，由使用者自行承担风险；在适用法律允许的最大范围内，作者不承担由使用或无法使用本项目产生的责任。
- 本项目不鼓励也不应被用于绕过付费、配额、访问控制、风控措施，或用于滥用、攻击、欺诈等行为。
- 若官方或相关权利人认为项目内容存在问题，可通过仓库联系作者沟通处理。

**作者：steamwo**

## 功能概览

### 兼容 API

- OpenAI Chat Completions：`POST /v1/chat/completions`
- OpenAI Responses：`POST /v1/responses`
- Anthropic Messages：`POST /v1/messages`
- OpenAI 风格模型列表：`GET /v1/models`，并兼容 `POST /v1/models`
- 流式与非流式响应
- Function Calling / Tool Use
- OpenAI / Anthropic 对应错误格式
- Qoder 免费账号排队重试、流式心跳和额度错误映射

### 实时模型能力

模型能力不在本地写死，而是读取 Qoder 当前账号的实时模型列表：

- 合并服务端多个 `server scene` 的模型集合，而不是只读取 `chat`
- 保留模型来源的 `server_scene`
- `/v1/models` 对外仍使用 Qoder `display_name` 作为公开模型 ID
- Qoder 内部 `key/model_id` 只用于上游请求，不通过公开模型目录暴露
- 同名模型继续做确定性去重，保持现有公开模型 ID 行为
- 上下文窗口来自实时 `context_config`
- 推理强度来自实时 `thinking_config`
- 价格倍率来自可选 `price_factor`；当活动优惠提供 `promotion.discount_factor` 时，桌面/Web 管理界面显示当前优惠倍率
- 上游没有提供倍率时显示 `—`，合法的 `0x` 不会被误判为缺失值

### 三种运行方式

| 入口 | 适合场景 | 登录/管理 |
| --- | --- | --- |
| `qoder-proxy-web-*` | 轻量本机服务、浏览器管理 | 内置 `/admin/` 页面，可登录、查看额度、模型、日志和设置 |
| `qoder-proxy-desktop-*` | 日常桌面使用 | 原生 Gio UI，无 WebView，支持托盘和完整本地管理 |
| `qoder-proxy-headless-*` | 服务器、最小内存、无图形环境 | 不提供登录 UI；需要先由 Web/桌面入口生成可用凭据 |

三种入口共享：

- Qoder 凭据文件
- `desktop.json` 设置
- 模型推理/上下文默认值
- 本地 API Key
- 队列策略
- 代理后端实现

## 下载

正式版本在 GitHub Releases 提供 6 个二进制：

```text
qoder-proxy-desktop-windows-amd64.exe
qoder-proxy-web-windows-amd64.exe
qoder-proxy-headless-windows-amd64.exe
qoder-proxy-desktop-linux-amd64
qoder-proxy-web-linux-amd64
qoder-proxy-headless-linux-amd64
```

Release 页面：

https://github.com/steamwo/qoder-proxy/releases

## 快速开始

### 方案 A：Web 管理服务

Windows：

```powershell
.\qoder-proxy-web-windows-amd64.exe
```

Linux：

```bash
chmod +x ./qoder-proxy-web-linux-amd64
./qoder-proxy-web-linux-amd64
```

启动后默认打开：

```text
http://127.0.0.1:9000/admin/
```

Web 服务把兼容 API 和管理页面放在同一个监听端口：

```text
http://127.0.0.1:9000/v1/...
http://127.0.0.1:9000/admin/...
```

可选参数：

```text
--no-browser   启动时不自动打开浏览器
--shutdown     请求已运行的本地 Web 服务退出
```

首次使用可在管理页完成 Qoder 登录。默认设置 `auto_start=true`，凭据可用时会自动启用代理。

### 方案 B：原生桌面端

Windows 直接运行：

```text
qoder-proxy-desktop-windows-amd64.exe
```

Linux：

```bash
chmod +x ./qoder-proxy-desktop-linux-amd64
./qoder-proxy-desktop-linux-amd64
```

桌面端提供：

- Qoder 登录/退出
- 账号身份、套餐、额度和重置时间
- 可搜索模型列表
- 模型上下文默认值
- 实时价格倍率
- 推理强度默认值
- 代理启动/停止、监听地址和运行时间
- 本地 API Key
- 免费账号排队策略
- 结构化日志和事件详情
- 数据与隐私路径
- Windows 托盘 / Linux StatusNotifierItem

### 方案 C：Headless

Headless 不实现登录流程，适合已经有本地凭据的机器：

Windows：

```powershell
.\qoder-proxy-headless-windows-amd64.exe
```

Linux：

```bash
chmod +x ./qoder-proxy-headless-linux-amd64
./qoder-proxy-headless-linux-amd64
```

也可以显式写：

```bash
./qoder-proxy-headless-linux-amd64 serve
```

它读取与桌面/Web 入口相同的凭据和设置，并直接启动兼容 API 服务。

## 从源码构建

需要 Go 1.23+。

Headless：

```bash
go build -trimpath -o dist/qoder-proxy-headless ./cmd/qoder-proxy
```

Web 管理服务：

```bash
go build -trimpath -o dist/qoder-proxy-web ./cmd/qoder-proxy-service
```

Windows 原生桌面端：

```powershell
./scripts/build-desktop.ps1
```

Linux 原生桌面端：

```bash
./scripts/build-desktop.sh
```

Linux 桌面构建需要 Gio 使用的 EGL、Vulkan、Wayland、X11 等开发依赖，详见 [docs/desktop.md](docs/desktop.md)。

## API 示例

以下示例假设使用默认地址 `127.0.0.1:9000`，且没有设置本地 API Key。

### 模型列表

```bash
curl http://127.0.0.1:9000/v1/models
```

公开模型 ID 使用 Qoder 的 `display_name`。内部模型 ID 不会出现在公开模型目录中。

### OpenAI Chat Completions

非流式：

```bash
curl http://127.0.0.1:9000/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "Claude Sonnet 4",
    "messages": [{"role":"user","content":"你好"}]
  }'
```

流式：

```bash
curl -N http://127.0.0.1:9000/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "Claude Sonnet 4",
    "stream": true,
    "stream_options": {"include_usage": true},
    "messages": [{"role":"user","content":"你好"}]
  }'
```

### OpenAI Responses

```bash
curl http://127.0.0.1:9000/v1/responses \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "Claude Sonnet 4",
    "input": "你好"
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
    "messages": [{"role":"user","content":"你好"}]
  }'
```

## 推理强度

代理从模型的实时 `thinking_config` 判断支持的选项，不维护静态模型白名单。

常见值包括：

```text
none, low, medium, high, xhigh, max
```

`auto` / `default` 表示不覆盖 Qoder 默认行为；当模型明确支持关闭思考时，`off` 会映射为 Qoder 的 `none`。

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

显式提交模型不支持的推理等级会返回 HTTP 400；模型没有可配置推理深度时，兼容客户端附带的全局推理提示不会被错误透传给 Qoder。

## 上下文窗口

支持的上下文档位来自实时 `context_config`。桌面/Web 管理界面允许为具体上游模型保存默认上下文窗口；请求显式指定参数时仍以请求行为为准。

模型能力变化后，代理会以最新服务端配置进行校验，避免长期依赖本地静态模型表。

## 模型倍率

倍率展示遵循当前服务端模型信息：

1. 基础倍率读取 `price_factor`
2. `price_factor` 缺失时视为未知，而不是 `0`
3. `0` 是合法倍率并显示为 `0x`
4. 当 `promotion.active=true` 且存在 `promotion.discount_factor` 时，界面优先显示优惠倍率
5. 倍率仅用于展示当前 Qoder 模型价格信息，不改变代理本身的请求路由或计费逻辑

模型、倍率、优惠和参数都可能被 Qoder 服务端动态调整，请以当前账号实时返回结果为准。

## 本地 API Key

可在 Web/桌面设置中配置本地 API Key。配置后：

OpenAI 客户端：

```text
Authorization: Bearer <local-api-key>
```

Anthropic 客户端：

```text
x-api-key: <local-api-key>
```

本地 API Key 只保护兼容 API；管理页面仍有独立的本机回环访问限制。

## Qoder 排队和额度错误

### 免费账号排队

当 Qoder 返回业务码 `10605`、`isQueued: true` 和建议的 `retryAfterSeconds` 时，代理会：

- 按服务端建议等待
- 重新签名并重试
- 遵守最大重试次数和总等待上限
- 对流式请求发送心跳，降低客户端空闲超时概率

默认设置：

```json
{
  "queue_retries": 20,
  "queue_max_wait": "10m"
}
```

### 额度不足

Qoder 业务码 `112` 会映射为 HTTP 429，并转换成 OpenAI / Anthropic 对应的额度错误；额度不足不会进入排队重试。

## 本地数据

### 凭据

- Windows：`%AppData%\qoder-proxy\credentials.json`
- Linux：`${XDG_CONFIG_HOME:-~/.config}/qoder-proxy/credentials.json`
- macOS：`~/Library/Application Support/qoder-proxy/credentials.json`

可用环境变量覆盖凭据路径：

```text
QODER_PROXY_CREDENTIALS=/custom/path/credentials.json
```

凭据文件包含访问令牌，请勿分享或提交到版本控制。

### 设置

- Windows：`%AppData%\qoder-proxy\desktop.json`
- Linux：`${XDG_CONFIG_HOME:-~/.config}/qoder-proxy/desktop.json`

默认配置大致为：

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

文件还可能保存本地 API Key、按模型推理默认值和上下文默认值。

### 日志

- Windows：`%LocalAppData%\qoder-proxy\logs\desktop.log`
- Linux：`${XDG_CACHE_HOME:-~/.cache}/qoder-proxy/logs/desktop.log`

持久日志自动限制为 4 MB；“清空日志”会同时清除内存和磁盘内容。日志用于诊断路径、状态码、模型、耗时等元数据，不记录凭据、Authorization 头或完整请求正文。

## 安全边界

- 默认只监听 `127.0.0.1:9000`
- 如需暴露到 LAN，请同时配置本地 API Key，并自行评估网络访问控制
- Web 管理面的 `/admin/*` 即使代理监听非回环地址，也只允许本机回环客户端访问
- 凭据和设置文件按操作系统能力使用用户私有权限
- 不建议把凭据文件、设置文件或日志上传给第三方

## CI 与发布

`.github/workflows/build.yml` 会在以下情况下运行：

- 推送到 `main`
- 创建或更新目标为 `main` 的 Pull Request
- 手动 `workflow_dispatch`
- 推送 `v*` 标签

质量检查：

```text
go test ./...
go vet ./...
```

PR 会构建 Windows/Linux 的 Web 与 Headless 二进制用于验证；正式 Release 还会构建原生 Desktop 二进制。

正式发布支持两种触发方式：

- 推送符合语义版本格式的 `v*` 标签
- 在 `main` 上推送首行提交信息为 `release: vX.Y.Z` 的提交

工作流会创建草稿 Release、上传 Windows/Linux 的 Desktop/Web/Headless 产物，全部构建成功后再发布并标记为 latest。

## 项目结构

```text
cmd/qoder-proxy            Headless 入口
cmd/qoder-proxy-service    Web 管理服务入口
cmd/qoder-proxy-desktop    Gio 原生桌面入口
internal/credential        Qoder 凭据持久化
internal/qoder             登录、刷新、签名、模型、额度与流式协议
internal/protocol          协议中立请求/事件结构
internal/openai            Chat Completions / Responses 适配
internal/anthropic         Anthropic Messages 适配
internal/server            兼容 API 路由与本地鉴权
internal/desktop           桌面/Web 服务、设置、日志与托盘
assets                     品牌图标
scripts                    桌面构建脚本
```

更多文档：

- [桌面端构建和本地数据](docs/desktop.md)
- [项目架构](docs/architecture.md)
- [设计说明](docs/design.md)

## 当前范围

当前版本聚焦本地单账号兼容代理：

- 单个 Qoder 凭据
- Qoder PKCE 登录与令牌刷新
- COSY 请求签名
- 多 server scene 模型发现与缓存
- `display_name` 公共模型 ID / 内部上游 ID 映射
- 实时上下文与推理能力
- 实时模型倍率展示
- 文本输入输出
- Function Calling / Tool Use
- OpenAI Chat Completions / Responses
- Anthropic Messages
- 流式与非流式兼容
- 本地 Web / Desktop / Headless 三种运行入口

多账号池、多供应商路由、Cloudflare Workers、D1、KV 和公网网关管理不属于当前项目目标。
