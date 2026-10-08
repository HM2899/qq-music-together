#!/usr/bin/env python3
"""离线验证发行包：不安装、不运行包钩子、不访问真实 HOME。"""
import hashlib
import io
import json
import os
import pathlib
import platform
import re
import struct
import subprocess
import sys
import tarfile
import tempfile


def require(condition, message):
    if not condition:
        raise ValueError(message)


def run(*args):
    return subprocess.check_output(args)


def archive(path):
    # libarchive 统一读取 RPM/cpio、zstd、gzip；先转 tar 再在内存检查，绝不解包任意路径。
    data = run("bsdtar", "-cf", "-", "@" + str(path.resolve()))
    return tarfile.open(fileobj=io.BytesIO(data), mode="r:")


def members(tar):
    result = {}
    for item in tar:
        path = pathlib.PurePosixPath(item.name)
        require(not path.is_absolute() and ".." not in path.parts, f"危险包路径：{item.name}")
        require(item.isfile() or item.isdir(), f"不允许链接/设备文件：{item.name}")
        require(item.uid == 0 and item.gid == 0, f"非 root 属主：{item.name}")
        if item.isfile():
            name = str(path)
            require(name not in result, f"重复路径：{name}")
            result[name] = (tar.extractfile(item).read(), item.mode & 0o7777)
    return result


def rpm_header(data, offset):
    require(data[offset:offset + 3] == b"\x8e\xad\xe8", "无效 RPM header")
    count, size = struct.unpack_from(">II", data, offset + 8)
    start = offset + 16 + count * 16
    result = {}
    for index in range(count):
        tag, kind, pos, length = struct.unpack_from(">IIII", data, offset + 16 + index * 16)
        if kind in (6, 8, 9):
            result[tag] = data[start + pos:start + size].split(b"\0")[:length]
    return result, start + size


def text_values(data, delimiter=" = "):
    result = {}
    for line in data.decode().splitlines():
        if delimiter in line:
            key, value = line.split(delimiter, 1)
            result.setdefault(key, []).append(value)
    return result


def metadata(path, fmt, version, arch):
    deps = {"mpv", "python" if fmt == "pkg.tar.zst" else "python3", "ca-certificates"}
    machine = {"amd64": "x86_64", "arm64": "aarch64"}[arch]
    if fmt == "deb":
        names = run("bsdtar", "-tf", str(path)).decode().splitlines()
        controls = [n for n in names if n.startswith("control.tar")]
        payloads = [n for n in names if n.startswith("data.tar")]
        require(len(controls) == len(payloads) == 1, "无效 deb 成员")
        control_tar = tarfile.open(fileobj=io.BytesIO(run("bsdtar", "-xOf", str(path), controls[0])), mode="r:*")
        control_files = members(control_tar)
        require(set(control_files) <= {"control", "md5sums", "conffiles"}, "deb 包含未授权安装脚本")
        control = control_files["control"][0]
        values = text_values(control, ": ")
        require(values.get("Package") == ["qqmusic-tui"] and values.get("Version") == [version + "-1"] and values.get("Architecture") == [arch], "deb 名称/版本/架构错误")
        require(deps <= set(values["Depends"][0].split(", ")), "deb 依赖不全")
        return members(tarfile.open(fileobj=io.BytesIO(run("bsdtar", "-xOf", str(path), payloads[0])), mode="r:*"))
    content = members(archive(path))
    if fmt == "rpm":
        data = path.read_bytes()
        _, offset = rpm_header(data, 96)
        values, _ = rpm_header(data, (offset + 7) & ~7)
        require(values[1000] == [b"qqmusic-tui"] and values[1001] == [version.encode()] and values[1002] == [b"1"] and values[1022] == [machine.encode()], "rpm 名称/版本/架构错误")
        require({d.encode() for d in deps} <= set(values[1049]), "rpm 依赖不全")
        require(values[1014] == [b"GPL-3.0-only"], "rpm 许可错误")
    elif fmt in ("apk", "pkg.tar.zst"):
        values = text_values(content.pop(".PKGINFO")[0])
        suffix = "-r1" if fmt == "apk" else "-1"
        require(values.get("pkgname") == ["qqmusic-tui"] and values.get("pkgver") == [version + suffix] and values.get("arch") == [machine], "apk/Arch 名称/版本/架构错误")
        require(deps <= set(values.get("depend", [])), "apk/Arch 依赖不全")
        require(values.get("license") == ["GPL-3.0-only"], "apk/Arch 许可错误")
        content.pop(".MTREE", None)
        content.pop(".BUILDINFO", None)
    return content


