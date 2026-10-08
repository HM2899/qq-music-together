package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// ---------- 我的歌单 ----------
//
// 一个独立页签只列「我创建的 / 我收藏的」歌单（数据和音乐库页共用 library），
// 外加三件写操作：新建 / 删除自己的歌单、把歌加进自己的歌单、从自己的歌单里移除。
// 写接口都在脚本里（playlist-create / playlist-delete / playlist-song），经 musics.fcg 带签名。

// favoriteDirID 是「我喜欢」的目录 ID：不能删除，往里加歌等于收藏。
const favoriteDirID = "201"

// playlistOpMsg 是一次歌单写操作的结果。
type playlistOpMsg struct {
	op      string // create / delete / add / remove
	pl      Playlist
	track   Track
	created *Playlist
	err     error
}

// dialogKind 是「我的歌单」页上弹出的小对话框。
type dialogKind int

const (
	dialogNone           dialogKind = iota
	dialogNewPlaylist               // 输入新歌单的名字
	dialogDeletePlaylist            // 确认删除
)

// myPlaylists 是自己创建的歌单（含「我喜欢」）。音乐库没加载时为 nil。
func (m Model) myPlaylists() []Playlist {
	if m.library == nil {
		return nil
	}
	return m.library.CreatedPlaylists
}

// isMyPlaylist 判断一个歌单是不是自己创建的——只有这种才能往里加歌、从里面删歌。
// 按 dirId 认：收藏来的别人的歌单也有 id（tid），但 dirId 只有自己的歌单才有意义。
func (m Model) isMyPlaylist(pl Playlist) bool {
	if pl.DirID == "" || pl.Kind == "album" {
		return false
	}
	for _, p := range m.myPlaylists() {
		if p.DirID == pl.DirID && p.ID == pl.ID {
			return true
		}
	}
	return false
}

func (m Model) mineRows() []row {
	if m.library == nil {
		return nil
	}
	var rows []row
	created := m.library.CreatedPlaylists
	rows = append(rows, row{kind: rowSection, text: fmt.Sprintf("我创建的歌单 · %d 个", len(created))})
	for _, p := range created {
		rows = append(rows, row{kind: rowPlaylist, pl: p})
	}
	if len(m.library.CollectedPlaylists) > 0 {
		rows = append(rows, row{kind: rowSection,
			text: fmt.Sprintf("我收藏的歌单 · %d 个", len(m.library.CollectedPlaylists))})
		for _, p := range m.library.CollectedPlaylists {
			rows = append(rows, row{kind: rowPlaylist, pl: p})
		}
	}
	rows = append(rows, row{kind: rowNote,
		text: "回车打开 · N 新建歌单 · D 删除选中的歌单 · 任意页选中歌曲按 A 加入歌单"})
	return rows
}

func (m Model) viewMine(w, h int) string {
	switch {
	case m.libraryLoading && m.library == nil:
		return placeholder([]string{styleDim.Render("正在加载我的歌单…")}, w, h)
	case m.libraryErr != nil:
		if IsAuthError(m.libraryErr) {
			return placeholder([]string{
				styleWarn.Render("需要登录才能看我的歌单"),
				"",
				styleText.Render("按 L 打开登录层，用手机扫码登录。"),
			}, w, h)
		}
		return placeholder(errLines(m.libraryErr, "按 r 重试"), w, h)
	case m.library == nil:
		return placeholder([]string{styleDim.Render("还没加载。按 r 拉取。")}, w, h)
	}
	return m.viewList(w, h)
}

// ---------- 按键入口 ----------

// openNewPlaylist 弹出「新建歌单」输入框。
func (m *Model) openNewPlaylist() {
	if m.session.Checked && !m.session.LoggedIn {
		m.status = "新建歌单需要先登录，按 L 扫码"
		return
	}
	m.dialog = dialogNewPlaylist
	m.dialogInput.Clear()
	m.focus = FocusDialog
}

