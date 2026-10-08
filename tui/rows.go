package main

import "fmt"

// 六个页面里的列表都抽象成 []row：分节标题、曲目、歌单、提示文字。
// 这样游标、滚动、渲染只需要写一遍，各页只负责「把自己那份数据摊平成行」。
//
// 有序敏感：游标是行数组的下标，所以可选性要显式标出来（分节标题不能停）。

type rowKind int

const (
	rowSection  rowKind = iota // 分节标题，不可选
	rowTrack                   // 一首歌，回车播放
	rowPlaylist                // 歌单/专辑，回车进入详情
	rowSinger                  // 歌手，回车按名字搜他的歌
	rowNote                    // 提示文字，不可选
)

type row struct {
	kind   rowKind
	text   string
	track  Track
	pl     Playlist
	singer Singer
}

func (r row) selectable() bool {
	return r.kind == rowTrack || r.kind == rowPlaylist || r.kind == rowSinger
}

// moveIn 在行数组里按 delta 行移动，跳过不可选的行。
// 用夹紧而不是回绕：到顶/到底就不动，和桌面客户端的手感一致。
//
// delta 可以大于 1（pgup/pgdn），所以先按行走到位、再朝运动方向吸附到可选行。
func (l *listState) moveIn(rows []row, delta, viewport int) {
	if len(rows) == 0 {
		l.cursor, l.offset = 0, 0
		return
	}
	dir := 0
	if delta != 0 {
		step := 1
		steps := delta
		if delta < 0 {
			step, dir, steps = -1, -1, -delta
		} else {
			dir = 1
		}
		idx := l.cursor
		for n := 0; n < steps; n++ {
			next := idx + step
			if next < 0 || next >= len(rows) {
				break
			}
			idx = next
		}
		l.cursor = idx
	}
	l.clamp(len(rows), viewport)
	l.snapToSelectable(rows, dir)
}

func (l *listState) toStart(rows []row) {
	for i := range rows {
		if rows[i].selectable() {
			l.cursor = i
			return
		}
	}
}

func (l *listState) toEnd(rows []row) {
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i].selectable() {
			l.cursor = i
			return
		}
	}
}

// snapToSelectable 在内容变化之后把游标吸附到最近的可选行上。
// 比如搜索结果从 30 条变成 3 条、或者游标原本停在分节标题上时。
//
// dir 是刚才的移动方向：向上移动时先往上找，否则会出现「按了 ↑ 却停在原地」。
func (l *listState) snapToSelectable(rows []row, dir int) {
	if len(rows) == 0 {
		l.cursor, l.offset = 0, 0
		return
	}
	if l.cursor < 0 {
		l.cursor = 0
	}
	if l.cursor >= len(rows) {
		l.cursor = len(rows) - 1
	}
	if rows[l.cursor].selectable() {
		return
	}
	if dir < 0 {
		for i := l.cursor - 1; i >= 0; i-- {
			if rows[i].selectable() {
				l.cursor = i
				return
			}
		}
	}
	// 先往下找，再往上找（往下更符合「刚刷新完列表」的直觉）。
	for i := l.cursor + 1; i < len(rows); i++ {
		if rows[i].selectable() {
			l.cursor = i
			return
		}
	}
	for i := l.cursor - 1; i >= 0; i-- {
		if rows[i].selectable() {
			l.cursor = i
			return
		}
	}
}

// selectedRow 返回当前选中的行；选中的不是可选行时返回 false。
func (m Model) selectedRow() (row, bool) {
	rows := m.rows()
	if len(rows) == 0 {
		return row{}, false
	}
	st := m.lists[m.page]
	if st.cursor < 0 || st.cursor >= len(rows) {
		return row{}, false
	}
	r := rows[st.cursor]
	if !r.selectable() {
		return row{}, false
	}
	return r, true
}

