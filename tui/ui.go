package main

import (
	"context"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ---------- 消息 ----------

type tickMsg time.Time

// endedMsg 表示「当前曲目结束」。
// end.Gen 用来丢掉那些已经被手动切歌取代的结束通报（见 Mpv.Ended 的注释）。
type endedMsg struct {
	end MpvEnd
}

// sessionView 是顶栏要显示的登录状态。
type sessionView struct {
	Checked  bool // 查过一次没有（没查过时不显示「未登录」，免得闪一下）
	LoggedIn bool
	Nickname string
	Uin      string
}

// ---------- Model ----------

type Model struct {
	// ---- 播放 ----
	//
	// queue/curIndex 是播放队列。curIndex 为 -1 表示还没有当前曲目
	// （刚启动、或队列为空），所有取当前曲目的地方都要用 current()，
	// 不能直接索引——接了真实歌库之后空队列是常态。
	queue    []Track
	curIndex int

	mpv *Mpv
	// pub 把播放状态推给桌面 shell 的顶栏歌词；测试里的字面量 Model 没有它，
	// Publish 对 nil 接收者安全，所以不影响那些测试。
	pub *Publisher

	paused bool
	pos    float64
	dur    float64
	volume int
	muted  bool
	// mpvDead 保证「播放器已退出」只播报一次（见 onTick）。
	mpvDead bool

	// playReq 是「这一代播放」的号码。每次 startCurrent 自增，
	// 还在飞的 resolve / lyrics 结果对不上号就被丢掉。
	playReq uint64

	// ---- 界面 ----
	page       Page
	focus      Focus
	helpReturn Focus // 从哪个焦点打开的帮助页，关掉时回到那里
	lists      [pageCount]listState
	input      textInput
	status     string
	w, h       int

	// ---- 接口 ----
	api *Client
	// baseCtx 随程序退出取消；每次调用再在 Client.run 内部派生超时。
	// 两者分开的原因见 Client.run 的注释。
	baseCtx context.Context
	// autoLoad 表示「这是真实运行，启动就把探索页拉起来」。
	// 测试构造的 Model 不带它，所以 go test 不会真去打接口。
	autoLoad bool

	// ---- 会话 ----
	session sessionView
	// bootDiscover 表示「这次 session 查询是启动时那次，完了要接着拉探索页」。
	//
	// 为什么不在 autoLoadMsg 里直接 tea.Batch(session, discover)：两个子进程会同时
	// 调 ensure_valid_session，而会话需要续期时两边都会重写 session.json，
	// 偏偏 write_private_json 是裸 path.write_text（非原子）——
	// 并发写会把会话文件截断。串行这一步的成本只有一次本地查询的时间。
	bootDiscover bool

	// ---- 登录层 ----
	login       loginState
	loginType   string // "qq" / "wechat"
	loginMsg    string // 脚本返回的提示，原样显示
	loginErr    error
	qrLines     []string // 渲染好的二维码；缓存住，因为脚本成功后会把 PNG 删掉
	qrPath      string
	qrNote      string // 二维码降级的原因（放不下 / 解不出）
	loginReq    uint64
	loginCtxV   context.Context
	loginCancel context.CancelFunc

	// ---- 探索 ----
	discover        *DiscoverResult
	discoverErr     error
	discoverLoading bool
	discoverReq     uint64

	// ---- 搜索 ----
	//
	// 四类结果分开存。服务端一次只回一类（body 里四类 key 都在，但只有当前
	// search_type 的那个非空），所以在 Go 侧也保持四份，切换类型时整批换掉。
	searchType        string // song / singer / album / songlist
	searchQuery       string
	searchPage        int
	searchResults     []Track
	searchSingers     []Singer
	searchAlbums      []Playlist
	searchPlaylists   []Playlist
	searchTotal       int
	searchHasMore     bool
	searchErr         error
	searchLoading     bool
	searchLoadingMore bool // 翻页中（区别于首屏的「搜索中」）
	searchReq         uint64

	// ---- 我喜欢 ----
	favorites        []Track
	favoritesTotal   int
	favoritesErr     error
	favoritesLoading bool
	favoritesReq     uint64
	// favoriteBusy 挡重复提交：脚本一次只能写一笔，连按 f 会发两笔方向相反的请求，
	// 最后落地的顺序由网络决定，界面就会和账号对不上。
	favoriteBusy bool
	favoriteReq  uint64
	// favMids 是「我喜欢」的**全部** songMid（favorites 接口的 mids 字段一次给全，
	// 不受分页影响），♥ 和 f 键都按它判断。以前用的是 favorites 那一页曲目，
	// 没进过「我喜欢」页时它是空的，对已收藏的歌按 f 会再「加」一次、永远取消不掉。
	favMids    map[string]bool
	favMidsReq uint64
	// favLocal 记本次运行里亲手改过的收藏，优先于 favMids：
	// 列表接口有 CDN 缓存，刚改完重新拉回来的还是旧数据，不能让它把刚点亮的 ♥ 打回去。
	favLocal map[string]bool
	// loggingOut 挡连按：logout 子进程还没回来时不再发第二条。
	loggingOut bool

	// ---- 音乐库 ----
	library        *LibraryResult
	libraryErr     error
	libraryLoading bool
	libraryReq     uint64

	// ---- 歌单 / 专辑详情 ----
	//
	// 详情不再只属于音乐库页：搜索结果里的歌单、专辑、探索页的推荐歌单
	// 都能回车进去。detailFrom 记住是从哪一页进来的，
	// 这样 rows()/View/esc 都只认「当前页有没有详情」，不用给每页写一份。
	detail            *PlaylistResult
	detailPl          Playlist // 进详情时那一条，翻页要拿它重发请求（id/dirID/kind）
	detailFrom        Page
	detailID          string
	detailTitle       string
	detailErr         error
	detailLoading     bool
	detailLoadingMore bool // 详情翻页中
	detailReq         uint64

	// ---- 评论浮层 ----
	comments        []Comment
	commentsTotal   int
	commentsPage    int
	commentsHasMore bool
	commentsErr     error
	commentsLoading bool
	commentsReq     uint64
	commentsSongID  string
	commentsTrack   Track
	commentsList    listState
	commentsSort    string         // hot / new
	commentsCursors map[int]string // 页码 → 取那一页用的游标
	// 写评论：w 打开输入行，回车发送。
	commentWriting bool
	commentSending bool
	commentInput   textInput
	// commentPraising 挡同一条评论的连点：上一次还没回来时再点，方向就乱了。
	commentPraising map[string]bool

	// ---- 正在播放页 ----
	lyricFollow bool // true = 跟着播放位置滚动
	lyricScroll int  // 手动浏览时视口的第一行

	// posAt / posWall 把进度钉在墙上时钟上：mpv 的位置每 tick 才采一次，
	// 两次采样之间靠「距上次采样过了多久」补偿，否则界面上的歌词和进度条
	// 会一直比声音慢半拍（详见 onTick）。
	posAt   time.Time
	posWall float64

	// ---- 我的歌单 ----
	playlistBusy bool // 歌单写操作一次只发一笔
	dialog       dialogKind
	dialogInput  textInput
	dialogTarget Playlist
	pickerTrack  Track
	pickerList   listState
	pickerReturn bool // 新建歌单的对话框是从「加入歌单」浮层里打开的，关掉后回到浮层

	// ---- 音质 ----
	quality     string  // 设置的档位（存盘），std / 128 / 320 / flac
	playQuality string  // 正在放的这首实际拿到的档位
	resumeAt    float64 // >0：下一次解析回来后从这个位置载入（切音质时接着放）

	// ---- 专辑封面 ----
	covers *coverCache

	// ---- 上次播放状态 ----
	store          *Store
	restorePending bool // 恢复了上次的队列和位置，但还没真正开始放（mpv 里是空的）
	resumeRestore  bool // 这次带 resumeAt 的解析是「恢复播放」而不是「切音质」，提示语不同

	// mpris 把播放状态挂到会话总线上（灵动岛 / 锁屏 / 媒体键），nil 时不发布。
	mpris *MprisServer
}

// current 返回当前曲目。队列空或索引越界时第二个返回值为 false。
func (m Model) current() (Track, bool) {
	if m.curIndex < 0 || m.curIndex >= len(m.queue) {
		return Track{}, false
	}
	return m.queue[m.curIndex], true
}

// NewModel 构造主模型。这里**不联网**：接口调用全部由 Init / Update 返回的
// tea.Cmd 驱动，否则 go test 一构造 Model 就会真去打 QQ 音乐的接口。
func NewModel(mpv *Mpv) Model {
	m := newModelWithQueue(mpv, nil)
	m.quality = loadConfig().Quality
	m.api = NewClient()
	m.baseCtx = context.Background()
	m.autoLoad = true
	return m
}

// WithContext 让调用方（main）把「随程序退出取消」的 ctx 注进来。
func (m Model) WithContext(ctx context.Context) Model {
	m.baseCtx = ctx
	return m
}

// WithMpris 把 MPRIS 服务挂上来（main 里调用；测试不挂，免得占用真实会话总线）。
func (m Model) WithMpris(s *MprisServer) Model {
	m.mpris = s
	return m
}

// newModelWithQueue 允许注入队列（测试用本地音频替换网络音频）。
// 刻意不建 Client：测试里构造出来的 Model 不该有能力联网。
func newModelWithQueue(mpv *Mpv, queue []Track) Model {
	m := Model{
		queue:       queue,
		curIndex:    -1,
		mpv:         mpv,
		pub:         NewPublisher(),
		volume:      70,
		page:        PageExplore,
		focus:       FocusList,
		searchType:  searchTypeSong,
		quality:     defaultQuality,
		covers:      newCoverCache(),
		lyricFollow: true,
		baseCtx:     context.Background(),
	}
	if len(queue) > 0 {
		m.curIndex = 0
		m.dur = float64(queue[0].Duration)
		_ = m.startCurrent()
	}
	m.publish()
	return m
}

func (m Model) Init() tea.Cmd {
	// autoLoadMsg 走 Update 而不是在这里直接建命令：Init 收到的是 Model 的副本，
	// 在里面设的「加载中」标志会被丢掉，只有消息回到 Update 里改才留得住。
	return tea.Batch(tickCmd(), waitEnded(m.mpv), func() tea.Msg { return autoLoadMsg{} })
}

// autoLoadMsg 是启动信号：Update 收到它时把首页需要的数据拉起来。
type autoLoadMsg struct{}

// ---------- 尺寸 ----------

// bodyH 是页面内容区的高度（页签栏 1 行 + 播放条 3 行 + 进度条下的歌词若干行 + 面板上下边框 2 行）。
func (m Model) bodyH() int {
	n := m.h - 6 - m.miniLyricsH()
	if n < 3 {
		n = 3
	}
	return n
}

// listH 是当前页列表区能显示的行数。搜索页上面还有一个 3 行的输入框。
// 必须和 View 里传给 viewList 的高度一致，否则滚动会错位。
func (m Model) listH() int {
	n := m.bodyH()
	if m.page == PageSearch {
		n -= 3
	}
	if n < 1 {
		n = 1
	}
	return n
}

// ---------- 命令 ----------

// tickCmd 的周期。
//
// 200ms 而不是 500ms：mpv 的位置只在 tick 时采一次，界面上的歌词/进度条因此
// 平均要落后半个周期。500ms 时这半拍已经能听出来（歌词总比声音晚一点），
// 200ms 之后残差小到听不出。再快就没意义了——显示层另有 displayPos 的
// 墙钟补偿，把两次采样之间的空档也填上。
const tickInterval = 200 * time.Millisecond

func tickCmd() tea.Cmd {
	return tea.Tick(tickInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// waitEnded 挂一条「等 mpv 说当前曲目结束了」的命令。
// Ended 通道不会被关闭，所以这里会一直阻塞到真有事件或程序退出。
func waitEnded(m *Mpv) tea.Cmd {
	return func() tea.Msg {
		if m == nil {
			return nil
		}
		end, ok := <-m.Ended
		if !ok {
			return nil
		}
		return endedMsg{end: end}
	}
}

// ---------- 播放控制 ----------

func (m *Model) nextSong() tea.Cmd {
	if len(m.queue) == 0 {
		return nil
	}
	if m.curIndex < 0 {
		m.curIndex = 0
	} else {
		m.curIndex = (m.curIndex + 1) % len(m.queue)
	}
	return m.startCurrent()
}

func (m *Model) prevSong() tea.Cmd {
	if len(m.queue) == 0 {
		return nil
	}
	if m.curIndex < 0 {
		m.curIndex = 0
	} else {
		m.curIndex = (m.curIndex - 1 + len(m.queue)) % len(m.queue)
	}
	return m.startCurrent()
}

func (m *Model) togglePlay() {
	if m.mpv == nil {
		return
	}
	m.paused = !m.paused
	_ = m.mpv.SetPause(m.paused)
	if m.paused {
		m.status = "已暂停"
	} else {
		m.status = "播放中"
	}
}

func (m *Model) seekBy(delta float64) {
	if m.mpv == nil {
		return
	}
	target := m.displayPos() + delta
	if target < 0 {
		target = 0
	}
	if m.dur > 0 && target > m.dur {
		target = m.dur
	}
	_ = m.mpv.SeekAbs(target)
	m.anchorPos(target)
}

func (m *Model) setVolume(v int) {
	if v < 0 {
		v = 0
	}
	if v > 100 {
		v = 100
	}
	m.volume = v
	if m.muted && v > 0 {
		m.muted = false
	}
	if m.mpv != nil {
		_ = m.mpv.SetVolume(v)
	}
	m.status = "音量 " + strconv.Itoa(v) + "%"
}

func (m *Model) toggleMute() {
	m.muted = !m.muted
	if m.mpv == nil {
		return
	}
	if m.muted {
		_ = m.mpv.SetVolume(0)
		m.status = "已静音"
	} else {
		_ = m.mpv.SetVolume(m.volume)
		m.status = "已取消静音"
	}
}

func itoa(v int) string { return strconv.Itoa(v) }

// ---------- Update ----------

// Update 在具体的消息处理（update）之后统一检查一次「该不该去下载封面」：
// 切到正在播放页、切歌、改窗口大小都可能需要新封面，挨个入口加不如在这里收口。
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.update(msg)
	nm, ok := next.(Model)
	if !ok {
		return next, cmd
	}
	if c := nm.ensureCover(); c != nil {
		return nm, tea.Batch(cmd, c)
	}
	return nm, cmd
}

func (m Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.reflow()
		return m, nil

	case tickMsg:
		m = m.onTick()
		return m, tea.Batch(tickCmd(), m.lyricWakeCmd())

	case mprisMsg:
		nm, cmd := m.onMpris(msg)
		nm.publish()
		return nm, cmd

	case playlistOpMsg:
		nm, cmd := m.onPlaylistOp(msg)
		return nm, cmd

	case coverMsg:
		return m.onCover(msg), nil

	case lyricWakeMsg:
		// 只是为了在换行那一刻重绘一次；位置由 displayPos 按墙钟算，不用再问 mpv。
		return m, nil

	case autoLoadMsg:
		// 测试构造的 Model 没有 autoLoad，所以 go test 不会真去打接口。
		if !m.autoLoad || m.api == nil {
			return m, nil
		}
		// 只发 session；探索页等它回来再发（见 bootDiscover 的注释）。
		m.bootDiscover = true
		// 恢复出来的那首歌顺手把歌词拉回来（歌词接口不碰登录会话，和 session 并发没问题）。
		if cur, ok := m.current(); ok && m.restorePending && len(cur.Lyrics) == 0 {
			return m, tea.Batch(m.cmdSession(), m.cmdLyrics(m.playReq, cur))
		}
		return m, m.cmdSession()

	case endedMsg:
		return m.onEnded(msg)

	case tea.KeyMsg:
		next, cmd := m.handleKey(msg)
		nm, ok := next.(Model)
		if !ok {
			return next, cmd
		}
		nm.publish()
		return nm, cmd

	case searchMsg:
		return m.onSearch(msg)

	case resolveMsg:
		return m.onResolve(msg)

	case lyricsMsg:
		return m.onLyrics(msg)

	case discoverMsg:
		m.discoverLoading = false
		if msg.req != m.discoverReq {
			return m, nil
		}
		if msg.err != nil {
			m.discoverErr = msg.err
			return m, nil
		}
		m.discoverErr = nil
		m.discover = msg.res
		m.reflow()
		return m, nil

	case favoritesMsg:
		m.favoritesLoading = false
		if msg.req != m.favoritesReq {
			return m, nil
		}
		if msg.err != nil {
			m.favoritesErr = msg.err
			return m, nil
		}
		m.favoritesErr = nil
		if msg.res != nil {
			m.favorites = msg.res.Tracks
			m.favoritesTotal = msg.res.Total
			m.setFavMids(msg.res.Mids)
		}
		m.reflow()
		return m, nil

	case favMidsMsg:
		if msg.req != m.favMidsReq || msg.err != nil || msg.res == nil {
			return m, nil // 拿不到就沿用旧的；按 f 时会再实时问一次服务端，不靠它定方向
		}
		m.setFavMids(msg.res.Mids)
		return m, nil

	case favoriteMutationMsg:
		m.favoriteBusy = false
		// 旧请求的结果照样落地：服务端那边已经改了，忽略它只会让界面更假。
		// 只跳过状态栏文案——那是给最后一次操作看的。
		stale := msg.req != m.favoriteReq
		if msg.err != nil {
			if !stale {
				m.status = "收藏失败：" + msg.err.Error()
			}
			return m, nil
		}
		if msg.res == nil {
			return m, nil
		}
		mid := strings.TrimSpace(msg.res.SongMid)
		if mid == "" {
			mid = msg.track.SongMid
		}
		was := m.isFavorite(mid)
		if m.favLocal == nil {
			m.favLocal = map[string]bool{}
		}
		m.favLocal[mid] = msg.res.Favorite
		inList := false
		for _, t := range m.favorites {
			if t.SongMid == mid {
				inList = true
				break
			}
		}
		if msg.res.Favorite {
			if !inList && m.favorites != nil {
				m.favorites = append(m.favorites, msg.track)
			}
			if !was {
				m.favoritesTotal++
			}
		} else {
			m.favorites = removeTrackByMid(m.favorites, mid)
			if was && m.favoritesTotal > 0 {
				m.favoritesTotal--
			}
		}
		if !stale {
			if msg.res.Favorite {
				m.status = "已加入我喜欢：" + msg.track.Title
			} else {
				m.status = "已从我喜欢移除：" + msg.track.Title
			}
		}
		m.reflow()
		return m, nil

	case libraryMsg:
		m.libraryLoading = false
		if msg.req != m.libraryReq {
			return m, nil
		}
		if msg.err != nil {
			m.libraryErr = msg.err
			return m, nil
		}
		m.libraryErr = nil
		m.library = msg.res
		m.reflow()
		return m, nil

	case playlistMsg:
		if msg.appendMore {
			m.detailLoadingMore = false
		} else {
			m.detailLoading = false
		}
		// req 挡乱序，id 挡「切了歌单之后旧结果才回来」。
		if msg.req != m.detailReq || msg.id != m.detailID {
			return m, nil
		}
		if msg.err != nil {
			m.detailErr = msg.err
			return m, nil
		}
		m.detailErr = nil
		if msg.res != nil {
			// 追加页：只把曲目接在后面、更新分页元信息，不换 Playlist 头
			// （服务端每页都会回一遍头，用第二页的会把已有的字段覆盖成空）。
			if msg.appendMore && m.detail != nil {
				m.detail.Tracks = append(m.detail.Tracks, msg.res.Tracks...)
				m.detail.Page = msg.res.Page
				m.detail.HasMore = msg.res.HasMore
				if msg.res.Total > 0 {
					m.detail.Total = msg.res.Total
				}
			} else {
				m.detail = msg.res
				m.detailFrom = msg.from
				m.lists[m.page] = listState{}
			}
		}
		m.reflow()
		return m, nil

	case commentsMsg:
		m.commentsLoading = false
		if msg.req != m.commentsReq || msg.songID != m.commentsSongID || msg.sort != m.commentsSortOrHot() {
			return m, nil
		}
		if msg.err != nil {
			m.commentsErr = msg.err
			return m, nil
		}
		m.commentsErr = nil
		if msg.res != nil {
			m.comments = msg.res.Comments
			m.commentsTotal = msg.res.Total
			m.commentsPage = msg.res.Page
			m.commentsHasMore = msg.res.HasMore
			if m.commentsCursors == nil {
				m.commentsCursors = map[int]string{}
			}
			m.commentsCursors[msg.page] = msg.cursor
		}
		m.commentsList.clamp(len(m.comments), m.commentsViewH())
		return m, nil

	case commentPraiseMsg:
		delete(m.commentPraising, msg.commentID)
		if msg.err == nil {
			if msg.like {
				m.status = "已点赞"
			} else {
				m.status = "已取消点赞"
			}
			return m, nil
		}
		// 失败：把乐观更新撤回来（那条评论可能已经翻页翻走了，找不到就算了）。
		for i := range m.comments {
			if m.comments[i].ID == msg.commentID {
				m.applyPraise(i, !msg.like)
			}
		}
		m.status = "点赞失败：" + msg.err.Error()
		return m, nil

	case commentAddMsg:
		m.commentSending = false
		if msg.err != nil {
			// 输入保留，用户改一改可以直接重发。
			m.status = "评论发送失败：" + msg.err.Error()
			return m, nil
		}
		m.commentWriting = false
		m.commentInput.Clear()
		if msg.pending {
			m.status = "评论已提交，等待审核"
			return m, nil
		}
		m.status = "评论已发送"
		// 切到「最新」的第一页，刚发的那条就在最上面。
		if msg.songID == m.commentsSongID && m.focus == FocusComments {
			m.commentsSort = "new"
			return m, m.cmdComments(m.commentsSongID, 1)
		}
		return m, nil

	case sessionMsg:
		m.session.Checked = true
		if msg.err == nil && msg.res != nil {
			m.session.LoggedIn = msg.res.LoggedIn
			m.session.Nickname = msg.res.Nickname
			m.session.Uin = msg.res.Uin
		}
		// 启动时那次查询回来之后才拉探索页，避免两个子进程同时续期写坏 session.json。
		// 收藏集合跟探索页一起发：session-status 刚续过期，这两条不会再同时续期。
		if m.bootDiscover {
			m.bootDiscover = false
			if m.session.LoggedIn {
				return m, tea.Batch(m.cmdDiscover(), m.cmdFavMids())
			}
			return m, m.cmdDiscover()
		}
		return m, nil

	case logoutMsg:
		m.loggingOut = false
		if msg.err != nil {
			m.status = "退出登录失败：" + msg.err.Error()
			return m, nil
		}
		m.session = sessionView{Checked: true}
		m.clearAccountData()
		m.closeLogin()
		m.status = "已退出登录"
		m.reflow()
		// 探索页的推荐按登录态算，换成游客态重新拉。
		return m, m.cmdDiscover()

	case loginQRMsg:
		return m.onLoginQR(msg)

	case loginPollMsg:
		return m.onLoginPoll(msg)

	case loginPollTickMsg:
		// 登录层已经关了、或者号码变了 → 这个闹钟作废。
		if msg.req != m.loginReq || m.focus != FocusLogin {
			return m, nil
		}
		return m, m.cmdLoginPoll()
	}
	return m, nil
}

func (m Model) onTick() Model {
	if m.mpv == nil {
		return m
	}
	// 恢复出来、还没开始放：mpv 里是空的，读到的 0 秒会把恢复的位置冲掉。
	if m.restorePending {
		m.publish()
		return m
	}
	// mpv 被外部杀掉或自己崩了：明确告诉用户，别让人对着没声音的界面发懵。
	if m.mpv.Dead() {
		// 只播报一次，之后让用户的其它操作（按 r 之类）能正常改状态栏，
		// 否则每 500ms 的 tick 都会把它压回去。
		if !m.mpvDead {
			m.mpvDead = true
			m.status = "播放器 mpv 已退出，请按 q 退出后重启"
		}
		m.paused = true
		m.publish()
		return m
	}
	m.pos = m.mpv.TimePos()
	m.dur = m.mpv.Duration()
	m.paused = m.mpv.Paused()
	// 记下「这个位置是在什么时刻采到的」，displayPos 靠它把两次采样之间
	// 流逝的时间补回去。只发布原始采样值，墙钟补偿留给显示层做——
	// 发布补偿过的值会让桌面侧再补一次，变成跑在前面。
	m.posAt = time.Now()
	m.posWall = m.pos
	m.publish()
	return m
}

// displayPos 返回界面上应该显示的播放位置。
//
// 为什么不能直接用 m.pos：那是 mpv 上次被采样的值，平均落后 tickInterval/2。
// 播放中就把「距上次采样过了多久」加上去；暂停/出错时不能加，
// 否则暂停之后进度条还会自己往前爬。
func (m Model) displayPos() float64 {
	if m.paused || m.mpvDead || m.mpv == nil {
		return m.pos
	}
	// 外推量封顶：正常情况下下一次 tick 早就重新采样了；封顶是为了
	// mpv 还在缓冲 / 解析直链时（time-pos 不动）界面不会自己往前跑。
	elapsed := time.Since(m.posAt).Seconds()
	if limit := 2 * tickInterval.Seconds(); elapsed > limit {
		elapsed = limit
	}
	pos := m.posWall + elapsed
	if m.dur > 0 && pos > m.dur {
		pos = m.dur
	}
	if pos < 0 {
		pos = 0
	}
	return pos
}

// lyricWakeMsg 是「下一行歌词该亮了」的闹钟。
type lyricWakeMsg struct{}

// lyricWakeCmd 在下一行歌词的切换时刻落在本次 tick 周期内时，掐着那一刻补一次重绘。
//
// 光靠 200ms 的 tick，换行平均要晚 100ms、最坏晚 200ms；把采样周期再缩短
// 又得每次多问 mpv 三个属性。这里只在真正需要换行的那一刻多醒一次，
// 闹钟本身不再续订，所以不会越积越多。
func (m Model) lyricWakeCmd() tea.Cmd {
	if m.paused || m.mpvDead || m.mpv == nil {
		return nil
	}
	cur, ok := m.current()
	if !ok || len(cur.Lyrics) == 0 {
		return nil
	}
	pos := m.displayPos() + lyricLead
	for _, l := range cur.Lyrics {
		if l.Time <= pos {
			continue
		}
		wait := time.Duration((l.Time - pos) * float64(time.Second))
		if wait > tickInterval {
			return nil // 下一次 tick 会处理
		}
		// 多等 5ms，保证醒来时 displayPos 已经越过时间戳，不会差一点没切过去。
		return tea.Tick(wait+5*time.Millisecond, func(time.Time) tea.Msg { return lyricWakeMsg{} })
	}
	return nil
}

// anchorPos 在任何「位置被我们主动改动」的地方重置墙钟锚点（seek / 切歌）。
func (m *Model) anchorPos(pos float64) {
	m.pos = pos
	m.posWall = pos
	m.posAt = time.Now()
}

func (m Model) onEnded(msg endedMsg) (tea.Model, tea.Cmd) {
	if m.mpv == nil {
		return m, nil
	}
	// 代际对不上 = 这条结束通报属于已经被切掉的那一首，丢掉。
	// 不丢的话，滞留在通道里的自然播完信号会在用户手动切歌之后才被处理，
	// 于是白白多跳一首。
	if msg.end.Gen != m.mpv.LoadSeq() {
		return m, waitEnded(m.mpv)
	}
	if msg.end.Err != "" {
		// 直链过期之类的失败：以前这种情况完全没人管，界面会永远停在
		// 「正在播放」而其实早就没声了。
		m.status = "播放中断（" + msg.end.Err + "），按回车重新播放"
		m.paused = true
		m.publish()
		return m, waitEnded(m.mpv)
	}
	cmd := m.nextSong()
	m.publish()
	return m, tea.Batch(cmd, waitEnded(m.mpv))
}

func (m Model) onSearch(msg searchMsg) (tea.Model, tea.Cmd) {
	m.searchLoading = false
	m.searchLoadingMore = false
	// req 挡乱序，query+kind 挡「改了关键词/换了类型之后旧结果才回来」。
	if msg.req != m.searchReq || msg.query != m.searchQuery || msg.kind != m.searchType {
		return m, nil
	}
	if msg.err != nil {
		m.searchErr = msg.err
		if !msg.appendMore {
			m.clearSearchResults()
		}
		return m, nil
	}
	m.searchErr = nil
	if msg.res != nil {
		m.searchTotal = msg.res.Total
		m.searchHasMore = msg.res.HasMore
		m.searchPage = msg.res.Page
		// 翻页是往同一个列表里追加；换了歌单/歌手/专辑类型时那边本来就是清空的。
		if msg.appendMore {
			switch m.searchType {
			case searchTypeSinger:
				m.searchSingers = append(m.searchSingers, msg.res.Singers...)
			case searchTypeAlbum:
				m.searchAlbums = append(m.searchAlbums, msg.res.Albums...)
			case searchTypePlaylist:
				m.searchPlaylists = append(m.searchPlaylists, msg.res.Playlists...)
			default:
				m.searchResults = append(m.searchResults, msg.res.Tracks...)
			}
		} else {
			m.searchResults = msg.res.Tracks
			m.searchSingers = msg.res.Singers
			m.searchAlbums = msg.res.Albums
			m.searchPlaylists = msg.res.Playlists
		}
	}

	if msg.appendMore {
		// 追加不重置游标——用户正在后面翻页，把他弹回第一条最烦人。
		m.lists[PageSearch].clamp(len(m.rows()), m.listH())
		return m, nil
	}

	// 结果换了，游标从头开始，并吸附到第一条可选行上（第 0 行是分节标题）。
	st := listState{}
	st.snapToSelectable(m.rows(), 1)
	st.clamp(len(m.rows()), m.listH())
	m.lists[PageSearch] = st

	// 焦点还在输入框里时，回车是「搜索」而不是「播放」——不说清楚的话，
	// 用户会以为回车坏了。esc 之后回车才是播放。
	if m.focus == FocusSearch {
		m.status = "回车 = 搜索；Esc 退出输入后，回车 = 播放、a = 加入队列"
	} else {
		m.status = ""
	}
	return m, nil
}

func (m Model) onResolve(msg resolveMsg) (tea.Model, tea.Cmd) {
	cur, ok := m.current()
	// 号码对不上、或者当前曲目已经换人了 → 这次解析属于上一首，丢掉。
	if !ok || msg.req != m.playReq || msg.songMid != cur.SongMid || msg.quality != m.quality {
		return m, nil
	}
	resume := m.resumeAt
	m.resumeAt = 0
	restoring := m.resumeRestore
	m.resumeRestore = false
	if msg.err != nil {
		if resume > 0 && !restoring {
			// 切音质失败：原来那个流还在放，不打断，只报一声。
			m.status = "切换音质失败：" + msg.err.Error()
			return m, nil
		}
		m.status = "无法播放「" + cur.Title + "」：" + msg.err.Error()
		m.paused = true
		m.publish()
		return m, nil
	}
	if m.mpv == nil || msg.url == "" {
		return m, nil
	}
	m.playQuality = msg.got
	gotNote := ""
	if msg.got != "" && msg.got != msg.quality {
		gotNote = "（这首没有" + qualityLabel(msg.quality) + "，用的是" + qualityLabel(msg.got) + "）"
	}
	if resume > 0 {
		// 切音质：在原位置载入新流，暂停状态保持不变。
		if err := m.mpv.LoadAt(msg.url, resume); err != nil {
			m.status = "切换音质失败: " + err.Error()
			return m, nil
		}
		_ = m.mpv.SetPause(m.paused)
		m.anchorPos(resume)
		m.status = "已切换到" + qualityLabel(msg.got) + gotNote
		if restoring {
			m.status = "已从 " + formatTime(resume) + " 接着播放：" + cur.Title + gotNote
		}
		m.publish()
		return m, nil
	}
	if err := m.mpv.Load(msg.url); err != nil {
		m.status = "载入失败: " + err.Error()
		return m, nil
	}
	_ = m.mpv.SetPause(false)
	m.paused = false
	m.status = "正在播放: " + cur.Title + gotNote
	m.publish()
	return m, nil
}

func (m Model) onLyrics(msg lyricsMsg) (tea.Model, tea.Cmd) {
	cur, ok := m.current()
	if !ok || msg.req != m.playReq || msg.songMid != cur.SongMid {
		return m, nil
	}
	if msg.err != nil {
		// 歌词拿不到不该打断播放，也不值得弹错——界面上「（这首歌没有歌词）」
		// 已经把情况说清楚了。
		return m, nil
	}
	if m.curIndex >= 0 && m.curIndex < len(m.queue) {
		m.queue[m.curIndex].Lyrics = msg.lyrics
	}
	return m, nil
}

// reflow 在尺寸或内容变化之后重新夹紧所有列表的游标。
func (m *Model) reflow() {
	for p := Page(0); p < pageCount; p++ {
		saved := m.page
		m.page = p
		if p != PageNow {
			m.lists[p].snapToSelectable(m.rows(), 0)
			m.lists[p].clamp(len(m.rows()), m.listH())
		}
		m.page = saved
	}
}

// ---------- 按键 ----------

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// 全局无条件：任何焦点下 ctrl+c 都退出。
	if key == "ctrl+c" {
		return m, tea.Quit
	}

	switch m.focus {
	case FocusSearch:
		return m.handleSearchKey(msg)
	case FocusLogin:
		return m.handleLoginKey(msg)
	case FocusComments:
		return m.handleCommentsKey(msg)
	case FocusPicker:
		return m.handlePickerKey(msg)
	case FocusDialog:
		return m.handleDialogKey(msg)
	case FocusHelp:
		m.focus = m.helpReturn
		return m, nil
	}
	return m.handleListKey(msg)
}

