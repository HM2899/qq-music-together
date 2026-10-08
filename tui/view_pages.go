package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// viewPage 按当前页分发。六个页面里只有一个会真正渲染。
//
// 歌单/专辑详情是盖在当前页上的一层，所以先判它——这样详情在哪个页打开
// 都走同一份渲染，不用给六页各写一遍。
func (m Model) viewPage(w, h int) string {
	if m.detailActive() {
		return m.viewDetail(w, h)
	}
	switch m.page {
	case PageExplore:
		return m.viewExplore(w, h)
	case PageSearch:
		return m.viewSearch(w, h)
	case PageNow:
		return m.viewNow(w, h)
	case PageFavorites:
		return m.viewFavorites(w, h)
	case PageLibrary:
		return m.viewLibrary(w, h)
	case PageMine:
		return m.viewMine(w, h)
	case PageQueue:
		return m.viewQueue(w, h)
	}
	return ""
}

// ---------- 歌单 / 专辑详情 ----------

func (m Model) viewDetail(w, h int) string {
	switch {
	case m.detailLoading:
		return placeholder([]string{
			styleDim.Render("正在加载：" + m.detailTitle + "…"),
			"",
			styleDim.Render("（按 esc 返回）"),
		}, w, h)
	case m.detailErr != nil:
		return placeholder(errLines(m.detailErr, "按 esc 返回，或按 r 重试"), w, h)
	case m.detail == nil:
		return placeholder([]string{styleDim.Render("还没有内容。按 esc 返回。")}, w, h)
	}
	return m.viewList(w, h)
}

// ---------- 通用列表 ----------

// viewList 渲染当前页面的行列表，并把视口裁到 h 行。
func (m Model) viewList(w, h int) string {
	rows := m.rows()
	if len(rows) == 0 {
		return placeholder([]string{styleDim.Render("这里还没有内容。")}, w, h)
	}
	st := m.lists[m.page]
	viewport := h
	lines := make([]string, 0, viewport)
	trackNo := 0
	for i := 0; i < len(rows); i++ {
		if rows[i].kind == rowTrack {
			trackNo++
		}
		if i < st.offset || i >= st.offset+viewport {
			continue
		}
		lines = append(lines, m.renderRow(rows[i], trackNo, i == st.cursor, w))
	}
	return fitLines(lines, w, h)
}

func (m Model) renderRow(r row, trackNo int, selected bool, w int) string {
	switch r.kind {
	case rowTrack:
		return m.trackRow(r.track, trackNo-1, selected, w)
	case rowPlaylist:
		return m.playlistRow(r.pl, selected, w)
	case rowSinger:
		return m.singerRow(r.singer, selected, w)
	case rowSection:
		return stylePink.Render(ansi.Truncate("─ "+r.text+" ", w, "…"))
	case rowNote:
		return styleDim.Render(ansi.Truncate("  "+r.text, w, "…"))
	}
	return ""
}

// playlistRow 渲染歌单行。歌单没有时长，右侧放曲目数。
func (m Model) playlistRow(p Playlist, selected bool, w int) string {
	if w < 20 {
		w = 20
	}
	marker := "  "
	if selected {
		marker = styleActive.Render("▶ ")
	}
	tail := styleDim.Render(fmt.Sprintf("%d 首 ", p.SongCount))

	room := w - lipgloss.Width(marker) - lipgloss.Width(tail) - 3
	if room < 10 {
		room = 10
	}
	title := ansi.Truncate(p.Title, room, "…")
	if p.Creator != "" && room > 28 {
		title += styleDim.Render(" — " + ansi.Truncate(p.Creator, room/3, "…"))
	}

	line := marker + styleText.Render("♪ ") + title + " " + tail
	line = padRight(line, w)
	if selected {
		return styleRowOn.Render(line)
	}
	return line
}

