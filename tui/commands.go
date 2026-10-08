package main

import (
	"errors"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// 接口调用的条数上限。search 的服务端硬上限是 50，超过它 total/hasMore 的分页算术会错位，
// 所以这里夹在 30（一屏够用，也不用为翻页再写一套 UI）。
const (
	searchLimit    = 30
	favoritesLimit = 50
	playlistLimit  = 50
	commentsLimit  = 20
)

var errNoClient = errors.New("接口客户端未初始化")

// ---------- 消息 ----------
//
// 每个域带两种过期判据：
//   req      —— 请求令牌。挡「同一个列表连按两次刷新」的乱序返回。
//   语义键   —— query / songMid / 歌单 id。挡「切歌之后旧结果才回来」。

type searchMsg struct {
	req        uint64
	query      string
	kind       string // 发起时的搜索类型，回来时对账用（用户可能中途按了 ctrl+t）
	page       int
	appendMore bool // 这一批是「追加到已有结果」，不是替换
	res        *SearchResult
	err        error
}

type commentsMsg struct {
	req    uint64
	songID string
	sort   string
	page   int
	cursor string // 这一页是用哪个游标取的；记下来，往回翻时原样再用
	res    *CommentsResult
	err    error
}

// commentPraiseMsg 是点赞 / 取消点赞的结果。界面已经乐观更新过，失败时据此回滚。
type commentPraiseMsg struct {
	commentID string
	like      bool
	err       error
}

// commentAddMsg 是发评论的结果。
type commentAddMsg struct {
	songID  string
	pending bool
	err     error
}

type resolveMsg struct {
	req     uint64
	songMid string
	quality string // 请求的音质：连按 Q 时，只认和当前设置一致的那次结果
	got     string // 实际拿到的音质
	url     string
	err     error
}

type lyricsMsg struct {
	req     uint64
	songMid string
	lyrics  []Lyric
	err     error
}

type discoverMsg struct {
	req uint64
	res *DiscoverResult
	err error
}

type favoritesMsg struct {
	req uint64
	res *FavoritesResult
	err error
}

// favoriteMutationMsg 是加/删收藏的回执。action 带回来是为了对账：
// 用户可能在请求飞在路上时又按了一次 f，那时方向已经反了。
type favoriteMutationMsg struct {
	req    uint64
	track  Track
	action string
	res    *FavoriteResult
	err    error
}

// favMidsMsg 是「我喜欢」全量 songMid 的结果（启动 / 登录后拉一次，给 ♥ 用）。
type favMidsMsg struct {
	req uint64
	res *FavoritesResult
	err error
}

// logoutMsg 是退出登录的结果。
type logoutMsg struct{ err error }

type libraryMsg struct {
	req uint64
	res *LibraryResult
	err error
}

// playlistMsg 是某个歌单/专辑详情的返回。id 用来对账，免得 A 歌单的结果填进 B 歌单；
// appendMore 区分「整批替换」和「往后翻页追加」。
type playlistMsg struct {
	req        uint64
	id         string
	title      string
	from       Page
	page       int
	appendMore bool
	res        *PlaylistResult
	err        error
}

type sessionMsg struct {
	res *SessionStatus
	err error
}

// ---------- 命令生产者 ----------
//
// 全部走 tea.Cmd（异步）。mpv 的调用则相反——必须在 Update goroutine 里同步发，
// 理由见 queue.go 顶部。

// cmdSession 查一次登录状态。这是本地读 session.json，很便宜，启动时可以直接发。
func (m *Model) cmdSession() tea.Cmd {
	c, ctx := m.api, m.baseCtx
	if c == nil {
		return nil
	}
	return func() tea.Msg {
		res, err := c.SessionStatus(ctx)
		return sessionMsg{res: res, err: err}
	}
}

// cmdFavMids 拉「我喜欢」的全量 songMid。只要 mids：limit=1 让脚本只补一首歌的详情。
func (m *Model) cmdFavMids() tea.Cmd {
	m.favMidsReq++
	req := m.favMidsReq
	c, ctx := m.api, m.baseCtx
	if c == nil {
		return nil
	}
	return func() tea.Msg {
		res, err := c.Favorites(ctx, 1, 1)
		return favMidsMsg{req: req, res: res, err: err}
	}
}

// cmdLogout 退出登录：脚本删掉 session.json 及头像、二维码缓存。
func (m *Model) cmdLogout() tea.Cmd {
	c, ctx := m.api, m.baseCtx
	if c == nil {
		return nil
	}
	return func() tea.Msg { return logoutMsg{err: c.Logout(ctx)} }
}

func (m *Model) cmdSearch() tea.Cmd {
	return m.runSearch(1, false)
}

// runSearch 发起一次搜索。
//
// appendMore=false（新搜索 / 往回翻页）：清空当前结果、游标归零。
// appendMore=true（往后翻页）：把新一页接到已有结果后面，游标留在原地。
func (m *Model) runSearch(page int, appendMore bool) tea.Cmd {
	q := strings.TrimSpace(m.input.Value())
	if q == "" {
		q = strings.TrimSpace(m.searchQuery)
	}
	kind := m.searchType
	if kind == "" {
		kind = searchTypeSong
	}

	m.searchReq++
	req := m.searchReq
	m.searchQuery = q
	m.searchType = kind
	m.searchPage = page
	m.searchErr = nil

	if !appendMore {
		m.clearSearchResults()
		m.lists[PageSearch] = listState{}
		m.searchLoading = true
		m.searchLoadingMore = false
	} else {
		m.searchLoadingMore = true
	}

	if q == "" {
		m.searchLoading = false
		m.searchLoadingMore = false
		return nil
	}
	c, ctx := m.api, m.baseCtx
	if c == nil {
		m.searchErr = errNoClient
		m.searchLoading = false
		m.searchLoadingMore = false
		return nil
	}
	return func() tea.Msg {
		res, err := c.Search(ctx, q, kind, page, searchLimit)
		return searchMsg{req: req, query: q, kind: kind, page: page,
			appendMore: appendMore, res: res, err: err}
	}
}

// clearSearchResults 把四类结果一起清掉。切换类型时也必须调，
// 否则旧类型的数据会挂在界面上，看起来像「切了类型没反应」。
func (m *Model) clearSearchResults() {
	m.searchResults = nil
	m.searchSingers = nil
	m.searchAlbums = nil
	m.searchPlaylists = nil
	m.searchTotal = 0
	m.searchHasMore = false
}

// cycleSearchType 在 歌曲 → 歌手 → 专辑 → 歌单 之间循环切换。
// 已经有搜索结果时立刻用新类型重搜，否则只是换个标签。
func (m *Model) cycleSearchType() tea.Cmd {
	idx := 0
	for i, k := range searchTypeOrder {
		if k == m.searchType {
			idx = i
			break
		}
	}
	m.searchType = searchTypeOrder[(idx+1)%len(searchTypeOrder)]
	if strings.TrimSpace(m.searchQuery) != "" {
		return m.runSearch(1, false)
	}
	m.clearSearchResults()
	m.status = "搜索类型：" + searchTypeLabels[m.searchType]
	return nil
}

// searchNextPage 往后翻一页，结果追加。
func (m *Model) searchNextPage() tea.Cmd {
	if m.searchLoading || m.searchLoadingMore {
		return nil
	}
	if !m.searchHasMore {
		m.status = "已经是最后一页了"
		return nil
	}
	return m.runSearch(m.searchPage+1, true)
}

// searchPrevPage 往回翻一页。往回翻是整批替换（不拼接），
// 否则结果列表会越翻越长，翻回第 1 页反而要滚很久。
func (m *Model) searchPrevPage() tea.Cmd {
	if m.searchPage <= 1 {
		m.status = "已经是第一页了"
		return nil
	}
	return m.runSearch(m.searchPage-1, false)
}

// searchByName 用歌手名直接搜歌曲。歌手的曲目列表接口要签名（见 playSelected），
// 这是没有签名时唯一能给出的近似。
func (m *Model) searchByName(name string) tea.Cmd {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	m.searchType = searchTypeSong
	m.input.SetValue(name)
	m.gotoPage(PageSearch)
	m.focus = FocusList
	return m.runSearch(1, false)
}

func (m *Model) cmdDiscover() tea.Cmd {
	m.discoverReq++
	req := m.discoverReq
	m.discoverErr = nil
	c, ctx := m.api, m.baseCtx
	if c == nil {
		m.discoverErr = errNoClient
		return nil
	}
	m.discoverLoading = true
	return func() tea.Msg {
		res, err := c.Discover(ctx)
		return discoverMsg{req: req, res: res, err: err}
	}
}

func (m *Model) cmdFavorites() tea.Cmd {
	m.favoritesReq++
	req := m.favoritesReq
	m.favoritesErr = nil
	c, ctx := m.api, m.baseCtx
	if c == nil {
		m.favoritesErr = errNoClient
		return nil
	}
	m.favoritesLoading = true
	return func() tea.Msg {
		res, err := c.Favorites(ctx, 1, favoritesLimit)
		return favoritesMsg{req: req, res: res, err: err}
	}
}

// cmdToggleFavorite 加/删一首歌的「我喜欢」。
//
// action 由调用方按当前已知状态算好（add / remove），这里不自己判断：
// 判断要用的是**界面此刻显示的状态**，放到异步命令里读 m 会读到另一份快照。
//
// track 整条带进消息里：加成功之后要把它塞进 favorites 列表，
// 而命令跑完时用户可能已经换了页、选中行也变了，不能再回头去取。
func (m *Model) cmdToggleFavorite(track Track, guess string) tea.Cmd {
	m.favoriteReq++
	req := m.favoriteReq
	m.favoriteBusy = true
	c, ctx := m.api, m.baseCtx
	if c == nil {
		m.favoriteBusy = false
		m.status = errNoClient.Error()
		return nil
	}
	return func() tea.Msg {
		// 先实时问一次服务端：本地的收藏集合可能是旧的（列表接口有 CDN 缓存，
		// 或者刚在桌面客户端 / 手机上改过），按旧状态定方向会「取消」成「再加一次」。
		// 问不到才退回本地猜测。
		action := guess
		if fan, err := c.FavoriteCheck(ctx, track.SongMid); err == nil {
			action = "add"
			if fan {
				action = "remove"
			}
		}
		res, err := c.ToggleFavorite(ctx, action, track.SongMid, track.ID)
		return favoriteMutationMsg{
			req: req, track: track, action: action, res: res, err: err,
		}
	}
}

func (m *Model) cmdLibrary() tea.Cmd {
	m.libraryReq++
	req := m.libraryReq
	m.libraryErr = nil
	c, ctx := m.api, m.baseCtx
	if c == nil {
		m.libraryErr = errNoClient
		return nil
	}
	m.libraryLoading = true
	return func() tea.Msg {
		res, err := c.Library(ctx)
		return libraryMsg{req: req, res: res, err: err}
	}
}

// cmdDetail 打开一个歌单或专辑的详情。
//
// 歌单和专辑走同一个函数：脚本把两者的信封对齐了（都有 playlist/page/total/tracks/hasMore），
// 这里只在发请求时按 Kind 分派到 playlist / album 子命令。
// detailFrom 记住是从哪一页进去的——详情是盖在当前页上的，esc 就退回那一页。
func (m *Model) cmdDetail(pl Playlist) tea.Cmd {
	return m.runDetail(pl, 1, false)
}

func (m *Model) runDetail(pl Playlist, page int, appendMore bool) tea.Cmd {
	m.detailReq++
	req := m.detailReq
	m.detailID = pl.ID
	m.detailTitle = pl.Title
	m.detailFrom = m.page
	m.detailErr = nil
	m.detailPl = pl

	if !appendMore {
		m.detail = nil
		m.detailLoading = true
		m.detailLoadingMore = false
		m.lists[m.page] = listState{}
	} else {
		m.detailLoadingMore = true
	}

	c, ctx := m.api, m.baseCtx
	if c == nil {
		m.detailLoading = false
		m.detailLoadingMore = false
		m.detailErr = errNoClient
		return nil
	}
	from := m.detailFrom
	return func() tea.Msg {
		var res *PlaylistResult
		var err error
		if pl.Kind == "album" {
			res, err = c.Album(ctx, pl.ID, page, playlistLimit)
		} else {
			res, err = c.Playlist(ctx, pl.ID, pl.DirID, page, playlistLimit)
		}
		return playlistMsg{req: req, id: pl.ID, title: pl.Title, from: from,
			page: page, appendMore: appendMore, res: res, err: err}
	}
}

// cmdDetailMore 歌单/专辑详情的下一页。
func (m *Model) cmdDetailMore() tea.Cmd {
	d := m.detail
	if d == nil || m.detailLoading || m.detailLoadingMore {
		return nil
	}
	if !d.HasMore {
		m.status = "已经是最后一页了"
		return nil
	}
	return m.runDetail(m.detailPl, d.Page+1, true)
}

// cmdComments 取当前曲目的评论第 page 页（按当前排序）。
//
// 游标：第 1 页为空；往后翻用当前页最后一条的 SeqNo；往回翻用当初取那一页时记下的游标。
// 「最新」只能这样翻（服务端不带游标时忽略页码），「热评」带不带都行，统一处理。
func (m *Model) cmdComments(songID string, page int) tea.Cmd {
	m.commentsReq++
	req := m.commentsReq
	m.commentsSongID = songID
	m.commentsErr = nil
	cursor := ""
	if page <= 1 {
		page = 1
		m.comments = nil
		m.commentsList = listState{}
		m.commentsCursors = map[int]string{1: ""}
	} else if c, ok := m.commentsCursors[page]; ok {
		cursor = c
	} else if page == m.commentsPage+1 && len(m.comments) > 0 {
		cursor = m.comments[len(m.comments)-1].SeqNo
	}
	m.commentsPage = page
	sort := m.commentsSort
	if sort == "" {
		sort = "hot"
	}
	m.commentsLoading = true

	c, ctx := m.api, m.baseCtx
	if c == nil {
		m.commentsLoading = false
		m.commentsErr = errNoClient
		return nil
	}
	return func() tea.Msg {
		res, err := c.Comments(ctx, songID, sort, page, cursor, commentsLimit)
		return commentsMsg{req: req, songID: songID, sort: sort, page: page, cursor: cursor, res: res, err: err}
	}
}

// cmdCommentPraise 点赞 / 取消点赞。
func (m *Model) cmdCommentPraise(commentID string, like bool) tea.Cmd {
	c, ctx := m.api, m.baseCtx
	if c == nil {
		return nil
	}
	return func() tea.Msg {
		return commentPraiseMsg{commentID: commentID, like: like, err: c.CommentPraise(ctx, commentID, like)}
	}
}

// cmdCommentAdd 发评论。
func (m *Model) cmdCommentAdd(songID, content string) tea.Cmd {
	c, ctx := m.api, m.baseCtx
	if c == nil {
		return nil
	}
	return func() tea.Msg {
		pending, err := c.CommentAdd(ctx, songID, content)
		return commentAddMsg{songID: songID, pending: pending, err: err}
	}
}

// cmdResolve / cmdLyrics 共用同一个 req（由 startCurrent 分配），
// 所以「播放第 N 首」这一代的两条返回可以用同一个号码对账。
func (m *Model) cmdResolve(req uint64, t Track) tea.Cmd {
	c, ctx := m.api, m.baseCtx
	if c == nil {
		return nil
	}
	quality := m.quality
	return func() tea.Msg {
		res, err := c.Resolve(ctx, t.SongMid, t.MediaMid, quality)
		var url, got string
		if res != nil {
			url, got = res.URL, res.Quality
		}
		return resolveMsg{req: req, songMid: t.SongMid, quality: quality, got: got, url: url, err: err}
	}
}

func (m *Model) cmdLyrics(req uint64, t Track) tea.Cmd {
	c, ctx := m.api, m.baseCtx
	if c == nil {
		return nil
	}
	return func() tea.Msg {
		lyrics, err := c.Lyrics(ctx, t.SongMid)
		return lyricsMsg{req: req, songMid: t.SongMid, lyrics: lyrics, err: err}
	}
}

// cmdRefresh 刷新当前页。正在播放 / 队列没有远程数据，刷新它们等于什么都不做。
func (m *Model) cmdRefresh() tea.Cmd {
	// 歌单/专辑详情是盖在当前页上的一层，刷新的是它，不是底下那页。
	if m.detailActive() {
		return m.runDetail(m.detailPl, 1, false)
	}
	switch m.page {
	case PageExplore:
		return m.cmdDiscover()
	case PageSearch:
		return m.cmdSearch()
	case PageFavorites:
		return m.cmdFavorites()
	case PageLibrary, PageMine:
		return m.cmdLibrary()
	}
	return nil
}
