package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Client 通过拉起 qqmusic_api.py 子进程来访问 QQ 音乐。
//
// 为什么是子进程而不是 Go 重写一份 HTTP 客户端：随包脚本源自桌面 QML 客户端
// 的实现（签名、cookie 续期、各种 fcgi 参数都在里面），不要求预装 Quickshell。
// 脚本是一次性进程：每个子命令跑完就退，
// 状态全靠它自己维护的 session.json。
type Client struct {
	python string // python3 可执行文件
	script string // qqmusic_api.py 路径
	// setupErr 在构造时就发现的问题（脚本/python 找不到）。
	// 存下来而不是构造失败返回 error：TUI 仍可启动并提示后端配置错误，
	// 后续接口调用会返回该错误；测试可独立注入本地音频，不依赖后端。
	setupErr error
}

// NewClient 定位脚本与解释器。不联网、不起进程。
func NewClient() *Client {
	c := &Client{}
	executable, _ := os.Executable()
	c.script, c.setupErr = locateScript(os.Getenv("QQMUSIC_API"), executable,
		[]string{"/usr/local/share/qqmusic-tui/backend", "/usr/share/qqmusic-tui/backend"})
	if c.setupErr != nil {
		return c
	}

	c.python = strings.TrimSpace(os.Getenv("QQMUSIC_PYTHON"))
	if c.python == "" {
		c.python = "python3"
	}
	python, err := exec.LookPath(c.python)
	if err != nil {
		c.setupErr = fmt.Errorf("找不到 %s，请先安装 Python 3（可用 QQMUSIC_PYTHON 指定解释器）", c.python)
	} else {
		c.python = python
	}
	return c
}

// locateScript 仅从可执行文件所在安装布局及系统路径找默认后端，不信任 cwd。
// 显式覆盖允许相对路径，但立即转为绝对路径，且任何错误都不静默回退。
func locateScript(override, executable string, systemDirs []string) (string, error) {
	if override = strings.TrimSpace(override); override != "" {
		path, err := filepath.Abs(override)
		if err != nil {
			return override, fmt.Errorf("无法定位接口脚本 %s: %w", override, err)
		}
		return path, checkScript(path)
	}

	var candidates []string
	if filepath.IsAbs(executable) {
		// 便携包经符号链接启动时，资源仍相对于真正的二进制。
		if resolved, err := filepath.EvalSymlinks(executable); err == nil {
			executable = resolved
		}
		dir := filepath.Dir(executable)
		candidates = append(candidates,
			filepath.Join(dir, "..", "share", "qqmusic-tui", "backend", "qqmusic_api.py"),
			filepath.Join(dir, "backend", "qqmusic_api.py"))
	}
	for _, dir := range systemDirs {
		if filepath.IsAbs(dir) {
			candidates = append(candidates, filepath.Join(dir, "qqmusic_api.py"))
		}
	}
	for _, path := range candidates {
		err := checkScript(path)
		if err == nil {
			return path, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return path, err
		}
	}
	return "", fmt.Errorf("找不到接口脚本（已检查 %s）；请安装随附后端或用 QQMUSIC_API 指定路径", strings.Join(candidates, "、"))
}

func checkScript(path string) error {
	info, err := os.Stat(path)
	if err == nil && !info.Mode().IsRegular() {
		err = errors.New("不是普通文件")
	}
	if err == nil {
		var file *os.File
		file, err = os.Open(path)
		if err == nil {
			err = file.Close()
		}
	}
	if err != nil {
		return fmt.Errorf("接口脚本不可用 %s（可用 QQMUSIC_API 指定路径）: %w", path, err)
	}
	return nil
}

// ScriptPath 返回实际使用的脚本路径（错误提示里要显示给用户看）。
func (c *Client) ScriptPath() string { return c.script }

// ---- 错误模型 ----

// ErrorKind 把失败分类到「用户能采取不同行动」的粒度上：
// 脚本没装要装着，网络问题要重试，业务错误要看服务端说了什么。
type ErrorKind int