// singerRow 渲染歌手行。右侧放作品数（歌曲/专辑），和桌面端歌手卡片一致。
func (m Model) singerRow(s Singer, selected bool, w int) string {
	if w < 20 {
		w = 20
	}
	marker := "  "
	if selected {
		marker = styleActive.Render("▶ ")
	}
	tail := ""
	if s.SongCount > 0 {
		tail = styleDim.Render(fmt.Sprintf("%d 首 ", s.SongCount))
	}

	room := w - lipgloss.Width(marker) - lipgloss.Width(tail) - 3
	if room < 10 {
		room = 10
	}
	name := ansi.Truncate(s.Name, room, "…")
	// 歌手歌曲列表那个接口要签名（见 queue.go 的 playSelected），所以这里
	// 说明回车实际会发生什么，免得按了以为进不去。
	hint := ""
	if room > 34 {
		hint = styleDim.Render("  ⏎ 按名字搜他的歌")
	}

	line := marker + styleText.Render("☺ ") + name + hint + " " + tail
	line = padRight(line, w)
	if selected {
		return styleRowOn.Render(line)
	}
	return line
}

// ---------- 探索 ----------

func (m Model) viewExplore(w, h int) string {
	switch {
	case m.discoverLoading:
		return placeholder([]string{
			styleDim.Render("正在加载探索内容…"),
			"",
			styleDim.Render("（每日推荐要登录后才有；最慢可能要几十秒）"),
		}, w, h)
	case m.discoverErr != nil && m.discover == nil:
		return placeholder(errLines(m.discoverErr, "按 r 重试"), w, h)
	}
	return m.viewList(w, h)
}

// ---------- 搜索 ----------

func (m Model) viewSearch(w, h int) string {
	box := m.input.View(w, "搜索歌曲 / 歌手 / 专辑 / 歌单…", m.focus == FocusSearch)
	boxLines := strings.Split(box, "\n")

	tab := m.searchTabLine(w)
	listH := h - len(boxLines) - 1
	if listH < 1 {
		return fitLines(append(boxLines, tab), w, h)
	}

	var body string
	switch {
	case m.searchLoading:
		body = placeholder([]string{styleDim.Render("搜索中…")}, w, listH)
	case m.searchErr != nil:
		body = placeholder(errLines(m.searchErr, "按回车重试"), w, listH)
	case strings.TrimSpace(m.searchQuery) == "":
		body = placeholder([]string{
			styleText.Render("输入关键词后回车开始搜索。"),
			"",
			styleDim.Render("按 Ctrl+T 在 歌曲 / 歌手 / 专辑 / 歌单 之间切换类型。"),
			styleDim.Render("曲目结果里 回车 = 播放（整段替换队列），a = 只加进队列。"),
		}, w, listH)
	case m.searchShown() == 0:
		body = placeholder([]string{
			styleDim.Render(fmt.Sprintf("没有找到与「%s」相关的%s。",
				m.searchQuery, searchTypeLabels[m.searchType])),
			"",
			styleDim.Render("换个关键词或按 Ctrl+T 换个类型；偶发空结果多半是网络抖动。"),
		}, w, listH)
	default:
		body = m.viewList(w, listH)
	}

	lines := append(boxLines, tab)
	lines = append(lines, strings.Split(body, "\n")...)
	return fitLines(lines, w, h)
}

// searchTabLine 是搜索类型的分段控件，外加当前的翻页位置。
func (m Model) searchTabLine(w int) string {
	var parts []string
	for _, k := range searchTypeOrder {
		label := searchTypeLabels[k]
		if k == m.searchType {
			parts = append(parts, styleActive.Render("["+label+"]"))
		} else {
			parts = append(parts, styleDim.Render(" "+label+" "))
		}
	}
	left := "  " + strings.Join(parts, "")
	right := ""
	if m.searchTotal > 0 {
		right = styleDim.Render(fmt.Sprintf("共 %d 条 · 第 %d 页 · ", m.searchTotal, m.searchPage))
	}
	room := w - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if room < 0 {
		return ansi.Truncate(left, w, "…")
	}
	return left + strings.Repeat(" ", room) + right + " "
}

// ---------- 正在播放 ----------

