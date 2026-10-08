package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// 这一组测试把 Update 循环接上假后端跑通：按键 → tea.Cmd → 子进程 → 消息 → 界面数据。
// 全程不联网，所以能在 CI 里跑。真网的手工验收在 README 里另列。

// fakeModel 造一个「像真实运行、但接口指向假后端」的模型。
// 刻意不走 NewModel：那条路会在 Init 里发 autoLoadMsg。
func fakeModel(t *testing.T, page Page) Model {
	t.Helper()
	m := newModelWithQueue(nil, nil)
	m.api = fakeAPI(t)
	m.baseCtx = context.Background()
	m.w, m.h = 120, 35
	m.page = page
	m.focus = FocusList
	m.lists[page] = listState{}
	return m
}

// runCmd 执行一条 tea.Cmd 并把它产出的消息喂回 Update。
// tea.Batch 会返回 BatchMsg，这里递归展开。
func runCmd(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		return m
	}
	msg := cmd()
	if msg == nil {
		return m
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			m = runCmd(t, m, c)
		}
		return m
	}
	next, _ := m.Update(msg)
	nm, ok := next.(Model)
	if !ok {
		t.Fatalf("Update 应返回 Model，实际 %T", next)
	}
	return nm
}

// press 走一次完整的按键 → 命令 → 消息回路。
func press(t *testing.T, m Model, key string) Model {
	t.Helper()
	next, cmd := m.Update(keyMsg(key))
	nm, ok := next.(Model)
	if !ok {
		t.Fatalf("Update 应返回 Model，实际 %T", next)
	}
	return runCmd(t, nm, cmd)
}

// 搜索页端到端：输入 → 回车 → 结果进列表 → 光标落在可选行上。
func TestSearchFlowEndToEnd(t *testing.T) {
	m := fakeModel(t, PageSearch)
	m.focus = FocusSearch
	m.input.SetValue("周杰伦")

	m = press(t, m, "enter")

	if m.searchLoading {
		t.Error("结果回来后不该还挂着「搜索中」")
	}
	if m.searchErr != nil {
		t.Fatalf("搜索不该失败: %v", m.searchErr)
	}
	if len(m.searchResults) != 1 || m.searchResults[0].Title != "假歌一号" {
		t.Fatalf("搜索结果不对: %+v", m.searchResults)
	}
	if m.searchQuery != "周杰伦" {
		t.Errorf("searchQuery 应为输入的词，实际 %q", m.searchQuery)
	}

	// 光标必须落在可选行上，否则回车是哑键。
	rows := m.rows()
	if len(rows) == 0 {
		t.Fatal("搜索页应有行")
	}
	r := rows[m.lists[PageSearch].cursor]
	if !r.selectable() {
		t.Errorf("回车后光标应落在可选行上，实际 %v", r.kind)
	}
	if r.kind != rowTrack || r.track.SongMid != "0039MnYb0qxYhV" {
		t.Errorf("光标应落在搜到的曲目上，实际 %+v", r)
	}
}

// 过期响应：连搜两次，先喂新请求的令牌再喂旧的，旧结果必须被丢弃。
func TestSearchDropsStaleResponse(t *testing.T) {
	m := fakeModel(t, PageSearch)
	m.focus = FocusSearch

	m.input.SetValue("第一次")
	next, _ := m.Update(keyMsg("enter"))
	m = next.(Model)
	staleReq := m.searchReq

	m.input.SetValue("第二次")
	next, _ = m.Update(keyMsg("enter"))
	m = next.(Model)
	if m.searchReq == staleReq {
		t.Fatal("第二次搜索应有新的请求号码")
	}

	// 旧号码带着「第一次」的结果回来：必须被丢掉，不能污染当前列表。
	next, _ = m.Update(searchMsg{
		req:   staleReq,
		query: "第一次",
		res:   &SearchResult{Query: "第一次", Tracks: []Track{{SongMid: "old", Title: "旧结果"}}},
	})
	m = next.(Model)
	if len(m.searchResults) != 0 {
		t.Errorf("过期结果应被丢弃，实际 %+v", m.searchResults)
	}
	if m.searchQuery != "第二次" {
		t.Errorf("searchQuery 不该被过期结果改掉，实际 %q", m.searchQuery)
	}
}

