$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root
New-Item -ItemType Directory -Force dist | Out-Null
if (-not $env:GOARCH) { $env:GOARCH = "amd64" }
$env:GOOS = "windows"
go mod download
& "$PSScriptRoot/generate-windows-icon.ps1"
go test ./internal/qoder ./internal/protocol ./internal/openai ./internal/anthropic ./internal/server ./internal/credential ./internal/desktop
# Build as a GUI subsystem binary so launching the product never creates a companion console window.
# 使用 GUI 子系统构建，确保启动产品时不会同时弹出命令行窗口。
go build -tags desktop -trimpath -ldflags "-H=windowsgui -s -w" -o "dist/qoder-proxy-desktop-windows-$($env:GOARCH).exe" ./cmd/qoder-proxy-desktop
Write-Host "Built dist/qoder-proxy-desktop-windows-$($env:GOARCH).exe"
