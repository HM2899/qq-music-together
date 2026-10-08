package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
)

// 发行构建通过 -ldflags "-X main.version=... -X main.commit=..." 注入。
var version = "dev"
var commit = "unknown"

const cliHelp = `QQ 音乐终端播放器

用法：qqmusic-tui [--help | --version]
  --help, -h    显示帮助并退出
  --version    显示版本和提交信息并退出
  无参数       启动交互播放器（需要 mpv 和 Python 3）

环境变量：
  QQMUSIC_API      显式指定后端脚本；无效时不会回退
  QQMUSIC_PYTHON   Python 解释器路径或命令名（默认 python3）

默认后端依次查找可执行文件相对的 ../share/qqmusic-tui/backend/、backend/，
然后查找 /usr/local/share/qqmusic-tui/backend/ 和 /usr/share/qqmusic-tui/backend/。
不从当前工作目录查找默认后端。
后端兼容状态目录：$XDG_CACHE_HOME/quickshell/qqmusic（默认 ~/.cache 下）。
它可能与桌面客户端共享登录及退出状态。
`

// startupArgs 必须在音频、数据库、D-Bus 和 API 初始化之前调用。
func startupArgs(args []string, stdout, stderr io.Writer) (handled bool, code int) {
	if len(args) == 0 {
		return false, 0
	}
	if len(args) == 1 {
		switch args[0] {
		case "--help", "-h":
			fmt.Fprint(stdout, cliHelp)
			return true, 0
		case "--version":
			fmt.Fprintf(stdout, "qqmusic-tui %s (commit %s)\n", version, commit)
			return true, 0
		}
	}
	fmt.Fprintln(stderr, "参数无效；请运行 qqmusic-tui --help 查看用法")
	return true, 2
}

func main() {
	if handled, code := startupArgs(os.Args[1:], os.Stdout, os.Stderr); handled {
		os.Exit(code)
	}
	// QQMUSIC_TUI_NO_AUDIO=1 时用空音频输出（无头环境 / 不打扰地验证）。
	var extra []string
	if os.Getenv("QQMUSIC_TUI_NO_AUDIO") == "1" {
		extra = append(extra, "--ao=null")
	}
	mpv, err := StartMpv(70, extra...)
	if err != nil {
		fmt.Fprintln(os.Stderr, "启动播放器失败:", err)
		os.Exit(1)
	}
	defer mpv.Close()

	model := NewModel(mpv)
	// baseCtx 管「随程序退出取消」，每次接口调用再在 Client.run 内派生超时。
	// 分开是因为用同一个 ctx 兼做两件事的话，退出时正在跑的 Cmd 要拖到超时才结束。
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	model = model.WithContext(ctx)
	// 退出时删掉状态文件，别给桌面顶栏留一条僵尸歌词。
	// （被 kill -9 时删不掉，那种情况由桌面侧的新鲜度判据兜底。）
	defer model.pub.Remove()

	// WithoutSignals：bubbletea 默认自己处理 SIGTERM 并直接退出，会绕过下面的
	// defer，留下 mpv 进程。这里关掉它，由我们自己接管信号。
	// （Ctrl+C 不受影响：raw 模式下它是普通输入字节，由 Update 处理。）
	// MPRIS：让灵动岛 / 锁屏媒体卡 / 全局媒体键看得见并控制得了 TUI。
	// 总线不可用（无图形会话、ssh 里跑）就降级为不发布，TUI 本身照常能用。
	var mpris *MprisServer
	if os.Getenv("QQMUSIC_TUI_NO_MPRIS") != "1" {
		if s, err := StartMpris(); err == nil {
			mpris = s
			defer mpris.Close()
			model = model.WithMpris(mpris)
		}
	}

	// 上次的播放状态（SQLite）。打不开就不记，TUI 照常能用。
	store, err := OpenStore(storePath())
	if err != nil {
		fmt.Fprintln(os.Stderr, "打不开播放状态数据库（这次不会记住播放进度）:", err)
	}
	defer store.Close()
	model = model.WithStore(store)

	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithoutSignals())
	mpris.Attach(p.Send)

	// 被 SIGTERM（例如关机）打断时走正常退出路径，让 defer 完成清理。
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		<-sig
		p.Quit()
	}()

	final, err := p.Run()
	// 退出前强制存一次，位置精确到退出那一刻（平时位置只每 5 秒落盘一次）。
	if fm, ok := final.(Model); ok {
		fm.persist(true)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "程序异常退出:", err)
		os.Exit(1)
	}
}
