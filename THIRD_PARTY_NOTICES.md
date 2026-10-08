# 第三方来源与许可

本项目自有代码按 GNU GPL v3（GPL-3.0-only）提供，完整条款见 LICENSE；第三方代码和素材保留各自的权利及许可。本项目与腾讯、QQ 音乐没有官方隶属关系，软件许可不授予音乐、商标或账号服务的额外使用权。

## Python 后端

`tui/backend/qqmusic_api.py` 从 statindet Quickshell 配置项目的 `scripts/qqmusic/qqmusic_api.py` 引入；该来源项目以 GPL-3.0 提供。此次导入仅涉及脚本源码，不包含配置、登录凭据、缓存或用户数据。后续修改记录见 Git 历史及脚本内说明。

网页请求签名实现保留了对 [jixunmoe/qmweb-sign](https://github.com/jixunmoe/qmweb-sign) 分析的引用。其 MIT 许可及版权声明保存在 `licenses/qmweb-sign-MIT.txt`，不以本项目 GPL 声明替代。

## Go 二进制依赖

版本与校验记录在 `tui/go.mod` 和 `tui/go.sum`。构建时从实际参与编译的 Go 模块及工具链收集原始许可证、版权通知至发行物内 `share/licenses/qqmusic-tui/`；源模块与版本映射一并记录。依赖原始许可不因链接到本项目而被改写。发布源码包和构建脚本与同版本二进制同时提供。

主要依赖包括 Charm 的 Bubble Tea / Lip Gloss / ANSI 工具、godbus、modernc SQLite 及其传递依赖。mpv、Python、系统 CA、D-Bus 等由系统包管理器提供，不将系统程序复制进本软件包。

## 网页演示资源

- `assets/lofi.jpg`、`assets/synthwave.jpg`：项目提供者已确认可以随项目公开再分发；不据此推断第三方商标或音乐授权。
- [Lucide](https://lucide.dev/) 图标：通过 CDN 加载，使用其原始 ISC 许可。
- Google Fonts 字体：由字体服务加载，各字体保留其原始许可。
- SoundHelix 等演示音频、外部头像：仅保留原远程引用，不下载后重新打包；来源在 `app.js` 中。曲目标题是界面演示数据，不表明远程音频与该歌曲相同。

网页依赖外部服务，不承诺离线可用。发布包中的项目许可证不覆盖外部服务条款，也不授予远程资源的额外再分发权。
