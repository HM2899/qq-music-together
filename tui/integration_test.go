package main

import (
	"bytes"
	"io"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// syncBuffer 是并发安全的输出缓冲（bubbletea 在另一个 goroutine 里写）。
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]|\x1b\][^\x07]*(\x07|\x1b\\)|\x1b[()][A-Z0-9]|\x1b[=>]`)

func stripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

// TestIntegrationStartupAndRender 端到端：真 mpv + 真 bubbletea 程序，
// 验证启动后画面里出现曲目、歌词与播放控制。
func TestIntegrationStartupAndRender(t *testing.T) {
	audio := makeTestAudio(t)
	mpv, err := StartMpv(70, "--ao=null", "--no-config")
	if err != nil {
		t.Fatalf("启动 mpv 失败: %v", err)
	}
	defer mpv.Close()

	songs := []Track{{
		ID: "t1", Title: "Test Track", Artist: "Tester", Album: "Test Album",
		DirectURL: audio,
		Lyrics:    []Lyric{{Time: 0, Text: "第一句歌词"}, {Time: 2, Text: "第二句歌词"}},
	}}
	m := newModelWithQueue(mpv, songs)

	inR, inW := io.Pipe()
	defer inW.Close()
	out := &syncBuffer{}

	p := tea.NewProgram(m, tea.WithInput(inR), tea.WithOutput(out))
	done := make(chan error, 1)
	go func() { _, err := p.Run(); done <- err }()

	// 等程序进入事件循环再注入窗口尺寸。
	time.Sleep(400 * time.Millisecond)
	p.Send(tea.WindowSizeMsg{Width: 120, Height: 35})
	time.Sleep(900 * time.Millisecond)

	screen := stripANSI(out.String())
	for _, want := range []string{"QQ音乐 TUI", "Test Track", "Tester"} {
		if !strings.Contains(screen, want) {
			t.Errorf("画面应包含 %q", want)
		}
	}

	// 歌词只在「正在播放」页上，按 3 切过去。
	p.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	time.Sleep(500 * time.Millisecond)
	screen = stripANSI(out.String())
	for _, want := range []string{"正在播放", "第一句歌词"} {
		if !strings.Contains(screen, want) {
			t.Errorf("「正在播放」页应包含 %q", want)
		}
	}

	// 播放应真的在推进。
	if pos := mpv.TimePos(); pos <= 0 {
		t.Errorf("播放进度应大于 0，实际 %v", pos)
	}

	p.Quit()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("程序退出报错: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("程序没能正常退出")
	}
}

// TestKeyControlsDriveMpv 验证按键真的作用到播放器上。
func TestKeyControlsDriveMpv(t *testing.T) {
	audio := makeTestAudio(t)
	mpv, err := StartMpv(70, "--ao=null", "--no-config")
	if err != nil {
		t.Fatalf("启动 mpv 失败: %v", err)
	}
	defer mpv.Close()

	songs := []Track{
		{ID: "1", Title: "A", DirectURL: audio, Lyrics: []Lyric{{Time: 0, Text: "a"}}},
		{ID: "2", Title: "B", DirectURL: audio, Lyrics: []Lyric{{Time: 0, Text: "b"}}},
	}
	m := newModelWithQueue(mpv, songs)
	m.w, m.h = 120, 35
	time.Sleep(500 * time.Millisecond)

	// 空格 -> 暂停
	m = pressKey(t, m, " ")
	if !m.paused {
		t.Error("按空格后应暂停")
	}
	if !mpv.Paused() {
		t.Error("mpv 应真的暂停了")
	}
	m = pressKey(t, m, " ")
	if m.paused {
		t.Error("再按空格应恢复播放")
	}

	// n -> 下一首
	m = pressKey(t, m, "n")
	if m.curIndex != 1 {
		t.Errorf("按 n 后应切到第 2 首，实际索引 %d", m.curIndex)
	}

	// p -> 上一首
	m = pressKey(t, m, "p")
	if m.curIndex != 0 {
		t.Errorf("按 p 后应回到第 1 首，实际索引 %d", m.curIndex)
	}

	// 音量（↑↓ 现在是移动列表游标，音量改用 -/=）
	m = pressKey(t, m, "-")
	if m.volume != 65 {
		t.Errorf("按 - 后音量应为 65，实际 %d", m.volume)
	}

	// 静音
	m = pressKey(t, m, "m")
	if !m.muted {
		t.Error("按 m 后应静音")
	}
}

// TestChainedKeys 覆盖 切歌->跳转->音量 的组合操作。
func TestChainedKeys(t *testing.T) {
	audio := makeTestAudio(t)
	mpv, err := StartMpv(70, "--ao=null", "--no-config")
	if err != nil {
		t.Fatalf("启动 mpv 失败: %v", err)
	}
	defer mpv.Close()

	m := newModelWithQueue(mpv, []Track{{ID: "1", Title: "A", DirectURL: audio, Lyrics: []Lyric{{Time: 0, Text: "a"}}}})
	m.w, m.h = 100, 30
	time.Sleep(1500 * time.Millisecond)

	// 量 mpv 的真实进度，而不是 Model 里那个数。Model.pos 是「界面上该显示的进度」，
	// 它按墙钟在两次采样之间外推，本来就不该等于某个固定的秒数。
	time.Sleep(200 * time.Millisecond)
	base := mpv.TimePos()

	// 快进
	m = pressKey(t, m, "right")
	time.Sleep(300 * time.Millisecond)
	if got := mpv.TimePos(); got < base+4 {
		t.Errorf("按 → 后应前进约 5 秒，实际 %.2f（起点 %.2f）", got, base)
	}
	// 快退
	m = pressKey(t, m, "left")
	time.Sleep(300 * time.Millisecond)
	if got := mpv.TimePos(); got > base+1 {
		t.Errorf("按 ← 后应退回起点附近，实际 %.2f（起点 %.2f）", got, base)
	}
}

// TestIntegrationLoginPageRendersQR 端到端跑真 bubbletea 程序，验证登录层：
// 按 L → 请求二维码 → 从 PNG 解码 → 画成半块字符 → 停留在等待扫码。
//
// 用假后端 + 自己生成的二维码图片，所以不联网。
// 断言的还是「累积输出里有没有这些字符串」——bubbletea 是差分渲染，
// 想还原「当前屏幕」得写一个完整的终端模拟器，而这里要的只是几个布尔判断。
func TestIntegrationLoginPageRendersQR(t *testing.T) {
	qrPath := writeQRPNG(t)
	m := newModelWithQueue(nil, nil)
	m.api = fakeAPI(t, "FAKE_QR_PATH="+qrPath)
	m.autoLoad = false

	inR, inW := io.Pipe()
	defer inW.Close()
	out := &syncBuffer{}

	p := tea.NewProgram(m, tea.WithInput(inR), tea.WithOutput(out), tea.WithoutSignals())
	done := make(chan error, 1)
	go func() { _, err := p.Run(); done <- err }()

	time.Sleep(400 * time.Millisecond)
	p.Send(tea.WindowSizeMsg{Width: 120, Height: 35})
	time.Sleep(400 * time.Millisecond)

	p.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("L")})
	time.Sleep(1200 * time.Millisecond)

	screen := stripANSI(out.String())
	// "等待 QQ 扫码" 是假后端在 message 里回的原话，界面要原样透传。
	for _, want := range []string{"扫码登录", "等待 QQ 扫码", "关闭（不登录）"} {
		if !strings.Contains(screen, want) {
			t.Errorf("登录层应显示 %q", want)
		}
	}
	// 二维码必须真的画出来了。
	//
	// 这里只断言「有没有半块字符」，不断言配色：lipgloss 会按终端能力降采样，
	// go test 下没有 COLORTERM，真彩色会被压成 ANSI 色号，断言写死必然不稳。
	// 「必须用纯黑纯白」这条是 qr.go 里的显式决定，靠代码评审和真机扫码保证。
	if !strings.Contains(screen, "▀") {
		t.Error("登录层应画出二维码的半块字符")
	}
	if strings.Contains(screen, "二维码没法画在终端里") {
		t.Error("这么宽的终端不该走兜底提示")
	}

	p.Quit()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("程序没能正常退出")
	}
}

func pressKey(t *testing.T, m Model, key string) Model {
	t.Helper()
	var msg tea.KeyMsg
	switch key {
	case " ":
		msg = tea.KeyMsg{Type: tea.KeySpace}
	case "up":
		msg = tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		msg = tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		msg = tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		msg = tea.KeyMsg{Type: tea.KeyRight}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
	next, _ := m.Update(msg)
	nm, ok := next.(Model)
	if !ok {
		t.Fatalf("Update 应返回 Model，实际 %T", next)
	}
	return nm
}
