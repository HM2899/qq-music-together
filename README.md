# QQ 音乐 TUI

使用 Go 编写的非官方 QQ 音乐终端播放器，支持搜索、播放、歌词、收藏、歌单、扫码登录与桌面媒体控制。通过随附的 Python 后端连接真实 QQ 音乐服务，产品不内置示例曲目。

> **使用前请注意**
>
> - TUI 已随附真实后端 `tui/backend/qqmusic_api.py`，只使用 Python 标准库，**无需 Quickshell 或 pip 安装依赖**。系统仍需 mpv、Python 3 和 CA 证书；登录与播放需要网络。
> - 本项目为非官方实现。曲目、音质和账号写操作受服务端接口、版权及账号权限限制，不提供绕过付费或版权限制的能力。

## Linux 软件包

从本仓库 **Releases** 选择与你架构对应的文件；在首次正式 tag 发布前，主分支 Actions 提供版本为 `0.0.0` 的测试制品，不是正式发行版。

| 系统 | 软件包 | 安装方式（替换为实际文件名） |
| --- | --- | --- |
| Debian / Ubuntu 及衍生版 | `.deb` | `sudo apt install ./qqmusic-tui_*.deb` |
| Fedora / RPM 系 | `.rpm` | `sudo dnf install ./qqmusic-tui_*.rpm` |
| openSUSE | `.rpm` | `sudo zypper install ./qqmusic-tui_*.rpm` |
| Arch Linux / EndeavourOS / Manjaro | `.pkg.tar.zst` | `sudo pacman -U ./qqmusic-tui_*.pkg.tar.zst` |
| Alpine | `.apk` | `sudo apk add --allow-untrusted ./qqmusic-tui_*.apk` |
| 其他 Linux | `.tar.gz` | 解压并运行其中的 `bin/qqmusic-tui` |

这些是项目自行构建的包，不是发行版官方仓库或 AUR 的包。Alpine 包暂未签名，只有核对来源与 SHA256 后才使用 `--allow-untrusted`。下载同一版本的 `SHA256SUMS` 后，可用 `sha256sum --ignore-missing -c SHA256SUMS` 校验已有文件；校验和不是数字签名。

包架构为 `amd64`（x86_64）和 `arm64`（aarch64）。通用归档仍需要系统安装 **mpv、Python 3、CA 证书**，不是完全自包含的播放器。MPRIS 需要用户会话 D-Bus，外部打开二维码可选安装 `xdg-utils`。

CI 默认只做构建、包结构和无副作用启动检查，**容器安装测试默认关闭**。如需验证，可手动运行 Actions 并勾选 `container_smoke`：包含 Debian、Fedora、Alpine 的两架构安装/卸载冒烟测试，以及 Arch x86_64 测试；Alpine 临时容器会使用 `--allow-untrusted`。Arch ARM 仅构建和结构检查。具体版本是否验证成功以该次 Actions 结果为准，不宣称兼容所有发行版历史版本。安装不会添加 systemd 服务、自启动或自动登录。

## 源码构建与快速开始

### 依赖

| 依赖 | 用途 |
| --- | --- |
| Go 1.27 或兼容的更新工具链 | 与 `tui/go.mod` 声明一致，用于构建 |
| Python 3 | 执行随附 API 脚本，仅需标准库 |
| mpv | 实际音频播放与进度控制 |
| ca-certificates | HTTPS 证书验证 |
| make（可选） | 简化构建、运行和测试命令 |

当前实现面向 Linux/Unix 环境，使用 Unix socket 与可选的会话 D-Bus；不承诺 Windows 原生兼容性。建议使用支持真彩色的终端。

### 构建与运行

在项目根目录执行：

```bash
cd tui
go mod download
make build
```

直接启动，程序会从可执行文件旁找到随附的 `backend/qqmusic_api.py`：

```bash
./qqmusic-tui
./qqmusic-tui --version
```

安装包会把后端放在 `/usr/share/qqmusic-tui/backend/qqmusic_api.py`，便携包保留 `bin/` 与 `share/` 相对目录。请勿只搬走便携包中的二进制。如需使用自定义兼容后端，设置 `QQMUSIC_API=/绝对路径/qqmusic_api.py`；显式指定无效路径时不会静默切换到其他脚本。程序不从任意当前工作目录搜寻并执行脚本。

如果不使用 make，构建命令为：

```bash
go build -buildvcs=false -o qqmusic-tui .
```

已配置好 API 后，也可以直接在 `tui/` 下运行 `make run`。

### 功能

