#!/usr/bin/env bash
# 用法：checksums.sh DIR；仅收集本项目发布命名的文件。
source "$(dirname -- "${BASH_SOURCE[0]}")/common.sh"
[[ $# == 1 && -d $1 ]] || fail '参数必须是发行目录'
python3 -I - "$1" <<'PY'
import hashlib, pathlib, sys
root = pathlib.Path(sys.argv[1])
files = sorted(p for p in root.iterdir() if p.name.startswith("qqmusic-") and p.is_file())
if not files:
    raise SystemExit("没有发行文件")
for p in files:
    if p.is_symlink() or p.name.startswith("qqmusic-web") or "\n" in p.name or "\\" in p.name:
        raise SystemExit(f"非法发行文件：{p.name}")
text = "".join(f"{hashlib.sha256(p.read_bytes()).hexdigest()}  {p.name}\n" for p in files)
(root / "SHA256SUMS.tmp").write_text(text)
(root / "SHA256SUMS.tmp").replace(root / "SHA256SUMS")
PY
