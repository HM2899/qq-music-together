package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeAPI 把 QQMUSIC_API 指向 testdata/fake_api.py，返回一个已配置的 Client。
// extra 用来设假后端的开关（FAKE_ERROR 等）。
func fakeAPI(t *testing.T, extra ...string) *Client {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("testdata", "fake_api.py"))
	if err != nil {
		t.Fatalf("定位假后端失败: %v", err)
	}
	t.Setenv("QQMUSIC_API", path)
	// 先清干净，免得宿主环境里的开关漏进来。
	for _, key := range []string{"FAKE_ERROR", "FAKE_EXIT", "FAKE_CRASH", "FAKE_SLEEP", "FAKE_FAV_FILE", "FAKE_PRAISE_ERROR", "FAKE_COMMENT_PENDING", "FAKE_PL_FILE", "FAKE_AUDIO", "FAKE_MAX_QUALITY"} {
		t.Setenv(key, "")
	}
	for _, kv := range extra {
		key, value, _ := strings.Cut(kv, "=")
		t.Setenv(key, value)
	}
	return NewClient()
}

func TestAPISearchReturnsTracks(t *testing.T) {
	c := fakeAPI(t)
	res, err := c.Search(context.Background(), "周杰伦", "song", 1, 30)
	if err != nil {
		t.Fatalf("搜索不该失败: %v", err)
	}
	if res.Query != "周杰伦" {
		t.Errorf("query 应回显为 %q，实际 %q", "周杰伦", res.Query)
	}
	if len(res.Tracks) != 1 {
		t.Fatalf("应返回 1 首歌，实际 %d", len(res.Tracks))
	}
	got := res.Tracks[0]
	if got.SongMid != "0039MnYb0qxYhV" || got.Title != "假歌一号" {
		t.Errorf("曲目字段解析有误: %+v", got)
	}
	if got.Duration != 214 {
		t.Errorf("时长应为 214，实际 %d", got.Duration)
	}
}

// 空关键词不该真的去起一个进程（脚本本身也会直接返回空）。
func TestAPISearchEmptyQueryShortCircuits(t *testing.T) {
	// 故意指到一个不存在的脚本：如果它还去执行，就会报 KindScriptMissing。
	t.Setenv("QQMUSIC_API", "/nonexistent/qqmusic_api.py")
	c := NewClient()
	res, err := c.Search(context.Background(), "   ", "song", 1, 30)
	if err != nil {
		t.Fatalf("空关键词不该报错: %v", err)
	}
	if len(res.Tracks) != 0 {
		t.Errorf("空关键词应返回空结果，实际 %d 首", len(res.Tracks))
	}
}

func TestAPIResolveLyricsAndCollections(t *testing.T) {
	c := fakeAPI(t)
	ctx := context.Background()

	res, err := c.Resolve(ctx, "0039MnYb0qxYhV", "", "")
	if err != nil {
		t.Fatalf("resolve 不该失败: %v", err)
	}
	if !strings.HasPrefix(res.URL, "https://") {
		t.Errorf("应返回 http(s) 直链，实际 %q", res.URL)
	}

	lyrics, err := c.Lyrics(ctx, "0039MnYb0qxYhV")
	if err != nil {
		t.Fatalf("lyrics 不该失败: %v", err)
	}
	if len(lyrics) != 2 || lyrics[1].Text != "第二句" || lyrics[1].Translation != "Line two" {
		t.Errorf("歌词解析有误: %+v", lyrics)
	}

	if _, err := c.Discover(ctx); err != nil {
		t.Errorf("discover 不该失败: %v", err)
	}
	if _, err := c.Library(ctx); err != nil {
		t.Errorf("library 不该失败: %v", err)
	}
	fav, err := c.Favorites(ctx, 1, 50)
	if err != nil {
		t.Fatalf("favorites 不该失败: %v", err)
	}
	if fav.Total != 1 || len(fav.Tracks) != 1 {
		t.Errorf("我喜欢解析有误: %+v", fav)
	}
	pl, err := c.Playlist(ctx, "9001", "", 1, 50)
	if err != nil {
		t.Fatalf("playlist 不该失败: %v", err)
	}
	// 假后端回显 --id，串台的话这里会立刻发现。
	if pl.Playlist.ID != "9001" {
		t.Errorf("歌单 id 应为 9001，实际 %q", pl.Playlist.ID)
	}
}

// 收藏两个方向都要走通，且 songId 要能解析成数字（真后端回的是 int）。
func TestAPIToggleFavorite(t *testing.T) {
	c := fakeAPI(t)
	ctx := context.Background()

	added, err := c.ToggleFavorite(ctx, "add", "0039MnYb0qxYhV", "1001")
	if err != nil {
		t.Fatalf("收藏不该失败: %v", err)
	}
	if !added.Favorite || added.SongMid != "0039MnYb0qxYhV" || added.SongID != 1001 {
		t.Errorf("收藏回执不对: %+v", added)
	}

	removed, err := c.ToggleFavorite(ctx, "remove", "0039MnYb0qxYhV", "")
	if err != nil {
		t.Fatalf("取消收藏不该失败: %v", err)
	}
	if removed.Favorite {
		t.Errorf("取消收藏后 Favorite 应为 false: %+v", removed)
	}
}