- **七个页签**：探索、搜索、正在播放、我喜欢、音乐库、播放队列、我的歌单。
- 搜索歌曲、歌手、专辑与歌单，支持分页和歌单/专辑详情。
- mpv 播放、同步歌词与翻译、终端真彩色专辑封面。
- QQ / 微信扫码登录、切换账号及退出登录。
- 收藏切换、歌单创建/删除、歌曲加入/移出歌单。
- 查看评论、热评/最新切换、评论点赞与发送评论的界面和接口调用；实际成功与否取决于后端支持及账号权限。
- 标准、128k、320k、FLAC 四档音质选择；最终音质受曲目与账号权限限制。
- SQLite 保存队列、播放位置、音量、静音与音质；重启后暂停在上次位置，手动继续播放。
- MPRIS 桌面媒体控制，可被兼容的媒体键、锁屏播放器和 `playerctl` 控制。

歌手搜索结果回车会按歌手名搜索歌曲，不是打开独立歌手详情页。

### 常用快捷键

| 按键 | 操作 |
| --- | --- |
| `1`–`7` / `Tab` / `Shift+Tab` | 切换页签 |
| `/` / `Ctrl+T` | 打开搜索 / 切换搜索类型 |
| `↑` `↓` / `j` `k` | 移动选择 |
| `Enter` | 播放选中歌曲，或打开歌单/专辑详情 |
| `Esc` | 离开输入框或关闭浮层 |
| `空格` / `n` / `p` | 播放暂停 / 下一首 / 上一首 |
| `←` / `→` | 快退 / 快进 5 秒 |
| `-` / `=` / `m` | 减小音量 / 增大音量 / 静音 |
| `a` | 在曲目列表中追加到队列；正在播放页中切换歌词跟随 |
| `f` / `A` | 收藏切换 / 加入自己的歌单 |
| `Q` / `L` | 切换音质 / 登录或账号页 |
| `c` | 查看当前歌曲评论；播放队列页中是清空队列 |
| `?` / `Ctrl+C` | 快捷键帮助 / 退出 |

在搜索框中，先按 `Enter` 搜索，再按 `Esc` 离开输入态，最后按 `Enter` 播放结果。输入框里的 `q` 是普通文字，不会退出程序。

从列表回车播放会**整体替换播放队列**；想保留现有队列时使用 `a` 追加。

详细操作见 [终端版说明](tui/README.md)。

### 环境变量与本地数据

| 变量 | 默认值或行为 | 用途 |
| --- | --- | --- |
| `QQMUSIC_API` | 默认查找随包 share 路径、可执行文件旁的 backend、系统 share 路径 | 显式覆盖后端脚本路径 |
| `QQMUSIC_PYTHON` | `python3` | Python 可执行文件 |
| `QQMUSIC_TUI_DB` | `${XDG_DATA_HOME:-$HOME/.local/share}/qqmusic-tui/state.db` | 队列及播放偏好数据库 |
| `QQMUSIC_TUI_STATE_FILE` | `$HOME/.cache/qqmusic-tui/now.json` | 桌面组件读取的当前播放状态 |
| `QQMUSIC_TUI_NO_AUDIO` | 未设置 | 设为 `1` 使用 mpv 空音频输出，仍需 mpv，不代表离线模式 |
| `QQMUSIC_TUI_NO_MPRIS` | 未设置 | 设为 `1` 禁用 MPRIS |
| `QQMUSIC_TUI_CONFIG` | 用户配置目录下的 `qqmusic-tui/config.json` | 旧版音质设置迁移来源，新设置保存在 SQLite |

随附后端将扫码会话、Cookie、二维码及头像保存在 `${XDG_CACHE_HOME:-$HOME/.cache}/quickshell/qqmusic/`。这是兼容历史客户端的目录名，不要求安装 Quickshell；如果你也使用原桌面客户端，两者会共享登录、刷新和退出状态。当前后端会话写入没有跨进程锁，避免同时切换或刷新同一账号；不会自动迁移、复制或删除旧会话。会话是以文件权限保护的明文 JSON，请勿公开。自定义后端的数据位置由脚本自行决定。`now.json` 包含当前播放信息，正常退出时会删除。

## 开发与测试

在项目根目录执行：

```bash
make -C tui test
```

可选竞态检测：

```bash
make -C tui test-race
```

- TUI 测试使用 `tui/testdata/fake_api.py` 和 `tui/fixtures_test.go` 中的离线 fixture，不需要真实账号；它们是测试替身与假曲目，不可替代真实后端使用，也不是产品内置曲库。
- 播放相关测试使用 mpv 空输出和 ffmpeg 生成的音频；D-Bus 测试使用私有 `dbus-daemon`。完整运行请安装 `mpv`、`ffmpeg` 和 `dbus-daemon`。
- 首次下载 Go 模块或工具链可能需要网络；测试逻辑本身不调用真实 QQ 音乐服务。
- 自动化测试不能代替真实出声、手机扫码和账号写操作的人工验收。