func (m Model) viewNow(w, h int) string {
	cur, ok := m.current()
	if !ok {
		return placeholder([]string{
			styleDim.Render("还没有在播放的歌。"),
			"",
			styleText.Render("去「探索」或「搜索」里选一首，回车就从这里开始播。"),
		}, w, h)
	}

	sub := cur.Artist
	if cur.Album != "" {
		sub += " · " + cur.Album
	}
	head := []string{
		" " + styleActive.Render(cur.Title),
		" " + styleDim.Render(sub),
	}
	if cur.Vip && !cur.Playable {
		head = append(head, " "+styleWarn.Render("会员专享，可能无法直接播放"))
	}
	head = append(head, "")

	// 封面放在左边，歌名和歌词在右边。终端窄、或者这首没有封面时整页给歌词。
	cols, rows := m.coverSize(w, h)
	var cover []string
	if cols > 0 {
		cover = m.coverLines(cols, rows)
	}
	textW := w
	if len(cover) > 0 {
		textW = w - cols - 3
	}

	lyricH := h - len(head)
	var lines []string
	if lyricH < 1 {
		lines = head
	} else {
		lines = append(append([]string{}, head...), m.lyricsBlock(lyricH, cur)...)
	}
	if len(cover) == 0 {
		return fitLines(lines, w, h)
	}

	// 封面上下居中；左右两栏各自先截好宽度再拼，避免长歌词把封面挤歪。
	top := (h - rows) / 2
	if top < 0 {
		top = 0
	}
	right := strings.Split(fitLines(lines, textW, h), "\n")
	out := make([]string, h)
	blank := strings.Repeat(" ", cols)
	for i := 0; i < h; i++ {
		left := blank
		if i >= top && i-top < len(cover) {
			left = cover[i-top]
		}
		r := ""
		if i < len(right) {
			r = right[i]
		}
		out[i] = " " + left + "  " + r
	}
	return strings.Join(out, "\n")
}

// lyricsBlock 按当前播放位置渲染滚动歌词。
// 手动滚动（m.lyricFollow == false）时按 m.lyricScroll 定位，不再跟着播放走。
func (m Model) lyricsBlock(n int, track Track) []string {
	if n < 1 {
		return nil
	}
	if len(track.Lyrics) == 0 {
		return []string{styleDim.Render("（这首歌没有歌词）")}
	}

	cur := m.currentLyricIndex(track.Lyrics)
	top := m.lyricsTop(len(track.Lyrics), cur, n)

	out := make([]string, 0, n)
	for i := top; i < top+n && i < len(track.Lyrics); i++ {
		l := track.Lyrics[i]
		text := l.Text
		if i == cur && m.lyricFollow {
			out = append(out, styleActive.Render("  ▶ "+text))
		} else {
			out = append(out, styleDim.Render("    "+text))
		}
		if l.Translation != "" && text != "" {
			out = append(out, stylePink.Render("      "+l.Translation))
		}
	}
	if !m.lyricFollow {
		out = append(out, "", styleDim.Render("  手动浏览中 —— 按 a 回到自动跟随"))
	}
	return out
}

// lyricLead 让歌词行比时间戳提前一点点亮起。
//
// LRC 的时间戳标的是「开口唱」那一刻，正好卡点切换在观感上已经是慢的——
// 眼睛要先看到字。取值与桌面端 QQMusicService.updateCurrentLyric 的 +0.12 保持一致，
// 两边（以及顶栏胶囊）才会在同一刻换行。
const lyricLead = 0.12

// currentLyricIndex 返回当前播放位置对应的歌词行下标。歌词为空时返回 -1。
//
// 用 displayPos 而不是 m.pos：后者是上次 tick 采到的值，平均落后半个周期。
func (m Model) currentLyricIndex(lyrics []Lyric) int {
	if len(lyrics) == 0 {
		return -1
	}
	pos := m.displayPos() + lyricLead
	cur := 0
	for i, l := range lyrics {
		if pos >= l.Time {
			cur = i
		} else {
			break
		}
	}
	return cur
}

// lyricsTop 算出歌词视口的第一行。// 自动跟随时把当前行摆在中间；手动滚动时用存下来的绝对行号。
func (m Model) lyricsTop(total, cur, n int) int {
	if m.lyricFollow {
		return m.lyricsTopAuto(total, cur, n)
	}
	return m.clampLyricScroll(m.lyricScroll, total, n)
}

