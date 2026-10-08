#!/usr/bin/env bash
# 用法：verify.sh DIR；需要 python3、bsdtar、readelf、go。
# 有 SHA256SUMS 时严格比对完整清单；没有时仍验证全部包体。
source "$(dirname -- "${BASH_SOURCE[0]}")/common.sh"
[[ $# == 1 && -d $1 ]] || fail '参数必须是发行目录'
for tool in python3 bsdtar readelf go; do need "$tool"; done
python3 -I "$PACKAGING_DIR/verify.py" "$1"
