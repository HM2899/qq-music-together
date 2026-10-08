#!/usr/bin/env bash
# 用法：bundle.sh --version 0.1.0 --ref HEAD --output DIR
# 网页与源码都取自同一个 Git 对象，不把工作区私有/未提交文件混入。
source "$(dirname -- "${BASH_SOURCE[0]}")/common.sh"
version= ref= output=
while (($#)); do
  [[ $# -ge 2 ]] || fail "缺少参数值：$1"
  case $1 in --version) version=$2;; --ref) ref=$2;; --output) output=$2;; *) fail "未知参数：$1";; esac
  shift 2
done
version_check "$version"
[[ -n $ref && -n $output ]] || fail '必须指定 --ref 和 --output'
commit=$(git -C "$ROOT" rev-parse --verify "$ref^{commit}")
for file in LICENSE THIRD_PARTY_NOTICES.md tui/go.mod tui/go.sum tui/backend/qqmusic_api.py; do
  git -C "$ROOT" cat-file -e "$commit:$file" || fail "Git 提交缺少 $file"
done
mkdir -p -- "$output"
output=$(cd -- "$output" && pwd)
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
source_name=qqmusic-together_${version}_source.tar.gz
web_name=qqmusic-web_${version}.tar.gz
[[ ! -e $output/$source_name && ! -e $output/$web_name ]] || fail '拒绝覆盖已有源码或网页包'
git -C "$ROOT" archive --format=tar --prefix="qqmusic-together-$version/" "$commit" | gzip -n > "$work/$source_name"
git -C "$ROOT" archive --format=tar --prefix="qqmusic-web-$version/" "$commit" -- index.html app.js style.css assets LICENSE THIRD_PARTY_NOTICES.md licenses | gzip -n > "$work/$web_name"
mv -- "$work/$source_name" "$work/$web_name" "$output/"
printf '已从 %s 生成源码与网页包\n' "$commit"