// openDeletePlaylist 对选中的歌单弹出删除确认。
func (m *Model) openDeletePlaylist() {
	r, ok := m.selectedRow()
	if !ok || r.kind != rowPlaylist {
		m.status = "先选中一个歌单"
		return
	}
	if !m.isMyPlaylist(r.pl) {
		m.status = "只能删除自己创建的歌单（收藏的歌单请在官方客户端取消收藏）"
		return
	}
	if r.pl.DirID == favoriteDirID {
		m.status = "「我喜欢」不能删除"
		return
	}
	m.dialog = dialogDeletePlaylist
	m.dialogTarget = r.pl
	m.focus = FocusDialog
}

// openPicker 为一首歌弹出「加入哪个歌单」。正在播放页作用于正在听的那首。
func (m *Model) openPicker() tea.Cmd {
	t, ok := m.favoriteTarget()
	if !ok {
		m.status = "先选中一首歌"
		return nil
	}
	if strings.TrimSpace(t.SongMid) == "" {
		m.status = "「" + t.Title + "」没有 songMid，加不进歌单"
		return nil
	}
	if m.session.Checked && !m.session.LoggedIn {
		m.status = "加入歌单需要先登录，按 L 扫码"
		return nil
	}
	m.pickerTrack = t
	m.pickerList = listState{}
	m.focus = FocusPicker
	// 歌单列表来自音乐库；还没拉过就现在拉，浮层里先显示「正在加载」。
	if m.library == nil && !m.libraryLoading {
		return m.cmdLibrary()
	}
	return nil
}

// removeFromCurrentPlaylist 把选中的歌移出当前打开的（自己的）歌单。
func (m *Model) removeFromCurrentPlaylist() tea.Cmd {
	if !m.detailActive() || !m.isMyPlaylist(m.detailPl) {
		return nil
	}
	r, ok := m.selectedRow()
	if !ok || r.kind != rowTrack {
		return nil
	}
	if m.playlistBusy {
		m.status = "上一个歌单操作还没完成，稍等一下"
		return nil
	}
	m.playlistBusy = true
	m.status = "正在从「" + m.detailPl.Title + "」移除：" + r.track.Title
	return m.cmdPlaylistSong("remove", m.detailPl, r.track)
}

func (m Model) handleDialogKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.dialog {
	case dialogNewPlaylist:
		switch msg.Type {
		case tea.KeyEsc:
			m.closeDialog()
			m.status = "已取消新建歌单"
		case tea.KeyEnter:
			name := strings.TrimSpace(m.dialogInput.Value())
			if name == "" || m.playlistBusy {
				return m, nil
			}
			m.playlistBusy = true
			m.status = "正在新建歌单：" + name
			return m, m.cmdCreatePlaylist(name)
		default:
			editInput(&m.dialogInput, msg)
		}
	case dialogDeletePlaylist:
		switch msg.String() {
		case "y", "Y":
			if m.playlistBusy {
				return m, nil
			}
			m.playlistBusy = true
			m.status = "正在删除歌单：" + m.dialogTarget.Title
			return m, m.cmdDeletePlaylist(m.dialogTarget)
		default: // 除了 y 一律当取消，免得误触删掉
			m.closeDialog()
			m.status = "已取消删除"
		}
	default:
		m.closeDialog()
	}
	return m, nil
}

func (m *Model) closeDialog() {
	m.dialog = dialogNone
	m.dialogTarget = Playlist{}
	m.focus = FocusList
	if m.pickerReturn {
		m.pickerReturn = false
		m.focus = FocusPicker
	}
}

func (m Model) handlePickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	lists := m.myPlaylists()
	switch msg.String() {
	case "esc", "q":
		m.focus = FocusList
	case "up", "k":
		m.pickerList.move(-1, len(lists), m.pickerViewH())
	case "down", "j":
		m.pickerList.move(1, len(lists), m.pickerViewH())
	case "N":
		// 想加的歌单还不存在：先新建，建好（或取消）后回到这里再选。
		m.pickerReturn = true
		m.openNewPlaylist()
	case "enter":
		i := m.pickerList.selected(len(lists))
		if i < 0 || m.playlistBusy {
			return m, nil
		}
		pl := lists[i]
		m.playlistBusy = true
		m.focus = FocusList
		m.status = "正在加入「" + pl.Title + "」：" + m.pickerTrack.Title
		return m, m.cmdPlaylistSong("add", pl, m.pickerTrack)
	}
	return m, nil
}

