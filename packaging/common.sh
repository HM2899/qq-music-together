#!/usr/bin/env bash
# 本文件只供同目录脚本 source。
set -euo pipefail
umask 022
PACKAGING_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
ROOT=$(cd -- "$PACKAGING_DIR/.." && pwd)
NFPM_VERSION=2.47.0
fail() { printf '错误：%s\n' "$*" >&2; exit 1; }
need() { command -v "$1" >/dev/null || fail "缺少工具 $1"; }
version_check() { [[ $1 =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] || fail '版本须为 MAJOR.MINOR.PATCH（无 v 前缀）'; }
go_check() { need go; [[ $(go env GOVERSION) == go1.27.* ]] || fail '构建需要 Go 1.27.x'; }