// rows 返回当前页面要显示的行。正在播放页不是列表，返回 nil。
//
// 歌单/专辑详情是「覆盖在当前页上」的：从哪一页进去，就在那一页显示详情，
// esc 退回那一页原来的列表。所以这里先判详情，再按页分发。
func (m Model) rows() []row {
	if m.detailActive() && m.detail != nil {
		return m.detailRows()
	}
	switch m.page {
	case PageExplore:
		return m.exploreRows()
	case PageSearch:
		return m.searchRows()
	case PageFavorites:
		return m.favoritesRows()
	case PageLibrary:
		return m.libraryRows()
	case PageMine:
		return m.mineRows()
	case PageQueue:
		return m.queueRows()
	}
	return nil
}

// detailActive 表示当前页正盖着一层详情——包括还在加载、或者加载失败的那两种，
// 因为这时候 esc 该退的是详情层，而不是底下那页。
func (m Model) detailActive() bool {
	if m.detailFrom != m.page {
		return false
	}
	return m.detail != nil || m.detailLoading || m.detailErr != nil || m.detailID != ""
}

// detailRows 渲染歌单/专辑详情的曲目列表。歌单和专辑共用一套，
// 因为脚本把两者的信封对齐成了同一个形状（见 fetch_album 的注释）。
func (m Model) detailRows() []row {
	d := m.detail
	kind := "歌单"
	if d.Playlist.Kind == "album" {
		kind = "专辑"
	}
	head := kind + " · " + d.Playlist.Title
	if d.Playlist.Creator != "" {
		head += " — " + d.Playlist.Creator
	}
	rows := []row{{kind: rowSection, text: head}}
	if len(d.Tracks) == 0 {
		rows = append(rows, row{kind: rowNote, text: "（这里是空的）"})
		return rows
	}
	for _, t := range d.Tracks {
		rows = append(rows, row{kind: rowTrack, track: t})
	}
	if d.HasMore {
		rows = append(rows, row{kind: rowNote,
			text: fmt.Sprintf("已显示 %d / %d 首，按 ] 看下一页", len(d.Tracks), d.Total)})
	}
	return rows
}

func (m Model) exploreRows() []row {
	var rows []row
	if d := m.discover; d != nil {
		if len(d.DailyTracks) > 0 {
			rows = append(rows, row{kind: rowSection,
				text: "每日 30 首" + suffixTitle(d.DailyPlaylist.Subtitle)})
			for _, t := range d.DailyTracks {
				rows = append(rows, row{kind: rowTrack, track: t})
			}
		}
		if len(d.GuessTracks) > 0 {
			rows = append(rows, row{kind: rowSection, text: "猜你喜欢"})
			for _, t := range d.GuessTracks {
				rows = append(rows, row{kind: rowTrack, track: t})
			}
		}
		if len(d.RadarTracks) > 0 {
			rows = append(rows, row{kind: rowSection, text: "为你探索"})
			for _, t := range d.RadarTracks {
				rows = append(rows, row{kind: rowTrack, track: t})
			}
		}
		if len(d.RecommendedPlaylists) > 0 {
			rows = append(rows, row{kind: rowSection, text: "推荐歌单"})
			for _, p := range d.RecommendedPlaylists {
				rows = append(rows, row{kind: rowPlaylist, pl: p})
			}
		}
	}
	return rows
}