// 搜索失败：错误原样透传，列表清空，不 panic。
func TestSearchErrorSurfaces(t *testing.T) {
	m := fakeModel(t, PageSearch)
	m.api = fakeAPI(t, "FAKE_ERROR=测试用的业务错误")
	m.focus = FocusSearch
	m.input.SetValue("随便")

	next, cmd := m.Update(keyMsg("enter"))
	m = next.(Model)
	m = runCmd(t, m, cmd)

	if m.searchErr == nil {
		t.Fatal("假后端报错时应该有错误")
	}
	if m.searchLoading {
		t.Error("失败之后不该还挂着「搜索中」")
	}
	// 错误文案必须原样来自脚本，不自造。
	if got := m.searchErr.Error(); got != "测试用的业务错误" {
		t.Errorf("错误文案应原样透传，实际 %q", got)
	}
	_ = m.View() // 错误态也要能渲染
}

// 探索页：假后端的 discover 返回后，接口曲目出现在列表里。
func TestExploreFlowEndToEnd(t *testing.T) {
	m := fakeModel(t, PageExplore)

	cmd := m.cmdDiscover()
	if cmd == nil {
		t.Fatal("cmdDiscover 应返回命令")
	}
	m = runCmd(t, m, cmd)

	if m.discoverErr != nil {
		t.Fatalf("discover 不该失败: %v", m.discoverErr)
	}
	if m.discover == nil || len(m.discover.GuessTracks) != 1 {
		t.Fatalf("discover 结果不对: %+v", m.discover)
	}

	var haveFake bool
	for _, r := range m.rows() {
		if r.kind != rowTrack {
			continue
		}
		if r.track.Title == "假歌一号" {
			haveFake = true
		}
	}
	if !haveFake {
		t.Error("探索页应包含接口返回的曲目")
	}
}

// 音乐库 → 歌单详情 → esc 返回，三层状态都要正确。
func TestLibraryPlaylistDrillDown(t *testing.T) {
	m := fakeModel(t, PageLibrary)

	m = runCmd(t, m, m.cmdLibrary())
	if m.libraryErr != nil {
		t.Fatalf("library 不该失败: %v", m.libraryErr)
	}
	if m.library == nil || len(m.library.CollectedPlaylists) != 1 {
		t.Fatalf("音乐库数据不对: %+v", m.library)
	}

	// 把光标移到歌单行上再进详情。
	rows := m.rows()
	plRow := -1
	for i, r := range rows {
		if r.kind == rowPlaylist {
			plRow = i
			break
		}
	}
	if plRow < 0 {
		t.Fatal("音乐库里应有一个歌单行")
	}
	m.lists[PageLibrary].cursor = plRow

	m = press(t, m, "enter")
	if m.detailErr != nil {
		t.Fatalf("歌单详情不该失败: %v", m.detailErr)
	}
	if m.detail == nil || m.detail.Playlist.ID != "9001" {
		t.Fatalf("歌单详情没加载出来: %+v", m.detail)
	}

	// 歌单详情的曲目要出现在行里。
	var haveTrack bool
	for _, r := range m.rows() {
		if r.kind == rowTrack && r.track.Title == "假歌一号" {
			haveTrack = true
		}
	}
	if !haveTrack {
		t.Error("歌单详情里应有曲目")
	}

	m = press(t, m, "esc")
	if m.detail != nil {
		t.Error("esc 之后应退回歌单列表")
	}
	if m.detailActive() {
		t.Error("esc 之后不该还停在这一层详情上")
	}
	if len(m.rows()) == 0 {
		t.Error("退回之后应看到音乐库列表")
	}
}

// 会话状态落地到顶栏。
func TestSessionFlowUpdatesTabBar(t *testing.T) {
	m := fakeModel(t, PageExplore)
	m = runCmd(t, m, m.cmdSession())

	if !m.session.Checked {
		t.Error("查过一次之后 Checked 应为 true")
	}
	if !m.session.LoggedIn || m.session.Nickname != "假用户" {
		t.Errorf("登录信息不对: %+v", m.session)
	}
	if view := m.View(); !contains(view, "假用户") {
		t.Error("顶栏应显示昵称")
	}
}

