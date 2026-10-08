package main

// 这个文件是 qqmusic_api.py 输出的 JSON 的 Go 镜像。
// 字段名和大小写必须与脚本完全一致（脚本用 camelCase 的 key）。
// 改了这里就要回头对照 scripts/qqmusic/qqmusic_api.py 的 normalize_song / normalize_playlist。

// Track 一首歌。搜索结果、歌单、我喜欢、音乐库返回的曲目**结构完全一致**，
// 因为脚本里它们都走同一个 normalize_song。所以只用一个类型。
type Track struct {
	ID       string   `json:"id"`
	SongMid  string   `json:"songMid"`
	MediaMid string   `json:"mediaMid"`
	Title    string   `json:"title"`
	Artists  []string `json:"artists"`
	Artist   string   `json:"artist"`
	Album    string   `json:"album"`
	AlbumMid string   `json:"albumMid"`
	Duration int      `json:"duration"` // 秒
	CoverURL string   `json:"coverUrl"`
	Vip      bool     `json:"vip"`
	Playable bool     `json:"playable"`
	// DirectURL 只给离线演示曲用（SoundHelix 直链），不来自接口。
	// 非空就跳过 resolve 直接丢给 mpv。
	DirectURL string `json:"-"`
	// Lyrics 不来自列表接口，是拿到曲目之后单独调 lyrics 子命令填进来的
	// （演示曲则自带）。挂在曲目上而不是单独存一份，切歌再切回来就不用重取。
	Lyrics []Lyric `json:"-"`
}

// Lyric 一行歌词。Time 单位为秒。
// JSON tag 是给桌面 shell 用的（Services/TuiPlayerService.qml 读 time/text）。
type Lyric struct {
	Time float64 `json:"time"`
	Text string  `json:"text"`
	// Translation 是对应行的翻译，来自 lyrics 接口的 translation 字段。
	// 桌面顶栏胶囊只有一行、放不下，所以那边实际会忽略它（QML 忽略未知字段）。
	Translation string `json:"translation,omitempty"`
}

// Playlist 歌单/专辑元信息。
//
// Kind 区分它到底是哪一种：
//   - "playlist" —— 真歌单（搜索结果、音乐库、推荐位）
//   - "album"    —— 专辑（搜索结果、专辑详情）
//
// 两者在界面上都只是「一张可回车进入的卡片」，所以共用一套渲染和进入逻辑，
// 只在真正取详情时按 Kind 分派到 playlist / album 子命令。
type Playlist struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	DirID     string `json:"dirId"`
	Title     string `json:"title"`
	Subtitle  string `json:"subtitle"`
	CoverURL  string `json:"coverUrl"`
	SongCount int    `json:"songCount"`
	PlayCount int    `json:"playCount"`
	Creator   string `json:"creator"`
}

// Singer 是 search --type singer 的一条结果。
// AvatarURL 由脚本按 T001 规则拼出来（接口只给 http 时脚本会升级成 https）。
type Singer struct {
	ID         string `json:"id"`
	Mid        string `json:"mid"`
	Name       string `json:"name"`
	AvatarURL  string `json:"avatarUrl"`
	SongCount  int    `json:"songCount"`
	AlbumCount int    `json:"albumCount"`
	MVCount    int    `json:"mvCount"`
}

// Comment 一条歌曲评论。time 是 Unix 秒。
type Comment struct {
	ID        string `json:"id"`
	SeqNo     string `json:"seqNo"`
	Author    string `json:"author"`
	AvatarURL string `json:"avatarUrl"`
	Text      string `json:"text"`
	Time      int64  `json:"time"`
	Likes     int    `json:"likes"`
	Replies   int    `json:"replies"`
	IsPraised bool   `json:"isPraised"` // 当前账号赞过没有
}

// ---- 各子命令的响应体 ----

// SearchResult 是 search 的响应。
//
// 四类结果各占一个字段，非当前 type 的那几个是空数组——服务端本来就一次只回一类
// （body 里四类都有 key，但只有当前 search_type 的那个非空）。
// Type 取值：song / singer / album / songlist。
type SearchResult struct {
	Query     string     `json:"query"`
	Type      string     `json:"type"`
	Page      int        `json:"page"`
	Tracks    []Track    `json:"tracks"`
	Singers   []Singer   `json:"singers"`
	Albums    []Playlist `json:"albums"`
	Playlists []Playlist `json:"playlists"`
	Total     int        `json:"total"`
	HasMore   bool       `json:"hasMore"`
}