def binary_check(data, version, commit, arch):
    require(data[:4] == b"\x7fELF", "不是 ELF 二进制")
    require(data[4:6] == b"\x02\x01", "预期 64 位小端 ELF")
    require(struct.unpack_from("<H", data, 18)[0] == {"amd64": 62, "arm64": 183}[arch], "ELF 架构错误")
    with tempfile.TemporaryDirectory(prefix="qqmusic-verify-") as temp:
        root = pathlib.Path(temp)
        binary = root / "qqmusic-tui"
        binary.write_bytes(data)
        binary.chmod(0o755)
        require(b"INTERP" not in run("readelf", "-l", str(binary)), "ELF 有动态解释器")
        require(b"NEEDED" not in run("readelf", "-d", str(binary)), "ELF 有动态链接依赖")
        info = run("go", "version", "-m", str(binary)).decode()
        for expected in ("go1.27.", "CGO_ENABLED=0", "GOOS=linux", "GOARCH=" + arch, "-trimpath=true"):
            require(expected in info, f"缺少构建属性 {expected}")
        if {"x86_64": "amd64", "aarch64": "arm64"}.get(platform.machine()) == arch:
            home = root / "home"
            home.mkdir()
            env = {"HOME": str(home), "XDG_CACHE_HOME": str(home / "cache"), "XDG_CONFIG_HOME": str(home / "config"), "XDG_STATE_HOME": str(home / "state"), "XDG_DATA_HOME": str(home / "data"), "PATH": "/nonexistent", "LANG": "C.UTF-8"}
            result = subprocess.check_output([str(binary), "--version"], env=env, cwd=root, timeout=10).decode()
            require(version in result and commit in result, "--version 没有正确版本与提交")
            subprocess.run([str(binary), "--help"], env=env, cwd=root, check=True, stdout=subprocess.DEVNULL, timeout=10)
            require(not list(home.rglob("*")), "--help/--version 写入了用户状态")


def package(path, version, arch, fmt):
    content = metadata(path, fmt, version, arch) if fmt != "tar.gz" else members(archive(path))
    prefix = "" if fmt == "tar.gz" else "usr/"
    require(all(name.startswith(prefix) for name in content), "包体包含非 /usr 路径")
    content = {name[len(prefix):]: value for name, value in content.items()}
    manifest_name = "share/qqmusic-tui/manifest.json"
    manifest = json.loads(content.pop(manifest_name)[0])
    require(manifest["version"] == version and manifest["arch"] == arch, "清单版本/架构错误")
    require(re.fullmatch(r"[0-9a-f]{7,40}", manifest["commit"]), "清单提交格式错误")
    require(set(content) == set(manifest["files"]), "清单与实际包体不同")
    required = {"bin/qqmusic-tui", "share/qqmusic-tui/backend/qqmusic_api.py", "share/licenses/qqmusic-tui/LICENSE", "share/licenses/qqmusic-tui/THIRD_PARTY_NOTICES.md", "share/licenses/qqmusic-tui/Go-LICENSE", "share/licenses/qqmusic-tui/go-modules.json", "share/licenses/qqmusic-tui/third-party/qmweb-sign-MIT.txt"}
    require(required <= set(content), "缺少可执行文件、后端或许可证")
    for name, (data, mode) in content.items():
        require(name in required or name.startswith("share/licenses/qqmusic-tui/"), f"安装清单越界：{name}")
        require(mode == (0o755 if name == "bin/qqmusic-tui" else 0o644), f"文件权限错误：{name}")
        require(hashlib.sha256(data).hexdigest() == manifest["files"][name], f"文件校验失败：{name}")
        require(not re.search(r"(^|/)(session\.json|.*\.db|.*\.log|__pycache__|\.env)(/|$)", name), f"疑似私有状态：{name}")
    compile(content["share/qqmusic-tui/backend/qqmusic_api.py"][0], "qqmusic_api.py", "exec")
    modules = json.loads(content["share/licenses/qqmusic-tui/go-modules.json"][0])
    require(bool(modules), "Go 依赖许可清单为空")
    for module in modules:
        require(bool(module["licenses"]), "模块缺少许可证")
        for notice in module["licenses"]:
            require(f'share/licenses/qqmusic-tui/go-modules/{module["module"]}@{module["version"]}/{notice}' in content, "模块许可证缺失")
    binary_check(content["bin/qqmusic-tui"][0], version, manifest["commit"], arch)
    return manifest


