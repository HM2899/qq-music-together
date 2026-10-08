package main

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// ---------- 主题 ----------

var (
	colGreen = lipgloss.Color("42") // QQ 音乐绿
	colPink  = lipgloss.Color("212")
	colDim   = lipgloss.Color("241")
	colText  = lipgloss.Color("252")
	colWarn  = lipgloss.Color("203")

	styleDim    = lipgloss.NewStyle().Foreground(colDim)
	styleActive = lipgloss.NewStyle().Foreground(colGreen).Bold(true)
	stylePink   = lipgloss.NewStyle().Foreground(colPink)
	styleText   = lipgloss.NewStyle().Foreground(colText)
	styleWarn   = lipgloss.NewStyle().Foreground(colWarn)
	styleTabOn  = lipgloss.NewStyle().Foreground(lipgloss.Color("232")).Background(colGreen).Bold(true)
	styleTabOff = lipgloss.NewStyle().Foreground(colDim)
	styleRowOn  = lipgloss.NewStyle().Foreground(colText).Background(lipgloss.Color("237"))

	// styleHeader 只用在页签栏最左边的应用名上。
	styleHeader = lipgloss.NewStyle().
			Background(lipgloss.Color("236")).
			Foreground(colGreen).
			Bold(true)

	styleKeyBadge = lipgloss.NewStyle().
			Foreground(lipgloss.Color("254")).
			Background(lipgloss.Color("238")).
			Bold(true).
			Padding(0, 1)

	styleKeyActive = lipgloss.NewStyle().
			Foreground(lipgloss.Color("16")).
			Background(colGreen).
			Bold(true).
			Padding(0, 1)

	styleKeyWarn = lipgloss.NewStyle().
			Foreground(lipgloss.Color("15")).
			Background(colWarn).
			Bold(true).
			Padding(0, 1)
)

// ---------- 通用渲染辅助 ----------

// renderModalCard 把内容行包装在一个精美的浮层弹窗卡片中，并在 (w, h) 区域内居中显示。
// - w, h: 视口可用宽高
// - cardW: 期望卡片宽度（会自动根据 w 限制）
// - title: 弹窗顶部标题
// - content: 卡片内容多行
// - footer: 底部操作快捷键栏（为空则不显示底部栏）
// - border: 边框强调色
func renderModalCard(w, h, cardW int, title string, content []string, footer string, border lipgloss.Color) string {
	if w < 45 || h < 8 {
		var all []string
		if title != "" {
			all = append(all, title)
		}
		all = append(all, content...)
		if footer != "" {
			all = append(all, footer)
		}
		return fitLines(all, w, h)
	}

	actualW := cardW
	if actualW > w-2 {
		actualW = w - 2
	}
	if actualW < 14 {
		actualW = 14
	}

	innerW := actualW - 2
	if innerW < 4 {
		innerW = 4
	}

	// 组装卡片内部行
	var cardBody []string
	if title != "" {
		cardBody = append(cardBody, " "+title)
		cardBody = append(cardBody, styleDim.Render(strings.Repeat("─", innerW)))
	}
	for _, l := range content {
		cardBody = append(cardBody, ansi.Truncate(l, innerW, "…"))
	}
	if footer != "" {
		cardBody = append(cardBody, styleDim.Render(strings.Repeat("─", innerW)))
		cardBody = append(cardBody, " "+ansi.Truncate(footer, innerW-1, "…"))
	}

	// 组合成卡片（加边框）
	cardContent := strings.Join(cardBody, "\n")
	cardRendered := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(border).
		Width(innerW).
		Render(cardContent)

	cardLines := strings.Split(cardRendered, "\n")
	cardH := len(cardLines)
	actualCardW := lipgloss.Width(cardLines[0])

	// 垂直与水平居中计算
	topPad := (h - cardH) / 2
	if topPad < 0 {
		topPad = 0
	}
	leftPad := (w - actualCardW) / 2
	if leftPad < 0 {
		leftPad = 0
	}
	padStr := strings.Repeat(" ", leftPad)

	var finalLines []string
	for i := 0; i < topPad; i++ {
		finalLines = append(finalLines, "")
	}
	for _, l := range cardLines {
		finalLines = append(finalLines, padStr+l)
	}

	return fitLines(finalLines, w, h)
}