// 队列编辑：加一首、删一首、清空。
func TestQueueEditing(t *testing.T) {
	m := fakeModel(t, PageExplore)
	// 探索页只显示接口返回的内容，先把它拉起来，否则列表是空的、没得选。
	m = runCmd(t, m, m.cmdDiscover())
	m.listToStart()
	// 落到第一首曲目上
	for i := 0; i < len(m.rows()); i++ {
		if r, ok := m.selectedRow(); ok && r.kind == rowTrack {
			break
		}
		m.moveList(1)
	}
	sel, ok := m.selectedRow()
	if !ok || sel.kind != rowTrack {
		t.Fatal("应能选中一首曲目")
	}
	wantTitle := sel.track.Title

	m = press(t, m, "a")
	if len(m.queue) != 1 || m.queue[0].Title != wantTitle {
		t.Fatalf("按 a 应把选中的歌加进队列，实际 %+v", m.queue)
	}

	// 队列页删掉它
	m.page = PageQueue
	m.lists[PageQueue] = listState{}
	m.listToStart()
	m = press(t, m, "d")
	if len(m.queue) != 0 {
		t.Errorf("按 d 应删掉选中的队列项，实际 %d 首", len(m.queue))
	}
	if m.curIndex != -1 {
		t.Errorf("队列空了之后 curIndex 应为 -1，实际 %d", m.curIndex)
	}
}

// ---------- 搜索类型 / 翻页 ----------

// Ctrl+T 循环切换类型，每换一次都按新类型重搜，结果整批换掉。
func TestSearchTypeCycling(t *testing.T) {
	m := fakeModel(t, PageSearch)
	m.focus = FocusSearch
	m.input.SetValue("假歌手")
	m = press(t, m, "enter")
	if m.searchType != searchTypeSong {
		t.Fatalf("默认类型应是 song，实际 %q", m.searchType)
	}

	// song → singer
	m = press(t, m, "ctrl+t")
	if m.searchType != searchTypeSinger {
		t.Fatalf("ctrl+t 之后应是 singer，实际 %q", m.searchType)
	}
	if len(m.searchSingers) != 1 || m.searchSingers[0].Name != "假歌手" {
		t.Fatalf("歌手结果不对: %+v", m.searchSingers)
	}
	// 换类型必须把上一类的结果清掉，否则界面上会留着看起来没反应。
	if len(m.searchResults) != 0 {
		t.Errorf("换类型后旧的曲目结果应清空，实际 %d 条", len(m.searchResults))
	}
	if r := m.rows()[m.lists[PageSearch].cursor]; r.kind != rowSinger {
		t.Errorf("歌手类型下光标应落在歌手行上，实际 %v", r.kind)
	}

	// singer → album → songlist
	m = press(t, m, "ctrl+t")
	if m.searchType != searchTypeAlbum || len(m.searchAlbums) != 1 {
		t.Fatalf("专辑类型结果不对: type=%q albums=%+v", m.searchType, m.searchAlbums)
	}
	if m.searchAlbums[0].Kind != "album" {
		t.Errorf("专辑结果的 kind 应为 album，实际 %q", m.searchAlbums[0].Kind)
	}
	m = press(t, m, "ctrl+t")
	if m.searchType != searchTypePlaylist || len(m.searchPlaylists) != 1 {
		t.Fatalf("歌单类型结果不对: type=%q playlists=%+v", m.searchType, m.searchPlaylists)
	}
	// songlist → song，回到起点
	m = press(t, m, "ctrl+t")
	if m.searchType != searchTypeSong {
		t.Errorf("循环一圈之后应回到 song，实际 %q", m.searchType)
	}
	_ = m.View()
}

// 歌手回车没有歌曲列表接口（要签名），退化成「按歌手名搜他的歌」。
func TestSingerEnterFallsBackToNameSearch(t *testing.T) {
	m := fakeModel(t, PageSearch)
	m.focus = FocusSearch
	m.input.SetValue("假歌手")
	m = press(t, m, "enter") // 先搜一次，searchQuery 才有值，切类型才会重搜
	m = press(t, m, "ctrl+t")
	if m.searchType != searchTypeSinger {
		t.Fatalf("应停在歌手类型上，实际 %q", m.searchType)
	}

	// 搜索框里回车是「再搜一次」，所以先 esc 退出输入，回车才是「选中这一行」。
	m = press(t, m, "esc")
	m.listToStart() // 第 0 行是分节标题，可选行从下一行开始
	m = press(t, m, "enter")

	if m.searchType != searchTypeSong {
		t.Errorf("回车后应切成歌曲搜索，实际 %q", m.searchType)
	}
	if m.input.Value() != "假歌手" {
		t.Errorf("输入框应被填成歌手名，实际 %q", m.input.Value())
	}
	if m.searchQuery != "假歌手" {
		t.Errorf("应该发起了一次按歌手名的搜索，实际 %q", m.searchQuery)
	}
	if len(m.searchResults) != 1 {
		t.Errorf("应拿到歌曲结果，实际 %+v", m.searchResults)
	}
}