def bundle(path):
    content = members(archive(path))
    source = "_source" in path.name
    match = re.fullmatch(r"qqmusic-(?:together_([0-9.]+)_source|web_([0-9.]+))\.tar\.gz", path.name)
    require(match, "无效源码/网页包名称")
    prefix = ("qqmusic-together-" if source else "qqmusic-web-") + (match[1] or match[2]) + "/"
    require(all(n.startswith(prefix) for n in content), "归档顶层目录不一致")
    names = {n[len(prefix):] for n in content}
    required = {"LICENSE", "THIRD_PARTY_NOTICES.md", "app.js", "index.html", "style.css"}
    if source:
        required |= {"tui/go.mod", "tui/go.sum", "tui/backend/qqmusic_api.py", "packaging/nfpm.yaml"}
    require(required <= names, "源码/网页归档缺少必要文件")
    for name in names:
        require(not re.search(r"(^|/)(\.git|\.env|session\.json|__pycache__|node_modules|.*\.db|.*\.log|qqmusic-tui)(/|$)", name), f"归档包含私有状态/二进制：{name}")
        if not source:
            require(name in required or name.startswith(("assets/", "licenses/")), f"网页清单越界：{name}")


def main():
    root = pathlib.Path(sys.argv[1]).resolve()
    files = sorted(p for p in root.iterdir() if p.name.startswith("qqmusic-") and p.is_file())
    require(bool(files), "没有发行产物")
    sums = root / "SHA256SUMS"
    if sums.exists():
        expected = {}
        for line in sums.read_text().splitlines():
            digest, name = line.split("  ", 1)
            require(re.fullmatch("[0-9a-f]{64}", digest) and name not in expected, "非法 SHA256SUMS")
            expected[name] = digest
        require(set(expected) == {p.name for p in files}, "SHA256SUMS 与发行目录文件不一致")
        for p in files:
            require(hashlib.sha256(p.read_bytes()).hexdigest() == expected[p.name], f"SHA256 失败：{p.name}")
    groups = {}
    manifests = {}
    for path in files:
        require(not path.is_symlink(), "发行文件不能为链接")
        match = re.fullmatch(r"qqmusic-tui_([0-9.]+)_linux_(amd64|arm64)\.(deb|rpm|apk|pkg\.tar\.zst|tar\.gz)", path.name)
        if match:
            version, arch, fmt = match.groups()
            manifest = package(path, version, arch, fmt)
            key = (version, arch)
            groups.setdefault(key, set()).add(fmt)
            require(key not in manifests or manifest == manifests[key], "不同包格式的安装清单不同")
            manifests[key] = manifest
        else:
            bundle(path)
        print("通过：" + path.name, flush=True)
    for key, formats in groups.items():
        require(formats == {"deb", "rpm", "apk", "pkg.tar.zst", "tar.gz"}, f"{key} 缺少发行格式")
    print("全部产物校验通过；仅宿主匹配架构运行了 --version/--help，未执行安装或 ARM 仿真。")


if __name__ == "__main__":
    try:
        main()
    except (ValueError, KeyError, subprocess.CalledProcessError) as error:
        raise SystemExit(f"校验失败：{error}")