// handleSearchKey 是 fzf 模型：只有少数几个键是命令，其余全部进输入框。
// 这样在搜索框里按 q 打出的是字母 q，而不是退出程序。
func (m Model) handleSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.focus = FocusList
		return m, nil
	case "enter":
		return m, m.cmdSearch()
	case "ctrl+t":
		// 切搜索类型。这是命令键，不能进输入框。
		return m, m.cycleSearchType()
	case "ctrl+f":
		return m, m.searchNextPage()
	case "ctrl+b":
		return m, m.searchPrevPage()
	case "tab":
		// 输入框里也要能翻页，否则进了搜索页就被困住了。
		return m, m.enterPage((m.page + 1) % pageCount)
	case "shift+tab":
		return m, m.enterPage((m.page + pageCount - 1) % pageCount)
	case "up", "ctrl+p":
		m.moveList(-1)
	case "down", "ctrl+n":
		m.moveList(1)
	case "pgup":
		m.moveList(-m.listH())
	case "pgdown":
		m.moveList(m.listH())
	default:
		m.inputKey(msg)
	}
	return m, nil
}

// handleCommentsKey 是评论浮层的按键。浮层是只读的，能做的只有滚动和翻页。
func (m Model) handleCommentsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// 写评论时和搜索框一样是 fzf 语义：除了回车、esc，其余键全部进输入行。
	if m.commentWriting {
		switch msg.Type {
		case tea.KeyEsc:
			m.commentWriting = false
			m.status = "已取消写评论（草稿保留，按 w 继续）"
		case tea.KeyEnter:
			text := strings.TrimSpace(m.commentInput.Value())
			if text == "" || m.commentSending {
				return m, nil
			}
			m.commentSending = true
			m.status = "正在发送评论…"
			return m, m.cmdCommentAdd(m.commentsSongID, text)
		default:
			editInput(&m.commentInput, msg)
		}
		return m, nil
	}

	switch msg.String() {
	case "s":
		if m.commentsSort == "new" {
			m.commentsSort = "hot"
			m.status = "评论：热评"
		} else {
			m.commentsSort = "new"
			m.status = "评论：最新"
		}
		return m, m.cmdComments(m.commentsSongID, 1)
	case "l":
		return m, m.toggleCommentPraise()
	case "w":
		if m.session.Checked && !m.session.LoggedIn {
			m.status = "写评论需要先登录，按 esc 关掉评论后按 L 扫码"
			return m, nil
		}
		m.commentWriting = true
		return m, nil
	case "esc", "q":
		m.focus = FocusList
	case "up", "k":
		m.moveComments(-1)
	case "down", "j":
		m.moveComments(1)
	case "pgup":
		m.moveComments(-m.commentsViewH())
	case "pgdown":
		m.moveComments(m.commentsViewH())
	case "g", "home":
		m.commentsList.cursor = 0
		m.commentsList.clamp(len(m.comments), m.commentsViewH())
	case "G", "end":
		m.commentsList.cursor = len(m.comments) - 1
		m.commentsList.clamp(len(m.comments), m.commentsViewH())
	case "]":
		return m, m.commentsNextPage()
	case "[":
		return m, m.commentsPrevPage()
	case "r":
		return m, m.cmdComments(m.commentsSongID, 1)
	}
	return m, nil
}