// 搜索结果里的专辑回车进详情，走的是 album 子命令；esc 退回搜索列表。
func TestAlbumDrillDownFromSearch(t *testing.T) {
	m := fakeModel(t, PageSearch)
	m.focus = FocusSearch
	m.input.SetValue("假专辑")
	m = press(t, m, "enter")
	m = press(t, m, "ctrl+t")
	m = press(t, m, "ctrl+t") // song → singer → album
	if m.searchType != searchTypeAlbum {
		t.Fatalf("应停在专辑类型上，实际 %q", m.searchType)
	}

	// 同歌手那条：先退出输入框，回车才是进详情。
	m = press(t, m, "esc")
	m.listToStart() // 第 0 行是分节标题，可选行从下一行开始
	m = press(t, m, "enter")

	if m.detailErr != nil {
		t.Fatalf("专辑详情不该失败: %v", m.detailErr)
	}
	if m.detail == nil || m.detail.Playlist.Kind != "album" {
		t.Fatalf("应加载出专辑详情，实际 %+v", m.detail)
	}
	if m.detailFrom != PageSearch {
		t.Errorf("detailFrom 应记住是搜索页进来的，实际 %v", m.detailFrom)
	}
	// 详情盖在搜索页上，所以 rows() 现在给的是详情的内容。
	var haveTrack bool
	for _, r := range m.rows() {
		if r.kind == rowTrack && r.track.Title == "假歌一号" {
			haveTrack = true
		}
	}
	if !haveTrack {
		t.Error("专辑详情里应有曲目")
	}

	m = press(t, m, "esc")
	if m.detailActive() {
		t.Error("esc 之后详情层应关掉")
	}
	if len(m.rows()) == 0 {
		t.Error("退回之后应看到搜索结果")
	}
}

// 翻页：第二页要追加到第一页后面，不是替换。
func TestSearchPaging(t *testing.T) {
	m := fakeModel(t, PageSearch)
	m.api = fakeAPI(t, "FAKE_PAGED=1")
	m.focus = FocusSearch
	m.input.SetValue("分页")
	m = press(t, m, "enter")

	if len(m.searchResults) != 30 {
		t.Fatalf("第一页应有 30 条，实际 %d", len(m.searchResults))
	}
	if !m.searchHasMore {
		t.Fatal("45 条里只拿了 30 条，应当还有下一页")
	}
	first := m.searchResults[0].Title

	m = press(t, m, "ctrl+f")
	if m.searchLoadingMore {
		t.Error("结果回来后不该还挂着「加载下一页」")
	}
	if len(m.searchResults) != 45 {
		t.Fatalf("两页加起来应有 45 条，实际 %d", len(m.searchResults))
	}
	if m.searchResults[0].Title != first {
		t.Errorf("追加不该动第一页的内容，实际首条 %q", m.searchResults[0].Title)
	}
	if m.searchHasMore {
		t.Error("45 条都拿到了，不该还说有下一页")
	}
	if m.searchPage != 2 {
		t.Errorf("页码应停在 2，实际 %d", m.searchPage)
	}

	// 往回翻是整批替换，不是往回拼接。
	m = press(t, m, "ctrl+b")
	if len(m.searchResults) != 30 {
		t.Errorf("往回翻应整批替换成 30 条，实际 %d", len(m.searchResults))
	}
	if m.searchPage != 1 {
		t.Errorf("往回翻后页码应为 1，实际 %d", m.searchPage)
	}
}

// ---------- 评论 ----------