// searchRows 按当前搜索类型摊平结果，末尾附一条翻页提示。
func (m Model) searchRows() []row {
	label := searchTypeLabels[m.searchType]
	rows := []row{{kind: rowSection,
		text: fmt.Sprintf("%s · “%s” 共 %d 条 · 第 %d 页", label, m.searchQuery, m.searchTotal, m.searchPage)}}

	switch m.searchType {
	case searchTypeSinger:
		for _, s := range m.searchSingers {
			rows = append(rows, row{kind: rowSinger, singer: s})
		}
	case searchTypeAlbum:
		for _, a := range m.searchAlbums {
			rows = append(rows, row{kind: rowPlaylist, pl: a})
		}
	case searchTypePlaylist:
		for _, p := range m.searchPlaylists {
			rows = append(rows, row{kind: rowPlaylist, pl: p})
		}
	default:
		for _, t := range m.searchResults {
			rows = append(rows, row{kind: rowTrack, track: t})
		}
	}

	if len(rows) == 1 {
		return nil
	}
	// 在输入框里时 ] 会被当成字符打进去，所以两个焦点下标不同的键。
	nextKey := "]"
	if m.focus == FocusSearch {
		nextKey = "ctrl+f"
	}
	switch {
	case m.searchLoadingMore:
		rows = append(rows, row{kind: rowNote, text: "正在加载下一页…"})
	case m.searchHasMore:
		rows = append(rows, row{kind: rowNote,
			text: fmt.Sprintf("按 %s 看下一页（已显示 %d 条）", nextKey, m.searchShown())})
	default:
		rows = append(rows, row{kind: rowNote, text: "已经是最后一页了"})
	}
	return rows
}

// searchShown 是当前已加载的条数（翻页是往里追加，不是替换）。
func (m Model) searchShown() int {
	switch m.searchType {
	case searchTypeSinger:
		return len(m.searchSingers)
	case searchTypeAlbum:
		return len(m.searchAlbums)
	case searchTypePlaylist:
		return len(m.searchPlaylists)
	default:
		return len(m.searchResults)
	}
}

func (m Model) favoritesRows() []row {
	if len(m.favorites) == 0 {
		return nil
	}
	rows := make([]row, 0, len(m.favorites)+2)
	rows = append(rows, row{kind: rowSection,
		text: fmt.Sprintf("我喜欢 · 共 %d 首", m.favoritesTotal)})
	for _, t := range m.favorites {
		rows = append(rows, row{kind: rowTrack, track: t})
	}
	// 服务端的「我喜欢」列表接口带 CDN 缓存，刚收藏的歌拉回来可能还是旧的。
	// 所以这里的列表按本地改动维护，可能落后于服务端——提前说明，
	// 免得用户看到「♥ 亮了但列表里没有」以为是 bug。
	rows = append(rows, row{kind: rowNote,
		text: "f 收藏 / 取消收藏；列表由本地维护，刚改动的内容服务端要过几分钟才会回传"})
	return rows
}

func (m Model) libraryRows() []row {
	var rows []row
	lib := m.library
	if lib == nil {
		return nil
	}
	// 歌单排在最前：「最近播放」动辄上百首，以前排第一，
	// 自己的歌单被压到列表最底下，看起来就像「音乐库里没有我的歌单」。
	if len(lib.CreatedPlaylists) > 0 {
		rows = append(rows, row{kind: rowSection, text: "创建的歌单"})
		for _, p := range lib.CreatedPlaylists {
			rows = append(rows, row{kind: rowPlaylist, pl: p})
		}
	}
	if len(lib.CollectedPlaylists) > 0 {
		rows = append(rows, row{kind: rowSection, text: "收藏的歌单"})
		for _, p := range lib.CollectedPlaylists {
			rows = append(rows, row{kind: rowPlaylist, pl: p})
		}
	}
	if len(lib.RecentTracks) > 0 {
		rows = append(rows, row{kind: rowSection, text: "最近播放"})
		for _, t := range lib.RecentTracks {
			rows = append(rows, row{kind: rowTrack, track: t})
		}
	}
	return rows
}

func (m Model) queueRows() []row {
	if len(m.queue) == 0 {
		return nil
	}
	rows := make([]row, 0, len(m.queue)+1)
	rows = append(rows, row{kind: rowSection,
		text: fmt.Sprintf("播放队列 · 共 %d 首", len(m.queue))})
	for _, t := range m.queue {
		rows = append(rows, row{kind: rowTrack, track: t})
	}
	return rows
}

func suffixTitle(s string) string {
	if s == "" {
		return ""
	}
	return " · " + s
}
