#!/usr/bin/env bash
# 用法：build.sh --arch amd64|arm64 --version 0.1.0 --commit HASH --output DIR
source "$(dirname -- "${BASH_SOURCE[0]}")/common.sh"
arch= version= commit= output=
while (($#)); do
  [[ $# -ge 2 ]] || fail "缺少参数值：$1"
  case $1 in
    --arch) arch=$2;; --version) version=$2;; --commit) commit=$2;; --output) output=$2;;
    *) fail "未知参数：$1";;
  esac
  shift 2
done
[[ $arch == amd64 || $arch == arm64 ]] || fail '架构须为 amd64 或 arm64'
version_check "$version"
[[ $commit =~ ^[0-9a-f]{7,40}$ ]] || fail 'commit 须为 7–40 位小写 Git 提交哈希'
[[ -n $output ]] || fail '必须指定 --output'
for tool in python3 tar gzip readelf; do need "$tool"; done
go_check
nfpm=${NFPM:-nfpm}
need "$nfpm"
nfpm=$(command -v "$nfpm")
# go install 的 --version 显示 dev，使用 Go 的模块构建信息核验真实版本。
module_info=$(go version -m "$nfpm" 2>/dev/null || true)
if ! grep -Eq "mod[[:space:]]+github.com/goreleaser/nfpm/v2[[:space:]]+v${NFPM_VERSION//./\.}([[:space:]]|$)" <<<"$module_info"; then
  "$nfpm" --version 2>&1 | grep -Eq "GitVersion:[[:space:]]+v?${NFPM_VERSION//./\.}([[:space:]]|$)" || fail "需要 nFPM $NFPM_VERSION"
fi
for file in LICENSE THIRD_PARTY_NOTICES.md tui/backend/qqmusic_api.py; do
  [[ -f $ROOT/$file && ! -L $ROOT/$file ]] || fail "缺少发行文件：$file"
done
[[ -d $ROOT/licenses ]] || fail '缺少 licenses 目录'
[[ -z $(find "$ROOT/licenses" -type l -print -quit) ]] || fail 'licenses 不能含符号链接'
mkdir -p -- "$output"
output=$(cd -- "$output" && pwd)
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
stage=$work/stage
license_dir=$stage/share/licenses/qqmusic-tui
mkdir -p "$stage/bin" "$stage/share/qqmusic-tui/backend" "$license_dir"
export GOTOOLCHAIN=local
(cd "$ROOT/tui" && go mod download all)
CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go -C "$ROOT/tui" build -mod=readonly -trimpath -buildvcs=false \
  -ldflags "-s -w -X main.version=$version -X main.commit=$commit" -o "$stage/bin/qqmusic-tui" .
if readelf -l "$stage/bin/qqmusic-tui" | grep -q INTERP; then fail '二进制不是静态链接'; fi
install -m 0644 "$ROOT/tui/backend/qqmusic_api.py" "$stage/share/qqmusic-tui/backend/qqmusic_api.py"
install -m 0644 "$ROOT/LICENSE" "$ROOT/THIRD_PARTY_NOTICES.md" "$license_dir/"
cp -R -- "$ROOT/licenses" "$license_dir/third-party"
CGO_ENABLED=0 GOOS=linux GOARCH="$arch" python3 -I "$PACKAGING_DIR/licenses.py" "$ROOT" "$license_dir"
python3 -I - "$stage" "$version" "$commit" "$arch" <<'PY'
import hashlib, json, pathlib, sys
stage = pathlib.Path(sys.argv[1])
manifest = {"version": sys.argv[2], "commit": sys.argv[3], "arch": sys.argv[4], "files": {}}
for p in sorted(stage.rglob("*")):
    if p.is_file():
        p.chmod(0o755 if p.parent.name == "bin" else 0o644)
        manifest["files"][str(p.relative_to(stage))] = hashlib.sha256(p.read_bytes()).hexdigest()
(stage / "share/qqmusic-tui/manifest.json").write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + "\n")
PY
# SOURCE_DATE_EPOCH 可由 CI 提交时间提供；默认固定值避免机器时钟进入包体。
export SOURCE_DATE_EPOCH=${SOURCE_DATE_EPOCH:-0}
[[ $SOURCE_DATE_EPOCH =~ ^[0-9]+$ ]] || fail 'SOURCE_DATE_EPOCH 须为非负整数'
find "$stage" -exec touch -h -d "@$SOURCE_DATE_EPOCH" {} +
export QQMUSIC_STAGE=$stage QQMUSIC_ARCH=$arch QQMUSIC_VERSION=$version
base=qqmusic-tui_${version}_linux_${arch}
for format in deb rpm archlinux apk; do
  case $format in archlinux) suffix=pkg.tar.zst;; *) suffix=$format;; esac
  "$nfpm" package --config "$PACKAGING_DIR/nfpm.yaml" --packager "$format" --target "$work/$base.$suffix"
done
tar --sort=name --mtime="@$SOURCE_DATE_EPOCH" --owner=0 --group=0 --numeric-owner -C "$stage" -cf - bin share | gzip -n > "$work/$base.tar.gz"
# 仅在全部格式生成成功后交付，不把 stage 留在发行目录。
for suffix in deb rpm pkg.tar.zst apk tar.gz; do
  [[ ! -e $output/$base.$suffix ]] || fail "拒绝覆盖已有产物：$base.$suffix"
done
for suffix in deb rpm pkg.tar.zst apk tar.gz; do mv -- "$work/$base.$suffix" "$output/"; done
printf '已生成 %s 的五种发行包：%s\n' "$arch" "$output"
