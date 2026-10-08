package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// pressChain 按一次键，并把 Update 返回的后续命令也跟着跑完（最多几层）。
// 歌单写操作成功后会再发一条「重拉音乐库」，press 只跑一层看不到刷新后的列表。
func pressChain(t *testing.T, m Model, key string) Model {
	t.Helper()
	next, cmd := m.Update(keyMsg(key))
	m = next.(Model)
	for depth := 0; depth < 4 && cmd != nil; depth++ {
		msg := cmd()
		cmd = nil
		if msg == nil {
			break
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				m = runCmd(t, m, c)
			}
			break
		}
		next, cmd = m.Update(msg)
		m = next.(Model)
	}
	return m
}

// mineModel 造一个接着「有状态的假歌单后端」的模型，并停在「我的歌单」页、数据已加载。
func mineModel(t *testing.T) (Model, string) {
	t.Helper()
	plFile := filepath.Join(t.TempDir(), "pl.json")
	m := fakeModel(t, PageMine)
	m.api = fakeAPI(t, "FAKE_PL_FILE="+plFile)
	m.session = sessionView{Checked: true, LoggedIn: true, Nickname: "假用户"}
	m = runCmd(t, m, m.enterPage(PageMine))
	return m, plFile
}

type fakePLState struct {
	Created []Playlist          `json:"created"`
	Songs   map[string][]string `json:"songs"`
}

func readPL(t *testing.T, path string) fakePLState {
	t.Helper()
	var st fakePLState
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatal(err)
	}
	return st
}

// 页签：7 号键直达，列出自己创建的和收藏的歌单。
func TestMinePageListsPlaylists(t *testing.T) {
	m := fakeModel(t, PageExplore)
	m.api = fakeAPI(t, "FAKE_PL_FILE="+filepath.Join(t.TempDir(), "pl.json"))
	m = press(t, m, "7")
	if m.page != PageMine {
		t.Fatalf("按 7 应进「我的歌单」，实际 %v", m.page)
	}
	v := stripANSI(m.View())
	for _, want := range []string{"7 我的歌单", "我创建的歌单 · 2 个", "我喜欢", "自建一号", "我收藏的歌单", "假歌单"} {
		if !contains(v, want) {
			t.Errorf("应显示 %q:\n%s", want, v)
		}
	}
	// 原来的 1~6 不受影响
	m = press(t, m, "6")
	if m.page != PageQueue {
		t.Errorf("6 仍应是播放队列，实际 %v", m.page)
	}
}

// 新建：N → 输入名字（按键全进输入框）→ 回车 → 服务端多一个歌单、列表刷新。
func TestMineCreatePlaylist(t *testing.T) {
	m, plFile := mineModel(t)
	m = press(t, m, "N")
	if m.focus != FocusDialog {
		t.Fatalf("按 N 应弹出新建对话框，实际 focus=%v", m.focus)
	}
	for _, k := range []string{"q", "D", "j"} { // 浏览态是命令的键，这里必须是普通字符
		m = press(t, m, k)
	}
	if v := stripANSI(m.View()); !contains(v, "qDj") {
		t.Errorf("对话框里应看到输入的名字:\n%s", v)
	}
	m = pressChain(t, m, "enter")
	if m.focus != FocusList || !contains(m.status, "已新建歌单「qDj」") {
		t.Fatalf("创建后应关闭对话框并提示，实际 focus=%v status=%q", m.focus, m.status)
	}
	if st := readPL(t, plFile); st.Created[len(st.Created)-1].Title != "qDj" {
		t.Errorf("服务端应多出 qDj，实际 %+v", st.Created)
	}
	if v := stripANSI(m.View()); !contains(v, "我创建的歌单 · 3 个") || !contains(v, "qDj") {
		t.Errorf("列表应刷新出新歌单:\n%s", v)
	}

	// 重名：服务端报错，对话框和名字都留着
	m = press(t, m, "N")
	m.dialogInput.SetValue("qDj")
	m = pressChain(t, m, "enter")
	if m.focus != FocusDialog || m.dialogInput.Value() != "qDj" || !contains(m.status, "同名") {
		t.Errorf("重名应报错并保留输入，实际 focus=%v %q status=%q", m.focus, m.dialogInput.Value(), m.status)
	}
}