func (m Model) pickerViewH() int {
	h := m.bodyH() - 2 - 6
	if h < 1 {
		h = 1
	}
	return h
}

// ---------- 浮层渲染 ----------

func (m Model) viewPicker(w, h int) string {
	cardW := 60
	if cardW > w-4 {
		cardW = w - 4
	}
	if cardW < 24 {
		cardW = w - 2
	}
	title := styleActive.Render("📥 收藏到歌单")
	sub := "  " + styleText.Render("把「") + styleActive.Render(m.pickerTrack.Title) + styleText.Render("」加入歌单:")
	lines := []string{"", sub, ""}
	lists := m.myPlaylists()
	switch {
	case m.libraryLoading && m.library == nil:
		lines = append(lines, "    "+styleDim.Render("正在加载你的歌单…"))
	case m.libraryErr != nil && m.library == nil:
		for _, l := range errLines(m.libraryErr, "按 esc 关闭") {
			lines = append(lines, "    "+l)
		}
	case len(lists) == 0:
		lines = append(lines, "    "+styleDim.Render("你还没有歌单，按 N 新建一个。"))
	default:
		st := m.pickerList
		n := m.pickerViewH()
		for i := st.offset; i < st.offset+n && i < len(lists); i++ {
			p := lists[i]
			marker := "    "
			itemTitle := styleText.Render(p.Title)
			if i == st.cursor {
				marker = "  " + styleActive.Render("▶ ")
				itemTitle = styleActive.Render(p.Title)
			}
			extra := styleDim.Render(fmt.Sprintf("  %d 首", p.SongCount))
			if p.DirID == favoriteDirID {
				extra += styleDim.Render("（等于收藏）")
			}
			lines = append(lines, marker+itemTitle+extra)
		}
	}
	lines = append(lines, "")
	hint := styleKeyBadge.Render("j/k") + styleText.Render(" 选择  ") +
		styleKeyActive.Render("Enter") + styleText.Render(" 加入  ") +
		styleKeyBadge.Render("N") + styleText.Render(" 新建  ") +
		styleKeyBadge.Render("Esc") + styleText.Render(" 取消")
	return renderModalCard(w, h, cardW, title, lines, hint, colGreen)
}

func (m Model) viewDialog(w, h int) string {
	switch m.dialog {
	case dialogNewPlaylist:
		cardW := 52
		if cardW > w-4 {
			cardW = w - 4
		}
		if cardW < 24 {
			cardW = w - 2
		}
		innerW := cardW - 2
		inputW := innerW - 4
		if inputW < 6 {
			inputW = 6
		}

		title := styleActive.Render("✨ 新建歌单")
		var bodyLines []string
		bodyLines = append(bodyLines,
			"",
			"  "+styleText.Render("歌单名："),
		)
		box := m.dialogInput.View(inputW, "输入新歌单名字", !m.playlistBusy)
		for _, l := range strings.Split(box, "\n") {
			bodyLines = append(bodyLines, "  "+l)
		}
		bodyLines = append(bodyLines, "")

		hint := styleKeyActive.Render("Enter") + styleText.Render(" 创建歌单  ") +
			styleKeyBadge.Render("Esc") + styleText.Render(" 取消")
		if m.playlistBusy {
			hint = styleWarn.Render("⏳ 正在创建中…")
		}
		return renderModalCard(w, h, cardW, title, bodyLines, hint, colGreen)

	case dialogDeletePlaylist:
		cardW := 56
		if cardW > w-4 {
			cardW = w - 4
		}
		if cardW < 24 {
			cardW = w - 2
		}
		title := styleWarn.Render("⚠️  删除歌单确认")
		var bodyLines []string
		bodyLines = append(bodyLines,
			"",
			"  "+styleWarn.Render("警告：歌单删除后将无法恢复！"),
			"",
			"  "+styleText.Render(fmt.Sprintf("确定删除「%s」（%d 首）吗？删除后无法恢复。",
				m.dialogTarget.Title, m.dialogTarget.SongCount)),
			"",
		)
		hint := styleKeyWarn.Render("y") + styleText.Render(" 确认删除  ") +
			styleKeyBadge.Render("其它任意键") + styleText.Render(" 取消")
		if m.playlistBusy {
			hint = styleWarn.Render("⏳ 正在删除中…")
		}
		return renderModalCard(w, h, cardW, title, bodyLines, hint, colWarn)
	}
	return fitLines(nil, w, h)
}