func (m Model) lyricsTopAuto(total, cur, n int) int {
	return m.clampLyricScroll(cur-n/2, total, n)
}

func (m Model) clampLyricScroll(top, total, n int) int {
	if top < 0 {
		top = 0
	}
	if max := total - n; top > max {
		top = max
	}
	if top < 0 {
		top = 0
	}
	return top
}

// lyricViewport 是「正在播放」页歌词区能显示的行数。
// 必须和 viewNow 里算出来的 lyricH 一致，否则手动滚动会跳。
func (m Model) lyricViewport() int {
	head := 3 // 标题、副标题、空行
	if cur, ok := m.current(); ok && cur.Vip && !cur.Playable {
		head++
	}
	n := (m.bodyH() - 2) - head
	if n < 1 {
		n = 1
	}
	return n
}

// ---------- 我喜欢 ----------

func (m Model) viewFavorites(w, h int) string {
	switch {
	case m.favoritesLoading:
		return placeholder([]string{styleDim.Render("正在加载「我喜欢」…")}, w, h)
	case m.favoritesErr != nil:
		if IsAuthError(m.favoritesErr) {
			return placeholder([]string{
				styleWarn.Render("需要登录才能看「我喜欢」"),
				"",
				styleText.Render("按 L 打开登录层，用手机扫码登录。"),
			}, w, h)
		}
		return placeholder(errLines(m.favoritesErr, "按 r 重试"), w, h)
	case m.favorites == nil:
		return placeholder([]string{styleDim.Render("还没加载。按 r 拉取「我喜欢」。")}, w, h)
	case len(m.favorites) == 0:
		return placeholder([]string{styleDim.Render("「我喜欢」里还没有歌。")}, w, h)
	}
	return m.viewList(w, h)
}

// ---------- 音乐库 ----------

func (m Model) viewLibrary(w, h int) string {
	switch {
	case m.libraryLoading:
		return placeholder([]string{styleDim.Render("正在加载音乐库…")}, w, h)
	case m.libraryErr != nil:
		if IsAuthError(m.libraryErr) {
			return placeholder([]string{
				styleWarn.Render("需要登录才能看音乐库"),
				"",
				styleText.Render("按 L 打开登录层，用手机扫码登录。"),
			}, w, h)
		}
		return placeholder(errLines(m.libraryErr, "按 r 重试"), w, h)
	case m.library == nil:
		return placeholder([]string{styleDim.Render("还没加载。按 r 拉取音乐库。")}, w, h)
	case len(m.rows()) == 0:
		return placeholder([]string{styleDim.Render("音乐库里还没有内容。")}, w, h)
	}
	return m.viewList(w, h)
}

// ---------- 播放队列 ----------

func (m Model) viewQueue(w, h int) string {
	if len(m.queue) == 0 {
		return placeholder([]string{
			styleDim.Render("播放队列是空的。"),
			"",
			styleText.Render("在列表里按回车播放（会整段替换队列），或者按 a 只加进来。"),
		}, w, h)
	}
	return m.viewList(w, h)
}

// ---------- 评论浮层 ----------

// viewComments 渲染当前曲目的评论。
//
// 按**实际行数**排版：每条评论 = 一行抬头 + 自动换行的正文（完整显示，不截断）。
// 以前按「一条评论一行」分配高度，而每条其实占两行、正文还被压成一行截断，
// 超出的部分连同底部的写评论输入框一起被裁掉——评论看不全、写的字也看不见。
func (m Model) viewComments(w, h int) string {
	header := make([]string, 0, 3)
	if m.commentsTrack.Title != "" {
		header = append(header, " "+styleActive.Render(m.commentsTrack.Title)+
			styleDim.Render(" 的评论"))
	} else {
		header = append(header, styleText.Render(" 评论"))
	}
	page := m.commentsPage
	if page <= 0 {
		page = 1
	}
	hot, latest := styleDim.Render("热评"), styleDim.Render("最新")
	if m.commentsSortOrHot() == "new" {
		latest = styleActive.Render("[最新]")
	} else {
		hot = styleActive.Render("[热评]")
	}
	header = append(header, " "+hot+" "+latest+styleDim.Render(fmt.Sprintf(
		"  共 %d 条 · 第 %d 页 · ] 下一页  [ 上一页  s 切换", m.commentsTotal, page)), "")

	footer := m.commentsFooter(w)
	bodyH := h - len(header) - len(footer)
	if bodyH < 1 {
		bodyH = 1
	}

	var body []string
	switch {
	case m.commentsLoading:
		body = []string{"  " + styleDim.Render("正在加载评论…")}
	case m.commentsErr != nil:
		for _, l := range errLines(m.commentsErr, "按 r 重试") {
			body = append(body, "  "+l)
		}
	case len(m.comments) == 0:
		body = []string{"  " + styleDim.Render("这首歌还没有评论。")}
	default:
		body = m.commentsBody(w, bodyH)
	}
	for len(body) < bodyH {
		body = append(body, "")
	}
	if len(body) > bodyH {
		body = body[:bodyH]
	}

	lines := append(header, body...)
	lines = append(lines, footer...)
	return fitLines(lines, w, h)
}