func (m Model) commentsSortOrHot() string {
	if m.commentsSort == "new" {
		return "new"
	}
	return "hot"
}

// toggleCommentPraise 给选中的评论点赞 / 取消点赞。
// 先乐观更新（数字和标记立刻变），失败再回滚。
func (m *Model) toggleCommentPraise() tea.Cmd {
	i := m.commentsList.cursor
	if i < 0 || i >= len(m.comments) {
		return nil
	}
	if m.session.Checked && !m.session.LoggedIn {
		m.status = "点赞需要先登录，按 esc 关掉评论后按 L 扫码"
		return nil
	}
	c := m.comments[i]
	if c.ID == "" {
		return nil
	}
	if m.commentPraising[c.ID] {
		m.status = "上一次点赞还没回来，稍等一下"
		return nil
	}
	if m.commentPraising == nil {
		m.commentPraising = map[string]bool{}
	}
	m.commentPraising[c.ID] = true
	like := !c.IsPraised
	m.applyPraise(i, like)
	return m.cmdCommentPraise(c.ID, like)
}

// applyPraise 把第 i 条评论改成「赞过 / 没赞过」，赞数跟着加减。
func (m *Model) applyPraise(i int, like bool) {
	c := &m.comments[i]
	if c.IsPraised == like {
		return
	}
	c.IsPraised = like
	if like {
		c.Likes++
	} else if c.Likes > 0 {
		c.Likes--
	}
}