// ---------- 命令 ----------

func (m *Model) cmdCreatePlaylist(name string) tea.Cmd {
	c, ctx := m.api, m.baseCtx
	if c == nil {
		m.playlistBusy = false
		return nil
	}
	return func() tea.Msg {
		pl, err := c.CreatePlaylist(ctx, name)
		return playlistOpMsg{op: "create", created: pl, err: err}
	}
}

func (m *Model) cmdDeletePlaylist(pl Playlist) tea.Cmd {
	c, ctx := m.api, m.baseCtx
	if c == nil {
		m.playlistBusy = false
		return nil
	}
	return func() tea.Msg {
		return playlistOpMsg{op: "delete", pl: pl, err: c.DeletePlaylist(ctx, pl.DirID)}
	}
}

func (m *Model) cmdPlaylistSong(action string, pl Playlist, t Track) tea.Cmd {
	c, ctx := m.api, m.baseCtx
	if c == nil {
		m.playlistBusy = false
		return nil
	}
	return func() tea.Msg {
		return playlistOpMsg{op: action, pl: pl, track: t, err: c.PlaylistSong(ctx, action, pl.DirID, t.SongMid)}
	}
}

// onPlaylistOp 落地一次写操作的结果。成功后重拉音乐库，歌单列表和歌曲数才是新的。
func (m Model) onPlaylistOp(msg playlistOpMsg) (Model, tea.Cmd) {
	m.playlistBusy = false
	if msg.err != nil {
		label := map[string]string{"create": "新建歌单", "delete": "删除歌单",
			"add": "加入歌单", "remove": "移出歌单"}[msg.op]
		m.status = label + "失败：" + msg.err.Error()
		return m, nil // 对话框留着（新建时名字还在），改一改可以直接重试
	}

	switch msg.op {
	case "create":
		title := ""
		if msg.created != nil {
			title = msg.created.Title
		}
		m.status = "已新建歌单「" + title + "」"
		// 从「加入歌单」浮层里按 N 进来的，closeDialog 会回到浮层继续选。
		m.closeDialog()
	case "delete":
		m.status = "已删除歌单「" + msg.pl.Title + "」"
		m.closeDialog()
		if m.detailPl.DirID == msg.pl.DirID {
			m.closeDetail()
		}
	case "add":
		m.status = "已加入「" + msg.pl.Title + "」：" + msg.track.Title
		m.notePlaylistFavorite(msg.pl, msg.track, true)
	case "remove":
		m.status = "已从「" + msg.pl.Title + "」移除：" + msg.track.Title
		m.notePlaylistFavorite(msg.pl, msg.track, false)
		if m.detail != nil && m.detailPl.DirID == msg.pl.DirID {
			m.detail.Tracks = removeTrackByMid(m.detail.Tracks, msg.track.SongMid)
			if m.detail.Total > 0 {
				m.detail.Total--
			}
		}
	}
	m.reflow()
	return m, m.cmdLibrary()
}

// notePlaylistFavorite：往「我喜欢」歌单加 / 删歌就是收藏 / 取消收藏，♥ 要跟着变。
func (m *Model) notePlaylistFavorite(pl Playlist, t Track, in bool) {
	if pl.DirID != favoriteDirID || t.SongMid == "" {
		return
	}
	if m.favLocal == nil {
		m.favLocal = map[string]bool{}
	}
	m.favLocal[t.SongMid] = in
	if !in {
		m.favorites = removeTrackByMid(m.favorites, t.SongMid)
	}
}