// commentsFooter 是底部区域：写评论时是提示行 + 3 行高的输入框，否则是一行按键提示。
func (m Model) commentsFooter(w int) []string {
	if !m.commentWriting {
		return []string{"", "  " + styleDim.Render(
			"j/k 滚动 · l 点赞/取消 · w 写评论 · s 热评/最新 · esc 关闭")}
	}
	label := " 写评论 · 回车发送 · esc 取消"
	if m.commentSending {
		label = " 发送中…"
	}
	box := m.commentInput.View(w-2, "说点什么…", !m.commentSending)
	return append([]string{styleActive.Render(label)}, strings.Split(box, "\n")...)
}

// commentsBody 从视口顶端那条开始往下排，直到填满 n 行；保证选中的那条完整可见。
func (m Model) commentsBody(w, n int) []string {
	blocks := make([][]string, len(m.comments))
	block := func(i int) []string {
		if blocks[i] == nil {
			blocks[i] = m.commentBlock(m.comments[i], i == m.commentsList.cursor, w)
		}
		return blocks[i]
	}

	cur := m.commentsList.cursor
	start := m.commentsList.offset
	if start > cur {
		start = cur
	}
	if start < 0 {
		start = 0
	}
	// 选中那条的末行要落在视口里，否则顶端往下挪。
	for start < cur {
		used := 0
		for i := start; i <= cur; i++ {
			used += len(block(i))
		}
		if used <= n {
			break
		}
		start++
	}

	var out []string
	for i := start; i < len(m.comments) && len(out) < n; i++ {
		out = append(out, block(i)...)
	}
	return out
}

// commentBlock 渲染一条评论：抬头（作者 · 赞 · 回复 · 日期）+ 换行后的完整正文 + 一行空隙。
func (m Model) commentBlock(c Comment, selected bool, w int) []string {
	marker := "  "
	if selected {
		marker = styleActive.Render("▶ ")
	}
	head := styleText.Render(ansi.Truncate(c.Author, w/3, "…"))
	if c.IsPraised {
		head += stylePink.Render(fmt.Sprintf("  👍 %d 已赞", c.Likes))
	} else if c.Likes > 0 {
		head += styleDim.Render(fmt.Sprintf("  👍 %d", c.Likes))
	}
	if c.Replies > 0 {
		head += styleDim.Render(fmt.Sprintf("  💬 %d", c.Replies))
	}
	if c.Time > 0 {
		head += styleDim.Render("  " + time.Unix(c.Time, 0).Format("2006-01-02"))
	}
	lines := []string{marker + head}

	text := strings.ReplaceAll(c.Text, "\r", "")
	bodyW := w - 4
	if bodyW < 10 {
		bodyW = 10
	}
	textStyle := styleDim
	if selected {
		textStyle = styleText
	}
	for _, para := range strings.Split(text, "\n") {
		if strings.TrimSpace(para) == "" {
			continue
		}
		// Hardwrap 按显示宽度折行（中文算 2 列），长英文单词也会被拆开，保证不越界。
		for _, l := range strings.Split(ansi.Hardwrap(para, bodyW, true), "\n") {
			lines = append(lines, "    "+textStyle.Render(l))
		}
	}
	return append(lines, "")
}