// moveComments 滚动评论列表。评论全是单行文本、没有不可选的标题行，所以直接夹紧即可。
func (m *Model) moveComments(delta int) {
	m.commentsList.move(delta, len(m.comments), m.commentsViewH())
}

// commentsNextPage 往后翻一页评论。和搜索一样是整批替换而不是追加——
// 一页 20 条，追加下去「第几页」就没有意义了。
func (m *Model) commentsNextPage() tea.Cmd {
	if m.commentsLoading {
		return nil
	}
	if !m.commentsHasMore {
		m.status = "已经是最后一页了"
		return nil
	}
	return m.cmdComments(m.commentsSongID, m.commentsPage+1)
}

func (m *Model) commentsPrevPage() tea.Cmd {
	if m.commentsPage <= 1 {
		m.status = "已经是第一页了"
		return nil
	}
	return m.cmdComments(m.commentsSongID, m.commentsPage-1)
}

func (m Model) handleListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// 数字键切页。放在最前面，免得和别的单键撞。
	if len(key) == 1 && key[0] >= '1' && key[0] < '1'+byte(pageCount) {
		return m, m.enterPage(Page(key[0] - '1'))
	}

	switch key {
	case "q":
		return m, tea.Quit

	case "tab":
		return m, m.enterPage((m.page + 1) % pageCount)
	case "shift+tab":
		return m, m.enterPage((m.page + pageCount - 1) % pageCount)

	case "?":
		m.helpReturn = m.focus
		m.focus = FocusHelp
		return m, nil

	case "/":
		cmd := m.enterPage(PageSearch)
		m.focus = FocusSearch
		return m, cmd
	case "L":
		m.focus = FocusLogin
		if m.loginType == "" {
			m.loginType = "qq"
		}
		// 已登录时先给账号页（看当前是谁、换号、退出登录），
		// 而不是直接甩一张新二维码——那样看起来像登录态丢了。
		if m.session.Checked && m.session.LoggedIn {
			m.cancelLogin()
			m.login = loginAccount
			m.loginErr = nil
			m.loginMsg = ""
			return m, nil
		}
		return m, m.cmdLoginQR()

	case " ":
		if m.restorePending {
			return m, m.resumeRestored()
		}
		m.togglePlay()
	case "n":
		return m, m.nextSong()
	case "p":
		return m, m.prevSong()
	case "left":
		m.seekBy(-5)
	case "right":
		m.seekBy(5)
	case "-", "_":
		m.setVolume(m.volume - 5)
	case "=", "+":
		m.setVolume(m.volume + 5)
	case "m":
		m.toggleMute()

	case "up", "k":
		if m.page == PageNow {
			m.scrollLyrics(-1)
		} else {
			m.moveList(-1)
		}
	case "down", "j":
		if m.page == PageNow {
			m.scrollLyrics(1)
		} else {
			m.moveList(1)
		}
	case "pgup":
		if m.page == PageNow {
			m.scrollLyrics(-m.lyricViewport())
		} else {
			m.moveList(-m.listH())
		}
	case "pgdown":
		if m.page == PageNow {
			m.scrollLyrics(m.lyricViewport())
		} else {
			m.moveList(m.listH())
		}
	case "g", "home":
		m.listToStart()
	case "G", "end":
		m.listToEnd()

	case "enter":
		// 先落到变量再返回：Value 接收者的 m 可能在指针接收者的修改生效之前
		// 就被复制进返回值里，那样 playSelected 设的状态栏会被吞掉。
		cmd := m.playSelected()
		return m, cmd
	case "a":
		// 「正在播放」页没有选中项，a 在这里是「回到自动跟随歌词」。
		if m.page == PageNow {
			m.lyricFollow = !m.lyricFollow
			if m.lyricFollow {
				m.status = "歌词已回到自动跟随"
			} else {
				m.status = "歌词手动浏览中（a 恢复跟随）"
			}
			return m, nil
		}
		return m, m.enqueueSelected()

	case "ctrl+t":
		// 只在搜索页有意义：切 歌曲/歌手/专辑/歌单。
		if m.page == PageSearch {
			return m, m.cycleSearchType()
		}

	case "]", "ctrl+f":
		if m.detailActive() {
			return m, m.cmdDetailMore()
		}
		if m.page == PageSearch {
			return m, m.searchNextPage()
		}
	case "[", "ctrl+b":
		if m.page == PageSearch {
			return m, m.searchPrevPage()
		}

	case "d":
		if m.page == PageQueue {
			return m, m.removeQueueAt(m.lists[PageQueue].cursor)
		}
		// 自己歌单的详情里：把选中的歌移出这个歌单。
		return m, m.removeFromCurrentPlaylist()
	case "A":
		return m, m.openPicker()
	case "Q":
		return m, m.cycleQuality()
	case "N":
		if m.page == PageMine && !m.detailActive() {
			m.openNewPlaylist()
		}
	case "D":
		if m.page == PageMine && !m.detailActive() {
			m.openDeletePlaylist()
		}
	case "f":
		// 收藏 / 取消收藏当前选中（正在播放页则是正在听的）这首歌。
		return m, m.toggleFavoriteSelected()
	case "c":
		// 队列页的 c 是清空队列；其它页是看当前曲目的评论。
		if m.page == PageQueue {
			return m, m.clearQueue()
		}
		return m, m.openComments()

	case "r":
		return m, m.cmdRefresh()

	case "esc":
		// 歌单/专辑详情是盖在当前页上的一层，esc 先退它。
		// （搜索框的 esc 不在这里：focus 是 FocusSearch 时根本进不到 handleListKey。）
		if m.detailActive() {
			m.closeDetail()
			return m, nil
		}
	}
	return m, nil
}