func TestCommentsOverlay(t *testing.T) {
	m := fakeModel(t, PageNow)
	m.queue = demoSongs
	m.curIndex = 0

	next, cmd := m.Update(keyMsg("c"))
	m = next.(Model)
	if m.focus != FocusComments {
		t.Fatalf("按 c 应打开评论浮层，实际 focus=%v", m.focus)
	}
	m = runCmd(t, m, cmd)

	if m.commentsErr != nil {
		t.Fatalf("评论不该失败: %v", m.commentsErr)
	}
	if len(m.comments) != 3 || m.commentsTotal != 7 {
		t.Fatalf("评论数据不对: %d 条 / 共 %d", len(m.comments), m.commentsTotal)
	}
	if m.commentsSongID != demoSongs[0].ID {
		t.Errorf("评论应对着当前曲目，实际 %q", m.commentsSongID)
	}
	if view := m.View(); !contains(view, "听众 1") {
		t.Errorf("评论浮层应显示评论内容\n%s", view)
	}

	// 翻页：整批替换。
	m = press(t, m, "]")
	if m.commentsPage != 2 {
		t.Errorf("按 ] 应翻到第 2 页，实际 %d", m.commentsPage)
	}
	if len(m.comments) != 3 || m.comments[0].Text != "第 2 页的第 1 条评论" {
		t.Errorf("第 2 页的评论内容不对: %+v", m.comments[0])
	}

	// esc 关闭浮层，回到列表焦点。
	m = press(t, m, "esc")
	if m.focus != FocusList {
		t.Errorf("esc 应关掉评论浮层，实际 focus=%v", m.focus)
	}
}

// 最新评论只能按游标翻：往后翻带上一页末条的 seqNo，往回翻复用当初那一页的游标。
func TestCommentsSortAndCursorPaging(t *testing.T) {
	m := fakeModel(t, PageNow)
	m.queue = demoSongs
	m.curIndex = 0
	next, cmd := m.Update(keyMsg("c"))
	m = runCmd(t, next.(Model), cmd)

	m = press(t, m, "s")
	if m.commentsSortOrHot() != "new" || m.commentsPage != 1 {
		t.Fatalf("按 s 应切到「最新」第 1 页，实际 %s 第 %d 页", m.commentsSort, m.commentsPage)
	}
	if !contains(m.comments[0].Text, "最新") || m.comments[0].AvatarURL != "cursor:" {
		t.Errorf("第 1 页应按最新排序、不带游标: %+v", m.comments[0])
	}
	if v := stripANSI(m.View()); !contains(v, "[最新]") {
		t.Errorf("浮层应标出当前是「最新」:\n%s", v)
	}

	lastSeq := m.comments[len(m.comments)-1].SeqNo
	m = press(t, m, "]")
	if m.commentsPage != 2 || m.comments[0].AvatarURL != "cursor:"+lastSeq {
		t.Errorf("第 2 页应带第 1 页末条的游标 %q，实际页 %d %q", lastSeq, m.commentsPage, m.comments[0].AvatarURL)
	}
	page2Seq := m.comments[len(m.comments)-1].SeqNo
	m = press(t, m, "]")
	if m.comments[0].AvatarURL != "cursor:"+page2Seq {
		t.Errorf("第 3 页应带第 2 页末条的游标，实际 %q", m.comments[0].AvatarURL)
	}
	m = press(t, m, "[")
	if m.commentsPage != 2 || m.comments[0].AvatarURL != "cursor:"+lastSeq {
		t.Errorf("往回翻到第 2 页应复用当初的游标 %q，实际页 %d %q", lastSeq, m.commentsPage, m.comments[0].AvatarURL)
	}

	m = press(t, m, "s")
	if m.commentsSortOrHot() != "hot" || contains(m.comments[0].Text, "最新") {
		t.Errorf("再按 s 应回到热评: %+v", m.comments[0])
	}
}

// 点赞：乐观更新，成功保持；失败回滚。
func TestCommentPraise(t *testing.T) {
	m := fakeModel(t, PageNow)
	m.queue = demoSongs
	m.curIndex = 0
	next, cmd := m.Update(keyMsg("c"))
	m = runCmd(t, next.(Model), cmd)

	// 第 1 条（没赞过、1 赞）→ 点赞
	m = press(t, m, "l")
	if c := m.comments[0]; !c.IsPraised || c.Likes != 2 || !contains(m.status, "已点赞") {
		t.Errorf("点赞后应为已赞、2 赞，实际 %+v status=%q", c, m.status)
	}
	if v := stripANSI(m.View()); !contains(v, "已赞") {
		t.Errorf("已赞的评论应有标记:\n%s", v)
	}
	// 再按一次取消
	m = press(t, m, "l")
	if c := m.comments[0]; c.IsPraised || c.Likes != 1 {
		t.Errorf("取消点赞后应回到 1 赞未赞，实际 %+v", c)
	}

	// 服务端拒绝 → 回滚
	m.api = fakeAPI(t, "FAKE_PRAISE_ERROR=点赞太频繁")
	m = press(t, m, "l")
	if c := m.comments[0]; c.IsPraised || c.Likes != 1 {
		t.Errorf("失败应回滚，实际 %+v", c)
	}
	if !contains(m.status, "点赞太频繁") {
		t.Errorf("状态栏应透传服务端错误，实际 %q", m.status)
	}

	// 未登录不发请求
	m.session = sessionView{Checked: true}
	next, cmd = m.Update(keyMsg("l"))
	if cmd != nil || !contains(next.(Model).status, "登录") {
		t.Error("未登录点赞应提示登录且不发请求")
	}
}

