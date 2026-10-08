# 第三方来源与许可

本项目自有代码按 GNU GPL v3（GPL-3.0-only）提供，完整条款见 LICENSE；第三方代码和素材保留各自的权利及许可。本项目与腾讯、QQ 音乐没有官方隶属关系，软件许可不授予音乐、商标或账号服务的额外使用权。

## Python 后端

`tui/backend/qqmusic_api.py` 从 statindet Quickshell 配置项目的 `scripts/qqmusic/qqmusic_api.py` 引入；该来源项目以 GPL-3.0 提供。此次导入仅涉及脚本源码，不包含配置、登录凭据、缓存或用户数据。后续修改记录见 Git 历史及脚本内说明。

QQ 音乐真实 API 的请求签名实现保留了对 [jixunmoe/qmweb-sign](https://github.com/jixunmoe/qmweb-sign) 分析的引用。其 MIT 许可及版权声明保存在 `licenses/qmweb-sign-MIT.txt`，不以本项目 GPL 声明替代。

## Go 二进制依赖

版本与校验记录在 `tui/go.mod` 和 `tui/go.sum`。构建时从实际参与编译的 Go 模块及工具链收集原始许可证、版权通知至发行物内 `share/licenses/qqmusic-tui/`；源模块与版本映射一并记录。依赖原始许可不因链接到本项目而被改写。发布源码包和构建脚本与同版本二进制同时提供。

主要依赖包括 Charm 的 Bubble Tea / Lip Gloss / ANSI 工具、godbus、modernc SQLite 及其传递依赖。mpv、Python、系统 CA、D-Bus 等由系统包管理器提供，不将系统程序复制进本软件包。

## 在线服务与离线测试数据

音乐、歌词、封面、头像等内容通过 QQ 音乐服务获取，受各自权利人及服务条款约束，不随发行包提供音乐资源。项目许可证不授予这些内容的额外使用或再分发权。

`tui/fixtures_test.go` 与 `tui/testdata/` 保留离线测试用的假曲目、歌词和接口响应；播放测试通过 ffmpeg 生成本地音频并注入播放器，不依赖在线音源。这些 fixture 不构成产品内置曲库，也不表明对应正式音乐资源的授权。