func (m *Model) inputKey(msg tea.KeyMsg) { editInput(&m.input, msg) }

// editInput 把一个按键作用到输入框上（搜索框和写评论共用）。
func editInput(in *textInput, msg tea.KeyMsg) {
	switch msg.Type {
	case tea.KeyRunes:
		in.Insert(msg.Runes...)
	case tea.KeySpace:
		in.Insert(' ')
	case tea.KeyBackspace:
		in.Backspace()
	case tea.KeyDelete:
		in.Delete()
	case tea.KeyCtrlW:
		in.DeleteWord()
	case tea.KeyCtrlU:
		in.Clear()
	case tea.KeyLeft:
		in.MoveLeft()
	case tea.KeyRight:
		in.MoveRight()
	case tea.KeyHome, tea.KeyCtrlA:
		in.Home()
	case tea.KeyEnd, tea.KeyCtrlE:
		in.End()
	}
}

func (m *Model) moveList(delta int) {
	if m.page == PageNow {
		return
	}
	rows := m.rows()
	m.lists[m.page].moveIn(rows, delta, m.listH())
}

// scrollLyrics 手动滚动歌词。第一次滚动会把「自动跟随」接管过来——
// 从当前自动位置起步，否则第一下会突然跳到底。
func (m *Model) scrollLyrics(delta int) {
	cur, ok := m.current()
	if !ok || len(cur.Lyrics) == 0 {
		return
	}
	n := m.lyricViewport()
	if m.lyricFollow {
		m.lyricScroll = m.lyricsTopAuto(len(cur.Lyrics), m.currentLyricIndex(cur.Lyrics), n)
		m.lyricFollow = false
	}
	m.lyricScroll = m.clampLyricScroll(m.lyricScroll+delta, len(cur.Lyrics), n)
	m.status = "歌词手动浏览中（a 恢复自动跟随）"
}