const (
	KindScriptMissing ErrorKind = iota // 脚本或 python3 找不到
	KindExec                           // 进程起不来，或崩了没留下信封
	KindTimeout                        // ctx 超时 / 被取消
	KindBadOutput                      // 不是 JSON、缺 ok 字段、argparse 参数错
	KindBusiness                       // ok:false 的业务错误（会员、需要登录……）
	KindNetwork                        // ok:false，但错误串看着是网络问题
)

func (k ErrorKind) String() string {
	switch k {
	case KindScriptMissing:
		return "脚本缺失"
	case KindExec:
		return "执行失败"
	case KindTimeout:
		return "超时"
	case KindBadOutput:
		return "输出异常"
	case KindBusiness:
		return "业务错误"
	case KindNetwork:
		return "网络错误"
	}
	return "未知错误"
}

// APIError 带上分类，方便 UI 决定是提示重试还是提示登录。
type APIError struct {
	Kind ErrorKind
	Msg  string
}

func (e *APIError) Error() string { return e.Msg }

// KindOf 取出错误的分类；不是 APIError 的（比如 mpv 的错）归 KindExec。
func KindOf(err error) ErrorKind {
	var ae *APIError
	if errors.As(err, &ae) {
		return ae.Kind
	}
	return KindExec
}

// IsAuthError 判断错误是不是「需要重新登录」这一类，UI 据此把登录入口亮出来。
func IsAuthError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, frag := range []string{"请先登录", "登录已过期", "登录已失效", "登录凭证"} {
		if strings.Contains(msg, frag) {
			return true
		}
	}
	return false
}

// ---- 执行 ----