// 写评论：w 打开输入行，按键进输入框（q 不会退出），回车发送。
func TestCommentWrite(t *testing.T) {
	m := fakeModel(t, PageNow)
	m.queue = demoSongs
	m.curIndex = 0
	next, cmd := m.Update(keyMsg("c"))
	m = runCmd(t, next.(Model), cmd)

	m = press(t, m, "w")
	if !m.commentWriting {
		t.Fatal("按 w 应进入写评论")
	}
	for _, k := range []string{"q", "s", "l"} { // 这些在浏览态是命令，写评论时必须是普通字符
		m = press(t, m, k)
	}
	if m.commentInput.Value() != "qsl" || m.focus != FocusComments {
		t.Fatalf("写评论时按键应进输入框，实际 %q focus=%v", m.commentInput.Value(), m.focus)
	}
	// 输入的字必须真的出现在画面上（以前输入框被挤出浮层底部，打了字看不见）。
	if v := stripANSI(m.View()); !contains(v, "写评论 · 回车发送") || !contains(v, "qsl") {
		t.Errorf("应显示输入框和已输入的文字:\n%s", v)
	}

	m = press(t, m, "enter")
	if m.commentWriting || m.commentInput.Value() != "" || !contains(m.status, "评论已发送") {
		t.Errorf("发送成功后应关闭输入行并清空，实际 writing=%v %q status=%q", m.commentWriting, m.commentInput.Value(), m.status)
	}
	if m.commentsSortOrHot() != "new" {
		t.Error("发送成功后应切到「最新」，刚发的那条在最上面")
	}

	// 待审核
	m.api = fakeAPI(t, "FAKE_COMMENT_PENDING=1")
	m = press(t, m, "w")
	m.commentInput.SetValue("再来一条")
	m = press(t, m, "enter")
	if !contains(m.status, "等待审核") {
		t.Errorf("待审核应如实提示，实际 %q", m.status)
	}

	// 发送失败：草稿保留
	m.api = fakeAPI(t, "FAKE_ERROR=内容违规")
	m = press(t, m, "w")
	m.commentInput.SetValue("留着改")
	m = press(t, m, "enter")
	if !m.commentWriting || m.commentInput.Value() != "留着改" || !contains(m.status, "内容违规") {
		t.Errorf("失败时草稿应保留，实际 writing=%v %q status=%q", m.commentWriting, m.commentInput.Value(), m.status)
	}
}

// 长评论要完整显示（自动换行，不截断），而且整屏高度不变、底部提示不被挤掉。
func TestCommentsLongTextFullyVisible(t *testing.T) {
	m := fakeModel(t, PageNow)
	m.queue = demoSongs
	m.curIndex = 0
	m.focus = FocusComments
	long := strings.Repeat("这是一条很长的评论，", 30) + "结尾在这里"
	m.comments = []Comment{
		{ID: "a", Author: "甲", Text: long, Likes: 3},
		{ID: "b", Author: "乙", Text: "第二条\n还有第二行"},
	}
	m.commentsTotal = 2

	v := stripANSI(m.View())
	if !contains(v, "结尾在这里") {
		t.Errorf("长评论的结尾应该能看到（以前被截成一行）:\n%s", v)
	}
	if !contains(v, "还有第二行") {
		t.Errorf("评论里的换行应保留:\n%s", v)
	}
	if !contains(v, "w 写评论") {
		t.Errorf("底部提示不该被挤掉:\n%s", v)
	}
	normal := m
	normal.focus = FocusList
	if got, want := len(strings.Split(m.View(), "\n")), len(strings.Split(normal.View(), "\n")); got != want {
		t.Errorf("评论浮层不该把整屏撑高：普通页面 %d 行，浮层 %d 行", want, got)
	}

	// 选中最后一条时它必须在画面里，哪怕前面的长评论占满了一屏。
	m.comments = nil
	for i := 0; i < 12; i++ {
		m.comments = append(m.comments, Comment{ID: fmt.Sprint(i), Author: fmt.Sprint("作者", i), Text: long})
	}
	m.commentsList.cursor = len(m.comments) - 1
	if v := stripANSI(m.View()); !contains(v, "作者11") {
		t.Errorf("选中的最后一条应可见:\n%s", v)
	}
}