func (m *Model) listToStart() {
	if m.page == PageNow {
		return
	}
	rows := m.rows()
	m.lists[m.page].toStart(rows)
	m.lists[m.page].clamp(len(rows), m.listH())
}

func (m *Model) listToEnd() {
	if m.page == PageNow {
		return
	}
	rows := m.rows()
	m.lists[m.page].toEnd(rows)
	m.lists[m.page].clamp(len(rows), m.listH())
}

func (m *Model) enterPage(p Page) tea.Cmd {
	if p < 0 || p >= pageCount {
		return nil
	}
	m.gotoPage(p)
	// 切到搜索页就直接把光标放进输入框——进这一页的目的就是搜索，
	// 还要再按一次 / 才能打字是多余的一步。（Tab 在输入框里仍然能翻页，
	// 见 handleSearchKey。）
	if p == PageSearch {
		m.focus = FocusSearch
	}

	// 首次进入某页时把数据拉起来。失败过（有 err）就不自动重试，交给用户按 r。
	switch p {
	case PageExplore:
		if m.discover == nil && !m.discoverLoading && m.discoverErr == nil {
			return m.cmdDiscover()
		}
	case PageFavorites:
		if m.favorites == nil && !m.favoritesLoading && m.favoritesErr == nil {
			return m.cmdFavorites()
		}
	case PageLibrary, PageMine:
		if m.library == nil && !m.libraryLoading && m.libraryErr == nil {
			return m.cmdLibrary()
		}
	}
	return nil
}