// ---------- 帮助 ----------

func (m Model) viewHelpPage(w, h int) string {
	line := func(k, d string) string {
		return styleActive.Render(fmt.Sprintf("  %-12s", k)) + styleText.Render(d)
	}

	cardW := 84
	if cardW > w-4 {
		cardW = w - 4
	}
	if cardW < 24 {
		cardW = w - 2
	}

	title := stylePink.Render("💡 QQ 音乐快捷键速查 · 按键说明")

	// 宽屏模式：双列排版更紧凑易读
	if cardW >= 76 {
		colW := (cardW - 6) / 2
		left := []string{
			stylePink.Render("【 播放与控制 】"),
			line("space", "播放 / 暂停"),
			line("n / p", "下一首 / 上一首"),
			line("← / →", "快退 / 快进 5 秒"),
			line("m", "静音开关"),
			line("Q", "切换音质(标准/128/320/无损)"),
			line("c", "查看曲目评论"),
			"",
			stylePink.Render("【 搜索与翻页 】"),
			line("/", "搜索（进入输入框）"),
			line("Ctrl+T", "切换 歌曲/歌手/专辑/歌单"),
			line("] / [", "下一页 / 上一页"),
		}
		right := []string{
			stylePink.Render("【 列表与导航 】"),
			line("1 - 7 / Tab", "切换页签"),
			line("↑ ↓ / j k", "移动选择"),
			line("g / G", "跳到首 / 尾"),
			line("回车", "曲目=播放 歌单/专辑=详情"),
			line("a", "把选中的歌追加到队列"),
			line("A", "把选中的歌加入我的歌单"),
			"",
			stylePink.Render("【 歌单与系统 】"),
			line("d", "移出队列或歌单"),
			line("N / D", "新建 / 删除歌单"),
			line("r", "刷新当前页"),
			line("L", "扫码登录 / 账号中心"),
			line("esc / q", "退出详情或浮层 / 退出程序"),
		}
		maxRows := len(left)
		if len(right) > maxRows {
			maxRows = len(right)
		}
		var bodyLines []string
		bodyLines = append(bodyLines, "")
		for i := 0; i < maxRows; i++ {
			lStr, rStr := "", ""
			if i < len(left) {
				lStr = left[i]
			}
			if i < len(right) {
				rStr = right[i]
			}
			row := padRight(lStr, colW) + styleDim.Render(" │ ") + rStr
			bodyLines = append(bodyLines, row)
		}
		bodyLines = append(bodyLines, "")
		hint := styleKeyBadge.Render("? / Esc") + styleText.Render(" 关闭帮助  ") +
			styleDim.Render("回车播放替换整个队列，与客户端一致")
		return renderModalCard(w, h, cardW, title, bodyLines, hint, colPink)
	}

	// 窄屏模式：单列排版
	var lines []string
	lines = append(lines,
		"",
		stylePink.Render("【 播放控制 】"),
		line("space", "播放 / 暂停"),
		line("n / p", "下一首 / 上一首"),
		line("← / →", "快退 / 快进 5 秒"),
		line("m", "静音开关"),
		line("Q", "切换音质"),
		line("c", "曲目评论"),
		"",
		stylePink.Render("【 列表与导航 】"),
		line("1-7/Tab", "切换页签"),
		line("↑↓/jk", "移动选择"),
		line("g / G", "跳到首 / 尾"),
		line("回车", "曲目播放/详情"),
		line("a / A", "追加队列 / 收藏"),
		"",
		stylePink.Render("【 搜索与操作 】"),
		line("/", "发起搜索"),
		line("Ctrl+T", "切换搜索类型"),
		line("] / [", "翻页"),
		line("N / D", "新建 / 删除歌单"),
		line("L", "登录 / 账号"),
		line("esc / q", "返回 / 退出"),
		"",
	)
	hint := styleKeyBadge.Render("? / Esc") + styleText.Render(" 关闭帮助说明")
	return renderModalCard(w, h, cardW, title, lines, hint, colPink)
}