// envelope 是所有子命令共用的外壳。成功是 {"ok":true,...}，失败是 {"ok":false,"error":"中文"}。
type envelope struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`
}

// run 跑一次子命令并把 stdout 当 JSON 取回来。
//
// ctx 管「随程序退出而取消」，timeout 管「这次调用最多等多久」，两者分开：
// 用同一个 ctx 兼做两件事的话，退出时正在跑的 Cmd 会一直拖到超时才结束。
// 超时在函数内部派生成子 ctx 并 defer cancel，调用方不用管。
func (c *Client) run(ctx context.Context, timeout time.Duration, args ...string) (json.RawMessage, error) {
	if c.setupErr != nil {
		return nil, &APIError{KindScriptMissing, c.setupErr.Error()}
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, c.python, append([]string{c.script}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()

	// 超时/取消要最先判：被 CommandContext 杀掉时 ExitCode() 是 -1，
	// 不先判这个就会误报成「进程起不来」。
	if ctx.Err() != nil {
		return nil, &APIError{KindTimeout, "请求超时或已取消（" + strings.Join(args, " ") + "）"}
	}

	out := bytes.TrimSpace(stdout.Bytes())

	if runErr != nil {
		var exitErr *exec.ExitError
		if !errors.As(runErr, &exitErr) {
			// 连进程都没起来（权限、解释器坏了……）
			return nil, &APIError{KindExec, "无法执行接口脚本: " + runErr.Error()}
		}
		code := exitErr.ExitCode()

		// exit 2 = argparse 参数错。此时 stdout 是空的，错误只在 stderr。
		if code == 2 {
			return nil, &APIError{KindBadOutput, "接口参数错误: " + firstLine(stderr.String())}
		}

		// exit 1：脚本只会为 ApiError 打印信封后返回 1。
		// 能解析出信封就是业务/网络错误，解析不出来说明是脚本自身崩了（traceback 在 stderr）。
		if env, ok := parseEnvelope(out); ok && !env.OK {
			msg := strings.TrimSpace(env.Error)
			if msg == "" {
				msg = "接口返回了未说明原因的错误"
			}
			return nil, &APIError{classifyBusiness(msg), msg}
		}
		detail := lastLine(stderr.String())
		if detail == "" {
			detail = fmt.Sprintf("退出码 %d", code)
		}
		return nil, &APIError{KindExec, "接口异常退出: " + detail}
	}

	// exit 0：正常应该是一行 JSON 信封。空输出说明脚本被改坏了。
	if len(out) == 0 {
		return nil, &APIError{KindBadOutput, "接口没有输出（脚本可能被截断或版本不匹配）"}
	}
	env, ok := parseEnvelope(out)
	if !ok {
		return nil, &APIError{KindBadOutput, "接口输出不是合法 JSON: " + truncateForError(string(out))}
	}
	if !env.OK {
		// 理论上到不了这里（ok:false 会返回 1），但信封说什么就信什么。
		msg := strings.TrimSpace(env.Error)
		if msg == "" {
			msg = "接口返回失败但没有说明原因"
		}
		return nil, &APIError{classifyBusiness(msg), msg}
	}
	return json.RawMessage(out), nil
}

// parseEnvelope 从输出里抽信封。取最后一行非空，
// 免得脚本将来在前面多打了一行日志就把解析带崩。
func parseEnvelope(out []byte) (envelope, bool) {
	line := lastNonEmptyLine(out)
	if len(line) == 0 {
		return envelope{}, false
	}
	var env envelope
	if err := json.Unmarshal(line, &env); err != nil {
		return envelope{}, false
	}
	return env, true
}

// classifyBusiness 在 ok:false 的错误串上做网络/业务的启发式区分。
//
// 为什么只能靠字符串：脚本把 OSError/URLError 全都包装成 ApiError，
// 在信封层面（{"ok":false,"error":"..."}）和真正的业务错误长得一模一样，
// 没有 code 字段可以判。所以这是唯一可行的手段，也是有意为之的近似——
// 判错了只影响提示文案，不影响功能。
func classifyBusiness(msg string) ErrorKind {
	networkPrefixes := []string{
		"QQ 音乐服务请求失败",
		"登录服务请求失败",
		"获取登录二维码失败",
		"检查登录状态失败",
		"完成 QQ 登录失败",
		"QQ 音乐授权失败",
		"微信登录服务",
	}
	for _, p := range networkPrefixes {
		if strings.HasPrefix(msg, p) {
			return KindNetwork
		}
	}
	networkFragments := []string{
		"timed out", "timeout", "Temporary failure in name resolution",
		"Name or service not known", "Connection refused", "Connection reset",
		"Remote end closed", "SSLError", "certificate", "urlopen error",
		"HTTP Error", "Network is unreachable",
	}
	for _, f := range networkFragments {
		if strings.Contains(msg, f) {
			return KindNetwork
		}
	}
	return KindBusiness
}

func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return truncateForError(t)
		}
	}
	return ""
}

func lastLine(s string) string {
	lines := strings.Split(s, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if t := strings.TrimSpace(lines[i]); t != "" {
			return truncateForError(t)
		}
	}
	return ""
}

func lastNonEmptyLine(b []byte) []byte {
	lines := bytes.Split(b, []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		if line := bytes.TrimSpace(lines[i]); len(line) > 0 {
			return line
		}
	}
	return nil
}

func truncateForError(s string) string {
	const max = 300
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// ---- 对外超时 ----
//
// 不用一刀切：桌面端在 sb-tun 下访问 u.y.qq.com 有 3~5 秒的偶发 TLS 卡顿，
// 超时给太紧会把网络抖动变成假错误。低于 25 秒基本都会误伤。

const (
	timeoutDefault = 30 * time.Second
	timeoutSearch  = 30 * time.Second
	// discover 内部是两次串行服务端请求（推荐歌单 + 每日 30 首的歌单详情），
	// 最坏能到 40 秒，必须单独放宽。
	timeoutDiscover = 75 * time.Second
	// login-poll 脚本里 --wait 是**解析了但从不使用**的死参数（见 poll_qr_login），
	// 每次调用都是单次查询立即返回，所以这里不需要长超时。
	timeoutLoginPoll = 20 * time.Second
)

// ---- 高层方法 ----

// Search 搜索。kind 取 song / singer / album / songlist，对应脚本的 --type。
func (c *Client) Search(ctx context.Context, query, kind string, page, limit int) (*SearchResult, error) {
	query = strings.TrimSpace(query)
	// 脚本对空关键词直接返回空结果，这里就不浪费一次进程了。
	if query == "" {
		return &SearchResult{Type: kind, Page: 1}, nil
	}
	if kind == "" {
		kind = "song"
	}
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 50 {
		limit = 30
	}
	raw, err := c.run(ctx, timeoutSearch, "search", query,
		"--type", kind, "--page", fmt.Sprint(page), "--limit", fmt.Sprint(limit))
	if err != nil {
		return nil, err
	}
	var res SearchResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, &APIError{KindBadOutput, "搜索结果无法解析: " + err.Error()}
	}
	return &res, nil
}

// Album 取一张专辑的曲目。返回体和歌单一模一样（PlaylistResult），
// 因为脚本那边刻意对齐了两者的信封，上层就不用为专辑再写一套详情逻辑。
func (c *Client) Album(ctx context.Context, albumMid string, page, limit int) (*PlaylistResult, error) {
	if strings.TrimSpace(albumMid) == "" {
		return nil, &APIError{KindBusiness, "缺少专辑标识"}
	}
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 50 {
		limit = 50
	}
	raw, err := c.run(ctx, timeoutDefault, "album",
		"--album-mid", albumMid, "--page", fmt.Sprint(page), "--limit", fmt.Sprint(limit))
	if err != nil {
		return nil, err
	}
	var res PlaylistResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, &APIError{KindBadOutput, "专辑无法解析: " + err.Error()}
	}
	return &res, nil
}

// Comments 取一首歌的评论。songID 是数字 id（Track.ID）。
//
// sort 是 hot（热评，按页码翻）或 new（最新，按游标翻：cursor 填上一页最后一条的 SeqNo；
// 服务端在不带游标时会忽略页码、永远回第一页）。热评也接受游标，所以统一都传。
func (c *Client) Comments(ctx context.Context, songID, sort string, page int, cursor string, limit int) (*CommentsResult, error) {
	if strings.TrimSpace(songID) == "" {
		return nil, &APIError{KindBusiness, "这首歌没有数字 id，取不到评论"}
	}
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 50 {
		limit = 20
	}
	if sort != "new" {
		sort = "hot"
	}
	raw, err := c.run(ctx, timeoutDefault, "comments",
		"--song-id", songID, "--sort", sort, "--page", fmt.Sprint(page),
		"--limit", fmt.Sprint(limit), "--cursor", cursor)
	if err != nil {
		return nil, err
	}
	var res CommentsResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, &APIError{KindBadOutput, "评论无法解析: " + err.Error()}
	}
	return &res, nil
}

// CreatePlaylist 新建一个自己的歌单，返回新歌单（含 dirId）。
func (c *Client) CreatePlaylist(ctx context.Context, name string) (*Playlist, error) {
	raw, err := c.run(ctx, timeoutDefault, "playlist-create", "--name", name)
	if err != nil {
		return nil, err
	}
	var res struct {
		Playlist Playlist `json:"playlist"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, &APIError{KindBadOutput, "新建歌单结果无法解析: " + err.Error()}
	}
	return &res.Playlist, nil
}

