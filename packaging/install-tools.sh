#!/usr/bin/env bash
# 用法：packaging/install-tools.sh 不存在的目录或已创建的空目录
# 不改宿主 PATH、不安装宿主软件包；继承用户 Go 镜像/校验配置。
source "$(dirname -- "${BASH_SOURCE[0]}")/common.sh"
[[ $# == 1 && -n $1 ]] || fail '必须指定工具目录'
[[ ! -L $1 ]] || fail '工具目录不能是符号链接'
mkdir -p -- "$1"
[[ -d $1 && -z $(find "$1" -mindepth 1 -maxdepth 1 -print -quit) ]] || fail '工具目录必须为空'
go_check
[[ $(go env GOSUMDB) != off ]] || fail '安装构建工具必须启用 Go 模块校验'
tools=$(cd -- "$1" && pwd)
GOBIN="$tools" GOTOOLCHAIN=local go install "github.com/goreleaser/nfpm/v2/cmd/nfpm@v$NFPM_VERSION"
printf 'NFPM=%s/nfpm\n' "$tools"