func (m *Model) gotoPage(p Page) {
	m.page = p
	// 离开搜索页就把焦点还给列表：留在输入态的话 ↑↓ 会被输入框吃掉。
	if p != PageSearch && m.focus == FocusSearch {
		m.focus = FocusList
	}
	m.lists[p].snapToSelectable(m.rows(), 0)
	m.lists[p].clamp(len(m.rows()), m.listH())
}

// closeDetail 关掉盖在当前页上的歌单/专辑详情，回到进详情之前那份列表。
func (m *Model) closeDetail() {
	m.detail = nil
	m.detailPl = Playlist{}
	m.detailID = ""
	m.detailTitle = ""
	m.detailErr = nil
	m.detailLoading = false
	m.detailLoadingMore = false
	m.detailReq++ // 作废还在飞的那次详情请求
	// 详情占的是「进去那一页」的列表位置，所以清的是那一页的游标。
	m.lists[m.detailFrom] = listState{}
	m.lists[m.detailFrom].clamp(len(m.rows()), m.listH())
}

// openComments 打开当前曲目的评论浮层。
//
// 评论接口要的是**数字 id**（Track.ID），不是 songMid：
// 脚本里 fetch_comments 会 isdigit() 校验，传 songMid 只会得到「该歌曲没有可用的评论标识」。
// ---------- 我喜欢 ----------