// DeletePlaylist 删除自己创建的歌单。脚本会拒绝删除「我喜欢」（dirId 201）。
func (c *Client) DeletePlaylist(ctx context.Context, dirID string) error {
	_, err := c.run(ctx, timeoutDefault, "playlist-delete", "--dir-id", dirID)
	return err
}

// PlaylistSong 往自己的歌单（按 dirId）里加（action=add）或删（remove）一首歌。
func (c *Client) PlaylistSong(ctx context.Context, action, dirID, songMid string) error {
	_, err := c.run(ctx, timeoutDefault, "playlist-song", action, "--dir-id", dirID, "--song-mid", songMid)
	return err
}

// CommentPraise 给评论点赞（like=true）或取消点赞。走 musics.fcg 带签名的网页端接口。
func (c *Client) CommentPraise(ctx context.Context, commentID string, like bool) error {
	action := "unlike"
	if like {
		action = "like"
	}
	_, err := c.run(ctx, timeoutDefault, "comment-praise", action, "--comment-id", commentID)
	return err
}

// CommentAdd 给歌曲发一条评论。pending=true 表示已受理但要审核（服务端 code 20015）。
func (c *Client) CommentAdd(ctx context.Context, songID, content string) (pending bool, err error) {
	raw, err := c.run(ctx, timeoutDefault, "comment-add", "--song-id", songID, "--content", content)
	if err != nil {
		return false, err
	}
	var res struct {
		Pending bool `json:"pending"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return false, &APIError{KindBadOutput, "评论结果无法解析: " + err.Error()}
	}
	return res.Pending, nil
}

// Resolve 把 songMid 换成可直接播放的直链。
// 直链有时效，每次播放都得重新取，不能缓存。
// quality 为空时不指定音质（服务端默认的标准 m4a），否则是 std / 128 / 320 / flac。
func (c *Client) Resolve(ctx context.Context, songMid, mediaMid, quality string) (*ResolveResult, error) {
	args := []string{"resolve", "--song-mid", songMid, "--media-mid", mediaMid}
	if quality != "" {
		args = append(args, "--quality", quality)
	}
	raw, err := c.run(ctx, timeoutDefault, args...)
	if err != nil {
		return nil, err
	}
	var res ResolveResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, &APIError{KindBadOutput, "播放地址无法解析: " + err.Error()}
	}
	if strings.TrimSpace(res.URL) == "" {
		return nil, &APIError{KindBusiness, "接口没有返回可用的播放地址"}
	}
	return &res, nil
}

func (c *Client) Lyrics(ctx context.Context, songMid string) ([]Lyric, error) {
	raw, err := c.run(ctx, timeoutDefault, "lyrics", "--song-mid", songMid)
	if err != nil {
		return nil, err
	}
	var res LyricsResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, &APIError{KindBadOutput, "歌词无法解析: " + err.Error()}
	}
	// 没歌词时脚本会补一行占位的「暂无歌词」（桌面客户端靠它显示）。
	// 这里当成「没有歌词」：正在播放页显示自己的提示，顶栏胶囊改显示歌名。
	if len(res.Lyrics) == 1 && strings.TrimSpace(res.Lyrics[0].Text) == "暂无歌词" {
		return nil, nil
	}
	return res.Lyrics, nil
}

func (c *Client) SessionStatus(ctx context.Context) (*SessionStatus, error) {
	raw, err := c.run(ctx, timeoutDefault, "session-status")
	if err != nil {
		return nil, err
	}
	var res SessionStatus
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, &APIError{KindBadOutput, "登录状态无法解析: " + err.Error()}
	}
	return &res, nil
}

// LoginQR 生成登录二维码。kind 是 "qq" 或 "wechat"。
func (c *Client) LoginQR(ctx context.Context, kind string) (*QRLogin, error) {
	if kind != "wechat" {
		kind = "qq"
	}
	raw, err := c.run(ctx, timeoutDefault, "login-qr", "--type", kind)
	if err != nil {
		return nil, err
	}
	var res QRLogin
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, &APIError{KindBadOutput, "二维码信息无法解析: " + err.Error()}
	}
	return &res, nil
}

// LoginPoll 查询一次扫码状态。脚本的 --wait 参数是死参数（不起作用），
// 所以每次调用都是立即返回，轮询节拍得由调用方自己控制。
func (c *Client) LoginPoll(ctx context.Context) (*LoginPoll, error) {
	raw, err := c.run(ctx, timeoutLoginPoll, "login-poll")
	if err != nil {
		return nil, err
	}
	var res LoginPoll
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, &APIError{KindBadOutput, "登录状态无法解析: " + err.Error()}
	}
	return &res, nil
}

func (c *Client) Logout(ctx context.Context) error {
	_, err := c.run(ctx, timeoutDefault, "logout")
	return err
}

func (c *Client) Discover(ctx context.Context) (*DiscoverResult, error) {
	raw, err := c.run(ctx, timeoutDiscover, "discover")
	if err != nil {
		return nil, err
	}
	var res DiscoverResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, &APIError{KindBadOutput, "探索内容无法解析: " + err.Error()}
	}
	return &res, nil
}

func (c *Client) Daily(ctx context.Context) (*DailyResult, error) {
	raw, err := c.run(ctx, timeoutDiscover, "daily")
	if err != nil {
		return nil, err
	}
	var res DailyResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, &APIError{KindBadOutput, "每日推荐无法解析: " + err.Error()}
	}
	return &res, nil
}

func (c *Client) Library(ctx context.Context) (*LibraryResult, error) {
	raw, err := c.run(ctx, timeoutDefault, "library")
	if err != nil {
		return nil, err
	}
	var res LibraryResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, &APIError{KindBadOutput, "音乐库无法解析: " + err.Error()}
	}
	return &res, nil
}

func (c *Client) Favorites(ctx context.Context, page, limit int) (*FavoritesResult, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 50 {
		limit = 50
	}
	raw, err := c.run(ctx, timeoutDefault, "favorites",
		"--page", fmt.Sprint(page), "--limit", fmt.Sprint(limit))
	if err != nil {
		return nil, err
	}
	var res FavoritesResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, &APIError{KindBadOutput, "我喜欢无法解析: " + err.Error()}
	}
	return &res, nil
}

func (c *Client) Playlist(ctx context.Context, id, dirID string, page, limit int) (*PlaylistResult, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 50 {
		limit = 50
	}
	args := []string{"playlist", "--id", id}
	if dirID != "" {
		args = append(args, "--dir-id", dirID)
	}
	args = append(args, "--page", fmt.Sprint(page), "--limit", fmt.Sprint(limit))
	raw, err := c.run(ctx, timeoutDefault, args...)
	if err != nil {
		return nil, err
	}
	var res PlaylistResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, &APIError{KindBadOutput, "歌单无法解析: " + err.Error()}
	}
	return &res, nil
}

// FavoriteCheck 问服务端这首歌此刻在不在「我喜欢」里。
//
// 走的是 IsSongFanByMid，实时且准确；不能拿 favorites 的列表代替——
// 那个列表接口有 CDN 缓存，刚在别处（桌面客户端、手机）改过的收藏几分钟内读回来还是旧的。
func (c *Client) FavoriteCheck(ctx context.Context, songMid string) (bool, error) {
	raw, err := c.run(ctx, timeoutDefault, "favorite-check", "--song-mid", songMid)
	if err != nil {
		return false, err
	}
	var res struct {
		Fans map[string]bool `json:"fans"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return false, &APIError{KindBadOutput, "收藏状态无法解析: " + err.Error()}
	}
	fan, ok := res.Fans[songMid]
	if !ok {
		return false, &APIError{KindBadOutput, "收藏状态缺少这首歌"}
	}
	return fan, nil
}

// ToggleFavorite 加/删「我喜欢」。
//
// songID 传空串也可以：脚本会拿 songMid 去换数字 ID。之所以两个都传，
// 是因为列表接口本来就带回了数字 ID，白拿的东西没必要再让服务端查一次。
func (c *Client) ToggleFavorite(ctx context.Context, action, songMid, songID string) (*FavoriteResult, error) {
	args := []string{"favorite", action, "--song-mid", songMid}
	if songID != "" {
		args = append(args, "--song-id", songID)
	}
	raw, err := c.run(ctx, timeoutDefault, args...)
	if err != nil {
		return nil, err
	}
	var res FavoriteResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, &APIError{KindBadOutput, "收藏结果无法解析: " + err.Error()}
	}
	return &res, nil
}