// 音乐库：自己的歌单排在「最近播放」前面，否则上百首最近播放会把歌单压到底下。
func TestLibraryPlaylistsFirst(t *testing.T) {
	m := fakeModel(t, PageLibrary)
	recent := make([]Track, 120)
	for i := range recent {
		recent[i] = Track{Title: fmt.Sprint("最近", i), SongMid: fmt.Sprint("m", i)}
	}
	m.library = &LibraryResult{
		RecentTracks:     recent,
		CreatedPlaylists: []Playlist{{ID: "1", Title: "我喜欢"}, {ID: "2", Title: "纯音"}},
	}
	m.reflow()
	rows := m.libraryRows()
	if rows[0].text != "创建的歌单" || rows[1].pl.Title != "我喜欢" {
		t.Fatalf("第一段应是创建的歌单，实际 %+v", rows[0])
	}
	if v := stripANSI(m.View()); !contains(v, "纯音") {
		t.Errorf("首屏应能看到自己的歌单:\n%s", v)
	}
}

// 没有在播放的歌时按 c 只提示，不进浮层。
func TestCommentsWithoutTrack(t *testing.T) {
	// 用探索页：队列页的 c 是「清空队列」，不是看评论。
	m := fakeModel(t, PageExplore)
	next, cmd := m.Update(keyMsg("c"))
	m = next.(Model)
	if m.focus == FocusComments {
		t.Error("没有曲目时不该打开评论浮层")
	}
	if cmd != nil {
		t.Error("没有曲目时不该发请求")
	}
	if m.status == "" {
		t.Error("应该说清楚为什么打不开")
	}
}