// CommentsResult 是 comments 的响应。
type CommentsResult struct {
	SongID   string    `json:"songId"`
	Sort     string    `json:"sort"`
	Page     int       `json:"page"`
	Total    int       `json:"total"`
	HasMore  bool      `json:"hasMore"`
	Comments []Comment `json:"comments"`
}

type ResolveResult struct {
	SongMid  string `json:"songMid"`
	MediaMid string `json:"mediaMid"`
	URL      string `json:"url"`
	// Quality 是实际拿到的音质档位（所选档位拿不到时脚本会往下降；default = 不指定文件名的兜底）。
	Quality string `json:"quality"`
}

type LyricsResult struct {
	SongMid string  `json:"songMid"`
	Lyrics  []Lyric `json:"lyrics"`
}

// SessionStatus 是 session-status 的响应。
// sessionState 的取值：active / expired / missing（脚本 ensure_valid_session 的 state）。
type SessionStatus struct {
	LoggedIn     bool   `json:"loggedIn"`
	Uin          string `json:"uin"`
	Nickname     string `json:"nickname"`
	AvatarURL    string `json:"avatarUrl"`
	AccountType  string `json:"accountType"`
	SessionState string `json:"sessionState"`
}

// QRLogin 是 login-qr 的响应。QRPath 是脚本写出的二维码 PNG 路径——
// 用它而不是自己拼路径，免得两边的缓存目录规则哪天走散了。
type QRLogin struct {
	Type    string `json:"type"`
	State   string `json:"state"` // waiting
	Message string `json:"message"`
	QRPath  string `json:"qrPath"`
}

// LoginPoll 是 login-poll 的响应。
// State 取值：waiting / scanned / expired / error / success。
// 只有 success 时才有登录信息（脚本此时才把 session.json 落盘）。
type LoginPoll struct {
	State       string `json:"state"`
	Message     string `json:"message"`
	Type        string `json:"type"`
	LoggedIn    bool   `json:"loggedIn"`
	Uin         string `json:"uin"`
	Nickname    string `json:"nickname"`
	AvatarURL   string `json:"avatarUrl"`
	AccountType string `json:"accountType"`
}

// DiscoverResult 是探索页的数据源。
type DiscoverResult struct {
	DailyPlaylist        Playlist   `json:"dailyPlaylist"`
	DailyTracks          []Track    `json:"dailyTracks"`
	DailyRequiresLogin   bool       `json:"dailyRequiresLogin"`
	RecommendedPlaylists []Playlist `json:"recommendedPlaylists"`
	GuessTracks          []Track    `json:"guessTracks"`
	RadarTracks          []Track    `json:"radarTracks"`
}

// DailyResult 是 daily 子命令的响应（内部其实是 discover 的裁剪版）。
type DailyResult struct {
	Playlist Playlist `json:"playlist"`
	Tracks   []Track  `json:"tracks"`
}

// LibraryResult 是音乐库页的数据源：本地播放历史 + 账号歌单。
// 未登录时要求登录，但 recentTracks 仍可能有值。
type LibraryResult struct {
	RecentTracks       []Track    `json:"recentTracks"`
	CollectedPlaylists []Playlist `json:"collectedPlaylists"`
	CreatedPlaylists   []Playlist `json:"createdPlaylists"`
	RequiresLogin      bool       `json:"requiresLogin"`
	SessionState       string     `json:"sessionState"`
}

// FavoritesResult 是我喜欢页的数据源。
type FavoritesResult struct {
	Page    int      `json:"page"`
	Total   int      `json:"total"`
	Mids    []string `json:"mids"`
	Tracks  []Track  `json:"tracks"`
	HasMore bool     `json:"hasMore"`
}

// FavoriteResult 是 favorite 子命令的回执。
// Favorite 是**操作后**的状态（true = 已在「我喜欢」里），不是操作本身。
type FavoriteResult struct {
	SongMid  string `json:"songMid"`
	SongID   int64  `json:"songId"`
	Favorite bool   `json:"favorite"`
}

// PlaylistResult 是某个歌单的曲目。
type PlaylistResult struct {
	Playlist Playlist `json:"playlist"`
	Page     int      `json:"page"`
	Total    int      `json:"total"`
	Tracks   []Track  `json:"tracks"`
	HasMore  bool     `json:"hasMore"`
}