// panel 画一个指定宽高的圆角边框面板（w/h 含边框）。
func panel(content string, w, h int, border lipgloss.Color) string {
	if w < 3 {
		w = 3
	}
	if h < 3 {
		h = 3
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(border).
		Width(w - 2).
		Height(h - 2).
		Render(content)
}

// fitLines 把行数组裁到 w 宽、h 高，返回可直接渲染的文本。
func fitLines(lines []string, w, h int) string {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	out := make([]string, 0, h)
	for _, l := range lines {
		out = append(out, ansi.Truncate(l, w, "…"))
	}
	for len(out) < h {
		out = append(out, "")
	}
	if len(out) > h {
		out = out[:h]
	}
	return strings.Join(out, "\n")
}

func progressBar(cur, dur float64, width int) string {
	if width < 10 {
		width = 10
	}
	ratio := 0.0
	if dur > 0 {
		ratio = cur / dur
	}
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	pos := int(ratio * float64(width-1))
	return styleActive.Render(strings.Repeat("━", pos)+"●") +
		styleDim.Render(strings.Repeat("─", width-1-pos))
}

func formatTime(sec float64) string {
	if sec < 0 || math.IsNaN(sec) {
		sec = 0
	}
	t := int(sec)
	return fmt.Sprintf("%02d:%02d", t/60, t%60)
}

// padRight 按显示宽度补空格（CJK 字符宽度是 2，不能用 len 算）。
func padRight(s string, width int) string {
	gap := width - lipgloss.Width(s)
	if gap <= 0 {
		return s
	}
	return s + strings.Repeat(" ", gap)
}

// ---------- 页签栏 ----------

func (m Model) viewTabBar() string {
	var b strings.Builder
	b.WriteString(styleHeader.Render(" ♫ QQ音乐 TUI "))
	for p := Page(0); p < pageCount; p++ {
		label := fmt.Sprintf(" %d %s ", int(p)+1, p.title())
		if p == m.page {
			b.WriteString(styleTabOn.Render(label))
		} else {
			b.WriteString(styleTabOff.Render(label))
		}
	}

	// 右侧：登录状态。没登录时提示一下，否则「我喜欢」之类的页会莫名报错。
	right := ""
	if m.session.LoggedIn {
		right = styleDim.Render("♫ " + m.session.Nickname + " ")
	} else if m.session.Checked {
		right = styleWarn.Render("未登录 ") + styleDim.Render("(L 登录) ")
	}

	left := b.String()
	gap := m.w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return ansi.Truncate(left, m.w, "…")
	}
	return left + strings.Repeat(" ", gap) + right
}

// ---------- 底部播放条 ----------