// ---------- 失败模式 ----------

func TestAPIScriptMissing(t *testing.T) {
	t.Setenv("QQMUSIC_API", "/nonexistent/qqmusic_api.py")
	c := NewClient()
	if c.setupErr == nil {
		t.Fatal("脚本不存在时构造阶段就该记下问题")
	}
	_, err := c.Search(context.Background(), "x", "song", 1, 10)
	if KindOf(err) != KindScriptMissing {
		t.Fatalf("应归类为 KindScriptMissing，实际 %v（%v）", KindOf(err), err)
	}
	// 提示里要带上脚本路径，否则用户不知道该放哪儿。
	if !strings.Contains(err.Error(), "/nonexistent/qqmusic_api.py") {
		t.Errorf("错误提示应包含脚本路径，实际 %q", err.Error())
	}
}

// argparse 参数错：stdout 为空、stderr 有 usage、exit 2。
func TestAPIBadOutputOnUsageError(t *testing.T) {
	c := fakeAPI(t, "FAKE_EXIT=2")
	_, err := c.Search(context.Background(), "x", "song", 1, 10)
	if KindOf(err) != KindBadOutput {
		t.Fatalf("应归类为 KindBadOutput，实际 %v（%v）", KindOf(err), err)
	}
	if !strings.Contains(err.Error(), "usage") {
		t.Errorf("应把 stderr 的 usage 带出来，实际 %q", err.Error())
	}
}

// 脚本自身崩溃：stderr 是 traceback，stdout 是空的。
func TestAPIExecCrash(t *testing.T) {
	c := fakeAPI(t, "FAKE_CRASH=1")
	_, err := c.Search(context.Background(), "x", "song", 1, 10)
	if KindOf(err) != KindExec {
		t.Fatalf("应归类为 KindExec，实际 %v（%v）", KindOf(err), err)
	}
	if !strings.Contains(err.Error(), "RuntimeError") {
		t.Errorf("应带出 traceback 的最后一行，实际 %q", err.Error())
	}
}

func TestAPIBusinessError(t *testing.T) {
	const msg = "该歌曲需要 QQ 音乐会员或登录后播放"
	c := fakeAPI(t, "FAKE_ERROR="+msg)
	_, err := c.Resolve(context.Background(), "x", "", "")
	if KindOf(err) != KindBusiness {
		t.Fatalf("应归类为 KindBusiness，实际 %v", KindOf(err))
	}
	// 脚本给的中文文案要原样透传，不能自造。
	if err.Error() != msg {
		t.Errorf("应原样透传脚本的文案，实际 %q", err.Error())
	}
}

// 网络错误在信封层面和业务错误长得一模一样，只能靠错误串前缀区分。
func TestAPINetworkErrorClassification(t *testing.T) {
	cases := []struct {
		msg  string
		want ErrorKind
	}{
		{"QQ 音乐服务请求失败: <urlopen error timed out>", KindNetwork},
		{"登录服务请求失败: [Errno -3] Temporary failure in name resolution", KindNetwork},
		{"获取登录二维码失败: Connection refused", KindNetwork},
		{"请先登录 QQ 音乐", KindBusiness},
		{"歌单加载失败", KindBusiness},
	}
	for _, tc := range cases {
		c := fakeAPI(t, "FAKE_ERROR="+tc.msg)
		_, err := c.Discover(context.Background())
		if got := KindOf(err); got != tc.want {
			t.Errorf("错误 %q 应归类为 %v，实际 %v", tc.msg, tc.want, got)
		}
	}
}

// 超时：外层 ctx 的期限比方法自带的超时更早，于是被 CommandContext 杀掉。
func TestAPITimeout(t *testing.T) {
	c := fakeAPI(t, "FAKE_SLEEP=5")
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := c.Search(ctx, "x", "song", 1, 10)
	if KindOf(err) != KindTimeout {
		t.Fatalf("应归类为 KindTimeout，实际 %v（%v）", KindOf(err), err)
	}
	// 必须真的提前返回，而不是等满脚本自带的 30 秒。
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("应在外层期限就返回，实际等了 %v", elapsed)
	}
}

// 主动取消（登录层按 esc 就是这样）：也要杀干净，不留孤儿进程。
func TestAPICancelKillsProcess(t *testing.T) {
	c := fakeAPI(t, "FAKE_SLEEP=5")
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()

	_, err := c.Search(ctx, "x", "song", 1, 10)
	if KindOf(err) != KindTimeout {
		t.Fatalf("取消应归类为 KindTimeout，实际 %v（%v）", KindOf(err), err)
	}
}

func TestIsAuthError(t *testing.T) {
	c := fakeAPI(t, "FAKE_ERROR=请先登录 QQ 音乐")
	_, err := c.Favorites(context.Background(), 1, 50)
	if !IsAuthError(err) {
		t.Errorf("「请先登录」应被判为需要登录，实际 %v", err)
	}

	c2 := fakeAPI(t, "FAKE_ERROR=歌单加载失败")
	_, err2 := c2.Playlist(context.Background(), "1", "", 1, 50)
	if IsAuthError(err2) {
		t.Errorf("普通业务错误不该被判为需要登录，实际 %v", err2)
	}
}
