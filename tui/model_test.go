package main

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// TestEmptyQueueNoPanic 是接真实歌库之后最重要的一条回归：
// 以前曲目来自包里的常量切片，永远非空，所以到处直接写 m.songs[m.idx] 也没事。
// 现在队列空是常态（刚启动、搜索没结果、歌单加载失败……），
// publish.go 里那处索引会直接 panic 把整个 TUI 带走。
func TestEmptyQueueNoPanic(t *testing.T) {
	empty := func() Model {
		return Model{queue: nil, curIndex: -1, volume: 70, w: 100, h: 30}
	}

	// 渲染
	for _, size := range [][2]int{{120, 35}, {40, 15}, {10, 5}} {
		m := empty()
		m.w, m.h = size[0], size[1]
		if got := m.View(); got == "" {
			t.Errorf("尺寸 %dx%d 下应仍有输出", size[0], size[1])
		}
	}
	// 对外发布的状态
	if np := empty().nowPlaying(); np.Playing || np.Track.Title != "" {
		t.Errorf("空队列应发布「没在播」的状态，实际 playing=%v title=%q", np.Playing, np.Track.Title)
	}
	empty().publish()

	// 所有按键
	for _, key := range []string{" ", "n", "p", "left", "right", "up", "down", "m", "?", "q", "1", "/", "esc", "enter"} {
		m := empty()
		next, _ := m.Update(keyMsg(key))
		if _, ok := next.(Model); !ok {
			t.Fatalf("按键 %q 之后 Update 应仍返回 Model", key)
		}
	}

	// 走一遍 tick / 结束事件
	m := empty()
	for _, msg := range []tea.Msg{tickMsg(time.Now()), endedMsg{end: MpvEnd{Gen: 0}}} {
		next, _ := m.Update(msg)
		m = next.(Model)
	}

	// 队列非空但索引越界（删队列时容易短暂出现）
	m = Model{queue: demoSongs, curIndex: 99, volume: 70, w: 100, h: 30}
	if _, ok := m.current(); ok {
		t.Error("越界索引不该返回曲目")
	}
	_ = m.View()
}

// 代际对不上的结束通报必须被丢掉，否则「自然播完」的信号滞留在通道里，
// 会在用户手动切歌之后才被处理，白白多跳一首。
func TestEndedIgnoresStaleGeneration(t *testing.T) {
	audio := makeTestAudio(t)
	mpv, err := StartMpv(70, "--ao=null", "--no-config")
	if err != nil {
		t.Fatalf("启动 mpv 失败: %v", err)
	}
	defer mpv.Close()

	m := newModelWithQueue(mpv, []Track{
		{ID: "1", Title: "A", DirectURL: audio},
		{ID: "2", Title: "B", DirectURL: audio},
	})
	// 切到第二首：Load 过一次，代际已经比第一首时大。
	m.curIndex = 1
	_ = m.startCurrent()
	if m.curIndex != 1 {
		t.Fatalf("应停在第 2 首，实际 %d", m.curIndex)
	}
	if mpv.LoadSeq() < 2 {
		t.Fatalf("两次 Load 后代际应至少为 2，实际 %d", mpv.LoadSeq())
	}

	// 属于上一代的结束通报：应被丢弃，队列不动。
	next, _ := m.Update(endedMsg{end: MpvEnd{Gen: mpv.LoadSeq() - 1}})
	if nm := next.(Model); nm.curIndex != 1 {
		t.Errorf("过期的结束通报不该让队列前进，实际索引 %d", nm.curIndex)
	}

	// 当前代际的结束通报：正常切下一首（两首的队列回绕到第 1 首）。
	next2, _ := m.Update(endedMsg{end: MpvEnd{Gen: mpv.LoadSeq()}})
	if nm := next2.(Model); nm.curIndex != 0 {
		t.Errorf("当前代际的结束应切到下一首，实际索引 %d", nm.curIndex)
	}
}

// 播放失败的结束通报（直链过期）不能当成正常播完去切歌，
// 也不能一声不吭——以前 end-file 的 error 分支根本没人处理，
// 界面会永远停在「正在播放」而其实早就没声了。
func TestEndedWithErrorDoesNotAdvance(t *testing.T) {
	audio := makeTestAudio(t)
	mpv, err := StartMpv(70, "--ao=null", "--no-config")
	if err != nil {
		t.Fatalf("启动 mpv 失败: %v", err)
	}
	defer mpv.Close()

	m := newModelWithQueue(mpv, []Track{
		{ID: "1", Title: "A", DirectURL: audio},
		{ID: "2", Title: "B", DirectURL: audio},
	})
	next, _ := m.Update(endedMsg{end: MpvEnd{Gen: mpv.LoadSeq(), Err: "Failed to open https://x"}})
	nm := next.(Model)
	if nm.curIndex != 0 {
		t.Errorf("播出错不该自动切歌，实际索引 %d", nm.curIndex)
	}
	if !nm.paused {
		t.Error("播出错后应停在暂停态")
	}
	if nm.status == "" {
		t.Error("播出错后应给出提示")
	}
}

func keyMsg(key string) tea.KeyMsg {
	switch key {
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
}

// TestLyricTiming 钉住歌词「不比声音慢」的三件事：
// 两次 tick 之间按墙钟外推、提前 lyricLead 亮起、换行时刻落在本周期内就补一次重绘。
func TestLyricTiming(t *testing.T) {
	lyrics := []Lyric{{Time: 0, Text: "一"}, {Time: 10, Text: "二"}, {Time: 20, Text: "三"}}
	m := Model{
		queue:    []Track{{Title: "t", Lyrics: lyrics}},
		curIndex: 0,
		dur:      30,
		mpv:      &Mpv{}, // 只要非 nil：displayPos 不碰 mpv 本身
	}

	// 采样时在 9.80s，过了 100ms：外推到 9.90，再提前 0.12 → 已越过 10s 那一行。
	m.posWall, m.pos = 9.80, 9.80
	m.posAt = time.Now().Add(-100 * time.Millisecond)
	if got := m.currentLyricIndex(lyrics); got != 1 {
		t.Errorf("外推 + 提前量之后应已切到第 2 行，实际 %d", got)
	}

	// 暂停时不外推，也不提前到越过一整行。
	m.paused = true
	m.posWall, m.pos = 9.0, 9.0
	if got := m.currentLyricIndex(lyrics); got != 0 {
		t.Errorf("暂停在 9s 应停在第 1 行，实际 %d", got)
	}

	// 外推量封顶：mpv 在缓冲时 time-pos 不动，界面不能自己一路往前跑。
	m.paused = false
	m.posWall, m.pos = 5.0, 5.0
	m.posAt = time.Now().Add(-10 * time.Second)
	if got := m.displayPos(); got > 5.0+2*tickInterval.Seconds()+0.01 {
		t.Errorf("外推量应封顶在两个 tick 周期内，实际 %.2f", got)
	}

	// 下一行 80ms 后就到 → 要有补一次重绘的闹钟；还早 → 交给普通 tick。
	m.posAt = time.Now()
	m.posWall = 10 - lyricLead - 0.08
	if m.lyricWakeCmd() == nil {
		t.Error("换行时刻落在本周期内时应安排一次精确唤醒")
	}
	m.posWall = 12
	if m.lyricWakeCmd() != nil {
		t.Error("下一行还远时不应额外唤醒")
	}
}