// viewPlayerBar 是常驻的播放状态条：曲目、进度、音量、按键提示。
// 它不属于任何一页，切页时都能看到。
func (m Model) viewPlayerBar() string {
	width := m.w
	if width < 20 {
		width = 20
	}
	cur, hasCur := m.current()

	state := "▶"
	if !hasCur {
		state = "·"
	} else if m.paused {
		state = "⏸"
	}

	leftInfo := " 未在播放"
	if hasCur {
		leftInfo = fmt.Sprintf(" %s %s — %s", state, cur.Title, cur.Artist)
	}
	volTxt := fmt.Sprintf("♪ %d%%", m.volume)
	if m.muted {
		volTxt = "♪ 静音"
	}
	// 音质：正在放的这首实际拿到的档位优先（可能因为没有所选档位而降了一档），否则显示设置。
	q := m.quality
	if hasCur && m.playQuality != "" {
		q = m.playQuality
	}
	if q != "" {
		volTxt = qualityLabel(q) + "  " + volTxt
	}
	room := width - lipgloss.Width(volTxt) - 2
	if room < 4 {
		room = 4
	}
	line1 := styleText.Render(padRight(ansi.Truncate(leftInfo, room, "…"), room)) +
		"  " + styleDim.Render(volTxt)

	barW := width - 18
	if barW < 10 {
		barW = 10
	}
	pos := m.displayPos()
	line2 := fmt.Sprintf(" %s %s %s",
		styleDim.Render(formatTime(pos)),
		progressBar(pos, m.dur, barW),
		styleDim.Render(formatTime(m.dur)),
	)

	line3 := m.statusLine(width)

	lines := []string{line1, line2}
	lines = append(lines, m.miniLyrics(width)...)
	lines = append(lines, line3)
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// miniLyricsH 是进度条下面那几行歌词占的行数。
//
// 「正在播放」页本身就是整屏歌词，再在底下放一份是重复，所以那一页不显示；
// 终端太矮时先缩成一行、再干脆不显示，保证列表区还有地方。
func (m Model) miniLyricsH() int {
	if m.page == PageNow && m.focus == FocusList {
		return 0
	}
	switch {
	case m.h >= 30:
		return 3
	case m.h >= 20:
		return 1
	}
	return 0
}

// miniLyrics 渲染进度条下面的几行歌词：上一句、当前句（高亮，带翻译）、下一句。
// 行数固定为 miniLyricsH()，没有歌词时也占位，免得界面随切歌上下跳。
func (m Model) miniLyrics(width int) []string {
	n := m.miniLyricsH()
	if n == 0 {
		return nil
	}
	out := make([]string, n)
	mid := n / 2
	cur, ok := m.current()
	if !ok {
		return out
	}
	if len(cur.Lyrics) == 0 {
		out[mid] = styleDim.Render("   （暂无歌词）")
		return out
	}
	idx := m.currentLyricIndex(cur.Lyrics)
	for row := 0; row < n; row++ {
		i := idx - mid + row
		if i < 0 || i >= len(cur.Lyrics) {
			continue
		}
		l := cur.Lyrics[i]
		var line string
		if i == idx {
			line = stylePink.Render(" ♪ " + l.Text)
			if l.Translation != "" {
				line += styleDim.Render("  " + l.Translation)
			}
		} else {
			line = styleDim.Render("   " + l.Text)
		}
		out[row] = ansi.Truncate(line, width, "…")
	}
	return out
}

// statusLine 优先显示当前状态文字，没有状态就显示按键提示。
func (m Model) statusLine(width int) string {
	if m.status != "" {
		return ansi.Truncate(styleDim.Render(" "+m.status), width, "…")
	}
	hint := " 回车播放  a 加入队列  f 收藏  c 评论  / 搜索  ↑↓ 选择  -/+ 音量  ? 帮助 "
	switch m.page {
	case PageSearch:
		if m.focus == FocusSearch {
			hint = " " + inputHint
		} else {
			hint = " ctrl+t 切换类型  ] 下一页  回车播放/进入  a 加入队列  f 收藏  esc 退出 "
		}
	case PageNow:
		hint = " 空格 播放/暂停  ←→ ±5s  n/p 切歌  f 收藏  c 评论  ? 帮助 "
	case PageQueue:
		hint = " 回车播放  d 移除  c 清空队列  ↑↓ 选择  ? 帮助 "
	case PageMine:
		hint = " 回车打开  N 新建歌单  D 删除歌单  r 刷新  ? 帮助 "
	case PageExplore:
		if m.discoverLoading {
			hint = " 正在加载探索内容…  ? 帮助 "
		}
	}
	// 详情盖在任何页上时，提示以它为准。
	if m.detailActive() {
		hint = " 回车播放  a 加入队列  f 收藏  ] 下一页  esc 返回上一页  ? 帮助 "
	}
	return ansi.Truncate(styleDim.Render(hint), width, "…")
}

// ---------- 列表行 ----------

// trackRow 渲染列表里的一行曲目。
// 宽度按显示宽度算，中文歌名不会把右侧的时长挤歪。
func (m Model) trackRow(t Track, index int, selected bool, width int) string {
	if width < 20 {
		width = 20
	}

	playing := m.isCurrent(t)
	marker := "  "
	if playing {
		marker = styleActive.Render(m.playMarker())
	} else if selected {
		marker = styleActive.Render("▶ ")
	}

	num := styleDim.Render(fmt.Sprintf("%2d ", index+1))

	// 右侧固定部分：收藏标记 + 时长 + VIP 标记
	tail := ""
	if m.isFavorite(t.SongMid) {
		tail += stylePink.Render("♥ ")
	}
	if t.Vip && !t.Playable {
		tail += styleWarn.Render(" VIP ")
	}
	tail += styleDim.Render(formatTime(float64(t.Duration)) + " ")

	room := width - lipgloss.Width(marker) - lipgloss.Width(num) - lipgloss.Width(tail) - 1
	if room < 8 {
		room = 8
	}

	title := t.Title
	if t.Artist != "" {
		// 歌名占 3/5，歌手占 2/5；太窄时干脆不显示歌手。
		titleRoom := room * 3 / 5
		if titleRoom < 12 || room < 24 {
			titleRoom = room
		}
		artistRoom := room - titleRoom - 3
		titleLine := padRight(ansi.Truncate(title, titleRoom, "…"), titleRoom)
		if artistRoom > 3 {
			titleLine += " — " + padRight(ansi.Truncate(t.Artist, artistRoom, "…"), artistRoom)
		}
		title = titleLine
	} else {
		title = padRight(ansi.Truncate(title, room, "…"), room)
	}

	line := marker + num + title + " " + tail
	if selected {
		return styleRowOn.Render(padRight(line, width))
	}
	if playing {
		return styleActive.Render(padRight(line, width))
	}
	return styleText.Render(padRight(line, width))
}

func (m Model) playMarker() string {
	if m.paused {
		return "⏸ "
	}
	return "♪ "
}

// isCurrent 判断某首歌是不是当前正在播的那首。
// 用 SongMid 而不是指针或下标：同一首歌可能同时出现在搜索结果、歌单和队列里。
func (m Model) isCurrent(t Track) bool {
	cur, ok := m.current()
	if !ok {
		return false
	}
	if cur.SongMid == "" || t.SongMid == "" {
		return false
	}
	return cur.SongMid == t.SongMid
}

// ---------- 空态 ----------

// placeholder 画一个居中的空态/加载态/错误提示。
func placeholder(lines []string, w, h int) string {
	if h < 1 {
		h = 1
	}
	out := make([]string, 0, h)
	top := (h - len(lines)) / 2
	if top < 0 {
		top = 0
	}
	for i := 0; i < top; i++ {
		out = append(out, "")
	}
	out = append(out, lines...)
	return fitLines(out, w, h)
}

// errLines 把错误渲染成空态。
// 脚本给的中文错误原样显示，不自造文案。
func errLines(err error, retryHint string) []string {
	lines := []string{styleWarn.Render("出错了")}
	if err != nil {
		for _, l := range wrapText(err.Error(), 60) {
			lines = append(lines, styleText.Render(l))
		}
	}
	if retryHint != "" {
		lines = append(lines, "", styleDim.Render(retryHint))
	}
	return lines
}

// wrapText 按显示宽度折行（中文没有空格，只能按宽度硬折）。
func wrapText(s string, width int) []string {
	if width < 4 {
		width = 4
	}
	var out []string
	var cur string
	used := 0
	for _, r := range s {
		w := runeWidth(r)
		if used+w > width {
			out = append(out, cur)
			cur = ""
			used = 0
		}
		cur += string(r)
		used += w
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