func contains(haystack, needle string) bool {
	return len(needle) == 0 || indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

// 收藏端到端：按 f → 子进程 → 消息 → 列表与状态栏都更新；再按一次反向。
func TestFavoriteToggleEndToEnd(t *testing.T) {
	m := fakeModel(t, PageSearch)
	m.searchResults = []Track{{
		ID: "1001", SongMid: "0039MnYb0qxYhV", Title: "假歌一号", Playable: true,
	}}
	m.reflow()

	if m.isFavorite("0039MnYb0qxYhV") {
		t.Fatal("还没收藏，不该是已收藏状态")
	}

	m = press(t, m, "f")

	if m.favoriteBusy {
		t.Error("结果回来之后不该还挂着「提交中」")
	}
	if m.status == "" || !contains(m.status, "已加入我喜欢") {
		t.Errorf("状态栏应说明已收藏，实际 %q", m.status)
	}
	if !m.isFavorite("0039MnYb0qxYhV") {
		t.Error("收藏成功后本地列表里应该有这首歌")
	}
	if m.favoritesTotal != 1 {
		t.Errorf("收藏总数应为 1，实际 %d", m.favoritesTotal)
	}

	m = press(t, m, "f")

	if m.isFavorite("0039MnYb0qxYhV") {
		t.Error("再按一次应该取消收藏")
	}
	if m.favoritesTotal != 0 {
		t.Errorf("取消后总数应为 0，实际 %d", m.favoritesTotal)
	}
	if !contains(m.status, "已从我喜欢移除") {
		t.Errorf("状态栏应说明已移除，实际 %q", m.status)
	}
}

// 本次修复的核心回归：没进过「我喜欢」页（本地列表是空的）时，
// 对一首已经收藏的歌按 f 必须是「取消收藏」，而不是再「加」一次。
// 方向以服务端实时状态为准。
func TestFavoriteToggleUsesServerState(t *testing.T) {
	favFile := filepath.Join(t.TempDir(), "fav")
	if err := os.WriteFile(favFile, []byte("0039MnYb0qxYhV\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := fakeModel(t, PageSearch)
	m.api = fakeAPI(t, "FAKE_FAV_FILE="+favFile)
	m.searchResults = []Track{{ID: "1001", SongMid: "0039MnYb0qxYhV", Title: "假歌一号", Playable: true}}
	m.reflow()
	if m.favorites != nil || m.isFavorite("0039MnYb0qxYhV") {
		t.Fatal("前提：本地还不知道这首歌已收藏")
	}

	m = press(t, m, "f")

	if !contains(m.status, "已从我喜欢移除") {
		t.Errorf("服务端说已收藏，按 f 应取消收藏，实际 %q", m.status)
	}
	if data, _ := os.ReadFile(favFile); strings.Contains(string(data), "0039MnYb0qxYhV") {
		t.Error("「服务端」里这首歌应该已被移除")
	}
	if m.isFavorite("0039MnYb0qxYhV") {
		t.Error("本地也应显示为未收藏")
	}
	if m.favorites != nil {
		t.Error("没加载过的「我喜欢」列表不该被凭空塞出一条（否则进页面时不会去拉真实列表）")
	}

	m = press(t, m, "f")
	if !contains(m.status, "已加入我喜欢") || !m.isFavorite("0039MnYb0qxYhV") {
		t.Errorf("再按一次应重新收藏，实际 %q", m.status)
	}
}

// 未登录时按 f 不该发请求，而是提示去登录。
func TestFavoriteNeedsLogin(t *testing.T) {
	m := fakeModel(t, PageSearch)
	m.session = sessionView{Checked: true}
	m.searchResults = []Track{{ID: "1001", SongMid: "0039MnYb0qxYhV", Title: "假歌一号", Playable: true}}
	m.reflow()
	next, cmd := m.Update(keyMsg("f"))
	m = next.(Model)
	if cmd != nil || !contains(m.status, "登录") {
		t.Errorf("未登录按 f 应提示登录且不发请求，实际 status=%q", m.status)
	}
}

// 收藏标记要真的画进曲目行，否则用户看不出状态。
func TestFavoriteMarkerRendered(t *testing.T) {
	m := fakeModel(t, PageSearch)
	m.searchResults = []Track{{
		ID: "1001", SongMid: "mid-x", Title: "假歌一号", Playable: true,
	}}
	// 搜索页只有 searchQuery 非空时才渲染结果列表（否则显示引导文案）。
	m.searchQuery = "假歌"
	m.reflow()
	m.w, m.h = 120, 35

	if view := m.View(); contains(view, "♥") {
		t.Fatal("没收藏时不该画爱心")
	}

	m.favorites = append(m.favorites, m.searchResults[0])
	if view := m.View(); !contains(view, "♥") {
		t.Error("收藏之后曲目行应带爱心标记")
	}
}

// 提交中再按 f 不发第二笔请求：两笔方向相反的写请求落地顺序由网络决定，
// 界面会和账号对不上。
func TestFavoriteIgnoresRepeatWhileBusy(t *testing.T) {
	m := fakeModel(t, PageSearch)
	m.searchResults = []Track{{ID: "1001", SongMid: "mid-x", Title: "假歌一号"}}
	m.reflow()
	m.favoriteBusy = true

	next, cmd := m.Update(keyMsg("f"))
	m = next.(Model)
	if cmd != nil {
		t.Error("提交中不该再发请求")
	}
	if m.status == "" {
		t.Error("应该说清楚为什么这次按键没生效")
	}
}

// 没有 songMid 的曲目没法收藏，要当场说清楚而不是发一笔注定失败的请求。
func TestFavoriteWithoutSongMid(t *testing.T) {
	m := fakeModel(t, PageSearch)
	m.searchResults = []Track{{ID: "1001", Title: "没有 mid 的歌"}}
	m.reflow()

	next, cmd := m.Update(keyMsg("f"))
	m = next.(Model)
	if cmd != nil {
		t.Error("没有 songMid 时不该发请求")
	}
	if !contains(m.status, "songMid") {
		t.Errorf("应说明缺少 songMid，实际 %q", m.status)
	}
}

// 收藏失败时状态栏要报错，而不是默默什么都不做。
func TestFavoriteFailureSurfaces(t *testing.T) {
	m := fakeModel(t, PageSearch)
	m.api = fakeAPI(t, "FAKE_ERROR=收藏接口炸了")
	m.searchResults = []Track{{ID: "1001", SongMid: "mid-x", Title: "假歌一号"}}
	m.reflow()

	m = press(t, m, "f")

	if m.favoriteBusy {
		t.Error("失败之后不该还挂着「提交中」")
	}
	if !contains(m.status, "收藏失败") {
		t.Errorf("应提示失败，实际 %q", m.status)
	}
	if m.isFavorite("mid-x") {
		t.Error("失败时不该把歌加进本地列表")
	}
}
