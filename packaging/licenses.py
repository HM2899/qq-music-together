#!/usr/bin/env python3
"""收集 Go 构建依赖的原始许可证，不更改上游许可。由 python3 -I 调用。"""
import json
import pathlib
import shutil
import subprocess
import sys

root, dest = map(pathlib.Path, sys.argv[1:])
modules = subprocess.check_output(["go", "list", "-deps", "-json", "."], cwd=root / "tui", text=True)
decoder = json.JSONDecoder()
items = []
while modules.strip():
    item, end = decoder.raw_decode(modules.lstrip())
    if item.get("Module"):
        items.append(item["Module"])
    modules = modules.lstrip()[end:]
manifest = []
unique = {item["Path"]: item for item in items}
for module in sorted(unique.values(), key=lambda item: item["Path"]):
    if module.get("Main"):
        continue
    if "Replace" in module:
        raise SystemExit("发行构建不允许未审查的 Go replace 模块")
    path, version = module["Path"], module["Version"]
    source = pathlib.Path(module["Dir"])
    target = dest / "go-modules" / (path + "@" + version)
    notices = []
    # 一些 Go 模块在子目录包含额外的许可证（SQLite、x/sys 等）。
    for entry in sorted(source.rglob("*")):
        if not entry.is_file():
            continue
        name = entry.name.upper()
        if not any(name == stem or name.startswith(stem + ".") or name.startswith(stem + "-") for stem in ("LICENSE", "LICENCE", "COPYING", "NOTICE", "COPYRIGHT", "AUTHORS", "PATENTS", "UNLICENSE")):
            continue
        if entry.is_symlink():
            raise SystemExit(f"许可证不应为符号链接：{entry}")
        relative = entry.relative_to(source)
        output = target / relative
        output.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(entry, output)
        notices.append(str(relative))
    if not notices:
        raise SystemExit(f"找不到模块许可证：{path}@{version}，请人工审查")
    manifest.append({"module": path, "version": version, "licenses": notices})
goroot = pathlib.Path(subprocess.check_output(["go", "env", "GOROOT"], text=True).strip())
go_license = goroot / "LICENSE"
if not go_license.is_file():
    # Arch 将工具链许可证从 GOROOT 移到标准系统许可目录。
    go_license = pathlib.Path("/usr/share/licenses/go/LICENSE")
shutil.copyfile(go_license, dest / "Go-LICENSE")
# Go 标准库 vendored 代码也保留原始许可证。
for entry in sorted((goroot / "src/vendor").rglob("LICENSE*")):
    if entry.is_file():
        output = dest / "go-stdlib-vendor" / entry.relative_to(goroot / "src/vendor")
        output.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(entry, output)
(dest / "go-modules.json").write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + "\n")
