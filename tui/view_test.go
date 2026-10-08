package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func testModel() Model {
	return Model{
		queue:       demoSongs,
		curIndex:    0,
		volume:      70,
		w:           120,
		h:           35,
		pos:         30,
		dur:         180,
		page:        PageNow,
		focus:       FocusList,
		lyricFollow: true,
	}
}

// checkFits 确认渲染结果每行宽度都不超过终端宽度。
func checkFits(t *testing.T, view string, w int) {
	t.Helper()
	for i, line := range strings.Split(view, "\n") {
		if lw := lipgloss.Width(line); lw > w {
			t.Errorf("第 %d 行宽度 %d 超过终端宽度 %d: %q", i, lw, w, line)
		}
	}
}

func TestViewRenders(t *testing.T) {
	m := testModel()
	view := m.View()
	checkFits(t, view, m.w)

	// 页签栏 + 播放条是常驻的，页面上应该有当前曲目。
	for _, want := range []string{"QQ音乐 TUI", "探索", "播放队列", "Midnight Groove", "Nightowls", "▶"} {
		if !strings.Contains(view, want) {
			t.Errorf("界面应包含 %q\n%s", want, view)
		}
	}
}

// 探索页只显示接口返回的内容：以前末尾挂着一条写死的「离线演示」入口，现在没有了。
func TestExploreHasNoOfflineDemo(t *testing.T) {
	m := testModel()
	m.page = PageExplore
	view := m.View()
	checkFits(t, view, m.w)
	if strings.Contains(view, "离线演示") {
		t.Errorf("探索页不该再有离线演示入口\n%s", view)
	}
}

func TestViewLyricsFollowPlayback(t *testing.T) {
	m := testModel()
	m.pos = 0
	first := m.View()
	m.pos = 50 // 第 47 秒那行应被高亮
	later := m.View()
	if !strings.Contains(first, "智能音效已开启") {
		t.Error("开头应显示第一句歌词")
	}
	if !strings.Contains(later, "哪怕隔着千山万水") {
		t.Errorf("播放到 50 秒时应显示对应歌词\n%s", later)
	}
}

func TestViewHelpOverlay(t *testing.T) {
	m := testModel()
	m.focus = FocusHelp
	view := m.View()
	checkFits(t, view, m.w)
	if !strings.Contains(view, "按键") {
		t.Errorf("帮助面板应显示按键说明\n%s", view)
	}
	if strings.Contains(view, "智能音效已开启") {
		t.Errorf("帮助面板应盖住当前页面\n%s", view)
	}
}

// TestKeyRouting 是焦点状态机的核心断言：
// 同一个键在不同焦点下必须做相反的事。
func TestKeyRouting(t *testing.T) {
	// FocusSearch：q 进输入框，不是退出程序。
	m := testModel()
	m.page = PageSearch
	m.focus = FocusSearch
	next, cmd := m.Update(keyMsg("q"))
	nm := next.(Model)
	if nm.input.Value() != "q" {
		t.Errorf("搜索框里按 q 应输入字母 q，实际 %q", nm.input.Value())
	}
	if cmd != nil {
		if _, isQuit := cmd().(tea.QuitMsg); isQuit {
			t.Error("搜索框里按 q 不该退出程序")
		}
	}

	// 同一个框里，n / p / 空格也要进输入框，不能触发播放控制。
	for _, k := range []string{"n", "p", " "} {
		mm := testModel()
		mm.page = PageSearch
		mm.focus = FocusSearch
		gotModel, _ := mm.Update(keyMsg(k))
		got := gotModel.(Model)
		if !strings.Contains(got.input.Value(), strings.TrimSpace(k)) || got.input.Empty() {
			t.Errorf("搜索框里按 %q 应进输入框，实际内容 %q", k, got.input.Value())
		}
	}

	// FocusSearch：esc 退出输入，焦点回到列表。
	esc, _ := nm.Update(keyMsg("esc"))
	if got := esc.(Model); got.focus != FocusList {
		t.Errorf("esc 后焦点应回到列表，实际 %v", got.focus)
	}

	// FocusList：q 退出程序。
	lst := testModel()
	_, qcmd := lst.Update(keyMsg("q"))
	if qcmd == nil {
		t.Fatal("列表焦点下按 q 应返回退出命令")
	}
	if _, isQuit := qcmd().(tea.QuitMsg); !isQuit {
		t.Error("列表焦点下按 q 应退出程序")
	}

	// 数字键切页
	p3, _ := lst.Update(keyMsg("4"))
	if got := p3.(Model); got.page != PageFavorites {
		t.Errorf("按 4 应切到「我喜欢」，实际 %v", got.page)
	}

	// / 切到搜索页并聚焦输入框
	sl, _ := lst.Update(keyMsg("/"))
	got := sl.(Model)
	if got.page != PageSearch || got.focus != FocusSearch {
		t.Errorf("/ 应进搜索页并聚焦输入框，实际 page=%v focus=%v", got.page, got.focus)
	}
}

// TestListCursorSkipsSectionHeaders：游标不能停在分节标题上，
// 否则回车就成了哑键。
func TestListCursorSkipsSectionHeaders(t *testing.T) {
	m := testModel()
	m.page = PageExplore
	m.h = 40
	m.discover = fixtureDiscover()
	rows := m.rows()
	if len(rows) < 3 {
		t.Fatal("探索页应有分节标题 + 曲目")
	}
	if rows[0].kind != rowSection {
		t.Fatalf("第一行应是分节标题，实际 %v", rows[0].kind)
	}
	m.listToStart()
	if !rows[m.lists[PageExplore].cursor].selectable() {
		t.Error("g 之后游标应落在可选行上")
	}
	m.listToEnd()
	if !rows[m.lists[PageExplore].cursor].selectable() {
		t.Error("G 之后游标应落在可选行上")
	}
	for i := 0; i < len(rows)+2; i++ {
		m.moveList(1)
		if r := rows[m.lists[PageExplore].cursor]; !r.selectable() {
			t.Fatalf("第 %d 次下移后游标停在了不可选行 %v", i, r.kind)
		}
	}
}

// TestEnterPlaysSelectedTrack：探索页回车应把该页曲目换成队列并开播。
func TestEnterPlaysSelectedTrack(t *testing.T) {
	m := testModel()
	m.page = PageExplore
	m.discover = fixtureDiscover()
	m.queue = nil
	m.curIndex = -1
	m.listToStart()
	next, cmd := m.Update(keyMsg("enter"))
	got := next.(Model)
	if len(got.queue) == 0 {
		t.Fatal("回车之后队列不该是空的")
	}
	_ = cmd
	if got.curIndex < 0 || got.curIndex >= len(got.queue) {
		t.Errorf("回车后应停在合法索引上，实际 %d", got.curIndex)
	}
}

// TestViewVariousSizes 确认极端尺寸下不 panic、不溢出。
func TestViewVariousSizes(t *testing.T) {
	sizes := [][2]int{{60, 20}, {80, 24}, {200, 50}, {40, 15}, {24, 10}}
	for _, s := range sizes {
		m := testModel()
		m.w, m.h = s[0], s[1]
		view := m.View()
		checkFits(t, view, s[0])
		lines := strings.Split(view, "\n")
		if len(lines) > s[1] {
			t.Errorf("尺寸 %dx%d: 渲染 %d 行，超出高度", s[0], s[1], len(lines))
		}
	}
}
