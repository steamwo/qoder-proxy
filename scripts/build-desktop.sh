#!/usr/bin/env sh
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT"
mkdir -p dist
go mod download
go test ./internal/qoder ./internal/protocol ./internal/openai ./internal/anthropic ./internal/server ./internal/credential ./internal/desktop
go build -tags desktop -trimpath -o dist/qoder-proxy-desktop-linux-"$(go env GOARCH)" ./cmd/qoder-proxy-desktop
echo "Built dist/qoder-proxy-desktop-linux-$(go env GOARCH)"