## 构建发行物与 GitHub 自动打包

需要 Go 1.27、Python 3、bash、GNU tar、zstd、binutils、libarchive（`bsdtar`）。固定版本 nFPM 由下面的脚本安装到你指定的空目录，不改变系统包：

```bash
bash packaging/install-tools.sh /tmp/qqmusic-nfpm-tools
export NFPM=/tmp/qqmusic-nfpm-tools/nfpm
bash packaging/build.sh --arch amd64 --version 0.1.0 --commit "$(git rev-parse HEAD)" --output dist
bash packaging/build.sh --arch arm64 --version 0.1.0 --commit "$(git rev-parse HEAD)" --output dist
bash packaging/bundle.sh --version 0.1.0 --ref HEAD --output dist
bash packaging/checksums.sh dist
bash packaging/verify.sh dist
```

上面的版本号仅为命令示例，不表示该版本已经发布。源码包来自指定 Git 提交，不包括未提交改动。发行产物采用明确文件清单，包含 TUI、Python 后端、文档与许可，不将整个工作目录原样打包。

每个正式版本只发布 `amd64` / `arm64` 两架构各五种格式（`.deb`、`.rpm`、`.pkg.tar.zst`、`.apk`、`.tar.gz`），以及对应源码包和 `SHA256SUMS`。

- `ci.yml`：主分支推送、PR、手动运行时执行离线测试、竞态检测、双架构构建和包检查；成功后保存测试包 7 天。容器安装测试需在手动运行时单独勾选，默认不执行。
- `release.yml`：推送严格的 `vMAJOR.MINOR.PATCH` tag 时检查它属于 main、运行检查、构建两架构各五种格式，再上传对应源码与 `SHA256SUMS`，完整后才公开 Release。
- 只有最后的发布 job 有仓库写权限；PR 不使用发布令牌。不自动启用 Pages、部署服务器或上传 AUR。

维护者完成测试并确定正式版本后，可手工创建和推送 tag，例如 `git tag v0.1.0 && git push origin v0.1.0`。不要覆盖已有正式 tag/Release；建议在 GitHub 仓库设置中限制 `v*` tag 的创建和删除权限。

## 许可证

自有代码按 [GNU GPL v3](LICENSE)（GPL-3.0-only）提供，无担保。第三方代码、依赖及在线音乐内容保留原权利和许可，真实 API 签名参考实现的 MIT 归属见 [第三方声明](THIRD_PARTY_NOTICES.md)。软件许可不授予歌曲版权或绕过账号权限的能力。

## 目录结构

```text
.
├── README.md              # 项目说明
├── LICENSE                # GPL-3.0-only 许可
├── THIRD_PARTY_NOTICES.md  # 第三方来源与许可
├── licenses/              # 第三方原始许可
├── .gitignore             # 隐私数据及本地产物忽略规则
├── .github/workflows/     # CI 与正式发布流程
├── packaging/             # 双架构打包、源码归档、校验及安装测试
└── tui/
    ├── README.md          # 终端版详细说明
    ├── Makefile           # 构建与测试入口
    ├── go.mod / go.sum    # Go 依赖及校验
    ├── backend/           # 随附的真实 qqmusic_api.py 后端
    ├── main.go            # 终端程序入口
    ├── api.go             # Python 后端桥接
    ├── ui.go              # 界面状态机与按键
    ├── mpv.go             # 播放引擎
    ├── store.go           # SQLite 持久化
    ├── mpris.go           # 桌面媒体控制
    ├── *_test.go          # 自动化测试及离线 fixture
    └── testdata/          # 测试后端及数据
```

## 隐私与 GitHub 上传

[`.gitignore`](.gitignore) 已覆盖常见的登录会话、Cookie、令牌文件、环境变量、私钥、播放历史、SQLite 数据库、二维码、日志、浏览器调试产物和编译后的播放器。源码、测试、`go.mod` 与 `go.sum` 应正常提交。

这些规则仅对 Git 忽略匹配的**未跟踪文件**生效：

- 不会扫描或清除源码内嵌的密钥，也不阻止手工强制添加文件。
- 不影响直接把整个文件夹压缩上传、使用网页上传文件等操作。
- 不会自动删除已经提交到历史中的敏感内容。
- 新增其他名称的会话文件、截图或导出文件时，需要另行检查并补充规则。

初始化 Git 后，在每次提交前检查：

```bash
git status --short --ignored
git add --dry-run .
git diff --cached
```

不要提交真实账号、Cookie、二维码、播放记录、抓包文件或带个人信息的截图。如果凭据曾公开上传，应先撤销或轮换，再处理 Git 历史；只增加忽略规则不足以消除泄露。
