#!/usr/bin/env bash
# 仅在临时容器中安装软件包，不触碰宿主账号或播放器。
set -euo pipefail
out=$(realpath "${1:?用法: smoke-containers.sh 输出目录 amd64|arm64}")
arch=${2:?缺少架构}
case "$arch" in
  amd64) platform=linux/amd64 ;;
  arm64) platform=linux/arm64 ;;
  *) printf '不支持的架构: %s\n' "$arch" >&2; exit 1 ;;
esac
command -v docker >/dev/null
common='set -eu
export HOME=/tmp/qqmusic-test XDG_CACHE_HOME=/tmp/qqmusic-test/cache XDG_DATA_HOME=/tmp/qqmusic-test/data XDG_CONFIG_HOME=/tmp/qqmusic-test/config
mkdir -p "$HOME"
qqmusic-tui --version
qqmusic-tui --help
python3 -I /usr/share/qqmusic-tui/backend/qqmusic_api.py --help >/dev/null
test -z "$(find "$HOME" -type f -print -quit)"
command -v mpv
mpv --no-config --ao=null --vo=null --idle=no
'
run() {
  local image=$1 install=$2 remove=$3
  docker run --rm --platform "$platform" -v "$out:/packages:ro" "$image" \
    sh -c "$install
$common
$remove"
}
run debian:bookworm-slim \
  'set -eu; apt-get update; apt-get install -y /packages/*_linux_'"$arch"'.deb' \
  'apt-get remove -y qqmusic-tui; test ! -e /usr/bin/qqmusic-tui'
run fedora:43 \
  'set -eu; dnf install -y /packages/*_linux_'"$arch"'.rpm' \
  'dnf remove -y qqmusic-tui; test ! -e /usr/bin/qqmusic-tui'
# Arch 官方镜像只有 x86_64；aarch64 包只做结构检查，不假称完成 Arch ARM 安装测试。
if [[ "$arch" == amd64 ]]; then
  run archlinux:base \
    'set -eu; pacman -Syu --noconfirm; pacman -U --noconfirm /packages/*_linux_amd64.pkg.tar.zst' \
    'pacman -R --noconfirm qqmusic-tui; test ! -e /usr/bin/qqmusic-tui'
fi
run alpine:3.22 \
  'set -eu; apk add --no-cache --allow-untrusted /packages/*_linux_'"$arch"'.apk' \
  'apk del qqmusic-tui; test ! -e /usr/bin/qqmusic-tui'