// isFavorite 判断这首歌在不在「我喜欢」里。
//
// 依据是本地那份 favorites 列表（就是我们喜欢页显示的东西），而不是再问一次服务端：
// 收藏状态的服务端读接口 IsSongFanByMid 是准的，但列表接口有 CDN 缓存、
// 刚改完几分钟内读回来还是旧的，拿它当依据会让爱心刚点亮就又灭掉。
func (m Model) isFavorite(songMid string) bool {
	if songMid == "" {
		return false
	}
	if v, ok := m.favLocal[songMid]; ok {
		return v
	}
	if m.favMids[songMid] {
		return true
	}
	for _, t := range m.favorites {
		if t.SongMid == songMid {
			return true
		}
	}
	return false
}

func removeTrackByMid(tracks []Track, songMid string) []Track {
	out := tracks[:0]
	for _, t := range tracks {
		if t.SongMid != songMid {
			out = append(out, t)
		}
	}
	return out
}

// favoriteTarget 取这次按 f 要操作哪首歌。
//
// 列表页取选中行；「正在播放」页没有列表，退化成当前播放的曲目——
// 想给正在听的歌点爱心是这里最自然的诉求。
func (m Model) favoriteTarget() (Track, bool) {
	if m.page == PageNow {
		return m.current()
	}
	if r, ok := m.selectedRow(); ok && r.kind == rowTrack {
		return r.track, true
	}
	return Track{}, false
}

func (m *Model) toggleFavoriteSelected() tea.Cmd {
	t, ok := m.favoriteTarget()
	if !ok {
		m.status = "这里没有可以收藏的歌曲"
		return nil
	}
	if strings.TrimSpace(t.SongMid) == "" {
		m.status = "「" + t.Title + "」没有 songMid，收藏不了"
		return nil
	}
	if m.session.Checked && !m.session.LoggedIn {
		m.status = "收藏需要先登录，按 L 扫码"
		return nil
	}
	if m.favoriteBusy {
		m.status = "上一笔收藏还没写完，稍等一下"
		return nil
	}
	// guess 只是兜底：命令里会先实时问服务端，以服务端为准决定加还是删。
	guess := "add"
	if m.isFavorite(t.SongMid) {
		guess = "remove"
	}
	m.status = "正在更新收藏：" + t.Title
	return m.cmdToggleFavorite(t, guess)
}

func (m *Model) openComments() tea.Cmd {
	t, ok := m.current()
	if !ok {
		m.status = "当前没有在播放的歌曲"
		return nil
	}
	if strings.TrimSpace(t.ID) == "" {
		m.status = "「" + t.Title + "」没有数字 id，取不到评论"
		return nil
	}
	m.commentsTrack = t
	m.commentsList = listState{}
	m.comments = nil
	m.commentsTotal = 0
	m.commentsPage = 0
	m.commentsHasMore = false
	m.commentsErr = nil
	if m.commentsSongID != t.ID {
		// 换了歌：草稿不该带过去。同一首歌重开则保留，免得误按 esc 丢了半段话。
		m.commentInput.Clear()
	}
	m.commentWriting = false
	m.commentsSort = "hot"
	m.focus = FocusComments
	return m.cmdComments(t.ID, 1)
}

// commentsViewH 是评论浮层一屏**大约**能放几条（PgUp/PgDn 的步长、列表夹紧用）。
//
// 评论按实际行数排版，每条高度不一，这里按平均 3 行（抬头 + 一两行正文 + 空隙）估。
// 估不准也没关系：选中那条是否可见由 commentsBody 按真实行数保证，不靠这个数。
func (m Model) commentsViewH() int {
	// 浮层内容区 = bodyH-2，减去标题、排序行、空行和两行页脚。
	h := (m.bodyH() - 2 - 5) / 3
	if h < 1 {
		h = 1
	}
	return h
}

// ---------- View ----------

func (m Model) View() string {
	if m.w <= 0 || m.h <= 0 {
		return "加载中…"
	}
	tab := m.viewTabBar()
	player := m.viewPlayerBar()

	bodyW := m.w
	if bodyW < 4 {
		bodyW = 4
	}
	bodyH := m.bodyH()

	var body string
	switch m.focus {
	case FocusHelp:
		body = m.viewHelpPage(bodyW-2, bodyH-2)
	case FocusLogin:
		body = m.viewLoginPage(bodyW-2, bodyH-2)
	case FocusComments:
		body = m.viewComments(bodyW-2, bodyH-2)
	case FocusPicker:
		body = m.viewPicker(bodyW-2, bodyH-2)
	case FocusDialog:
		body = m.viewDialog(bodyW-2, bodyH-2)
	default:
		body = m.viewPage(bodyW-2, bodyH-2)
	}

	bodyBorder := colGreen
	if m.focus == FocusDialog && m.dialog == dialogDeletePlaylist {
		bodyBorder = colWarn
	} else if m.focus == FocusLogin || m.focus == FocusHelp {
		bodyBorder = colPink
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		tab,
		panel(body, bodyW, bodyH, bodyBorder),
		player,
	)
}

// setFavMids 用服务端给的全量 songMid 刷新收藏集合。
func (m *Model) setFavMids(mids []string) {
	set := make(map[string]bool, len(mids))
	for _, mid := range mids {
		set[mid] = true
	}
	m.favMids = set
}

// clearAccountData 清掉所有按账号算的数据（登录换号 / 退出登录时用），
// 下次进对应页面时按新身份重新拉。
func (m *Model) clearAccountData() {
	m.favorites, m.favoritesTotal, m.favoritesErr = nil, 0, nil
	m.favoritesReq++ // 在飞的旧账号结果作废
	m.favMids, m.favLocal = nil, nil
	m.favMidsReq++
	m.library, m.libraryErr = nil, nil
	m.libraryReq++
}
