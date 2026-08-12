$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

if (-not $env:GOARCH) { $env:GOARCH = "amd64" }
$icon = "assets/qoder-proxy.ico"
$output = "cmd/qoder-proxy-desktop/rsrc_windows_$($env:GOARCH).syso"

# Embed resource ID 1 because Gio and the tray loader both use it as the product icon.
# 嵌入资源 ID 1，因为 Gio 与托盘加载器都会将其作为产品图标。
go run github.com/akavel/rsrc@v0.10.2 -arch $env:GOARCH -ico $icon -o $output
if ($LASTEXITCODE -ne 0) { throw "Failed to generate Windows icon resource" }
Write-Host "Generated $output"