// 删除：D → 只有 y 才删；「我喜欢」和收藏来的歌单不能删。
func TestMineDeletePlaylist(t *testing.T) {
	m, plFile := mineModel(t)

	// 光标在第一个可选行 = 「我喜欢」
	m = press(t, m, "D")
	if m.focus == FocusDialog || !contains(m.status, "不能删除") {
		t.Errorf("「我喜欢」不该弹删除确认，实际 focus=%v status=%q", m.focus, m.status)
	}

	m = press(t, m, "j") // 自建一号
	m = press(t, m, "D")
	if m.focus != FocusDialog || !contains(stripANSI(m.View()), "确定删除「自建一号」") {
		t.Fatalf("应弹出删除确认:\n%s", stripANSI(m.View()))
	}
	m = press(t, m, "n") // 非 y 一律取消
	if m.focus != FocusList || len(readPLOrDefault(t, plFile).Created) != 2 {
		t.Fatal("按 y 以外的键应取消，不能删")
	}

	m = press(t, m, "D")
	m = pressChain(t, m, "y")
	if !contains(m.status, "已删除歌单「自建一号」") {
		t.Errorf("确认后应删除，实际 status=%q", m.status)
	}
	for _, p := range readPL(t, plFile).Created {
		if p.Title == "自建一号" {
			t.Error("服务端里应该已经没有这个歌单了")
		}
	}

	m = press(t, m, "j") // 移到收藏的歌单
	m = press(t, m, "j")
	if r, ok := m.selectedRow(); ok && r.pl.Title == "假歌单" {
		m = press(t, m, "D")
		if m.focus == FocusDialog {
			t.Error("收藏来的歌单不该能删")
		}
	}
}

// readPLOrDefault：还没写过状态文件时（没发生写操作）按初始的 2 个歌单算。
func readPLOrDefault(t *testing.T, path string) fakePLState {
	if _, err := os.Stat(path); err != nil {
		return fakePLState{Created: make([]Playlist, 2)}
	}
	return readPL(t, path)
}

// 加入歌单：任意页选中歌按 A → 选歌单 → 回车；加进「我喜欢」等于收藏。
func TestMineAddSongViaPicker(t *testing.T) {
	m, plFile := mineModel(t)
	m.page = PageSearch
	m.focus = FocusList
	m.searchResults = []Track{{ID: "1", SongMid: "0039MnYb0qxYhV", Title: "假歌一号", Playable: true}}
	m.reflow()

	m = press(t, m, "A")
	if m.focus != FocusPicker || !contains(stripANSI(m.View()), "把「假歌一号」加入歌单") {
		t.Fatalf("按 A 应弹出选歌单浮层:\n%s", stripANSI(m.View()))
	}
	m = press(t, m, "j") // 自建一号
	m = pressChain(t, m, "enter")
	if !contains(m.status, "已加入「自建一号」") {
		t.Errorf("应提示已加入，实际 %q", m.status)
	}
	if got := readPL(t, plFile).Songs["5"]; len(got) != 1 || got[0] != "0039MnYb0qxYhV" {
		t.Errorf("服务端「自建一号」里应有这首歌，实际 %v", got)
	}

	// 加进「我喜欢」→ ♥ 亮
	m = press(t, m, "A")
	m = pressChain(t, m, "enter")
	if !m.isFavorite("0039MnYb0qxYhV") {
		t.Error("加进「我喜欢」应等于收藏")
	}

	// 在浮层里按 N 新建，建完回到浮层
	m = press(t, m, "A")
	m = press(t, m, "N")
	m.dialogInput.SetValue("新歌单")
	m = pressChain(t, m, "enter")
	if m.focus != FocusPicker {
		t.Errorf("从浮层里新建的歌单建好后应回到浮层，实际 focus=%v", m.focus)
	}
	if !contains(stripANSI(m.View()), "新歌单") {
		t.Errorf("浮层里应出现刚建的歌单:\n%s", stripANSI(m.View()))
	}
	m = press(t, m, "esc")
	if m.focus != FocusList {
		t.Errorf("esc 应关闭浮层，实际 %v", m.focus)
	}
}

// 自己歌单的详情里按 d 把歌移出；别人的歌单里 d 不做任何事。
func TestMineRemoveSongFromOwnPlaylist(t *testing.T) {
	m, plFile := mineModel(t)
	m = press(t, m, "j")          // 自建一号
	m = pressChain(t, m, "enter") // 进详情（假后端回 1 首歌）
	if !m.detailActive() || m.detail == nil || len(m.detail.Tracks) != 1 {
		t.Fatalf("应打开歌单详情，实际 %+v", m.detail)
	}
	m = pressChain(t, m, "d")
	if !contains(m.status, "已从「自建一号」移除") {
		t.Errorf("应提示已移除，实际 %q", m.status)
	}
	if len(m.detail.Tracks) != 0 {
		t.Error("详情里应立刻去掉这首歌")
	}
	if _, err := os.Stat(plFile); err != nil {
		t.Error("应真的调用了服务端")
	}

	// 收藏来的别人的歌单：d 不动
	m = press(t, m, "esc")
	for i := 0; i < 5; i++ {
		m = press(t, m, "j")
	}
	if r, ok := m.selectedRow(); !ok || r.pl.Title != "假歌单" {
		t.Skip("光标没落到收藏的歌单上，跳过这半段")
	}
	m = pressChain(t, m, "enter")
	before := m.status
	next, cmd := m.Update(keyMsg("d"))
	if cmd != nil || next.(Model).status != before {
		t.Error("别人的歌单里按 d 不该有任何动作")
	}
}
