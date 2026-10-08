package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain 把状态文件指到临时目录。所有构造 Model 的测试
// （包括 integration_test.go 里的 newModelWithSongs）都会经过这里，
// 否则跑一次测试就会在用户真实的 ~/.cache/qqmusic-tui 下留下垃圾。
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "qqmusic-tui-test-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "建临时目录失败:", err)
		os.Exit(1)
	}
	// 隔离后端与全部用户目录：普通 Go 测试只使用假 API；真实后端由
	// TestBundledBackendOffline 在拒绝网络的独立 Python 测试中覆盖。
	fakeScript, err := filepath.Abs(filepath.Join("testdata", "fake_api.py"))
	if err != nil {
		os.RemoveAll(dir)
		fmt.Fprintln(os.Stderr, "定位测试后端失败:", err)
		os.Exit(1)
	}
	// 构建命令行测试二进制仍复用已安装的 Go 依赖和缓存，不访问网络。
	for _, key := range []string{"GOCACHE", "GOPATH", "GOMODCACHE"} {
		if os.Getenv(key) == "" {
			out, err := exec.Command("go", "env", key).Output()
			if err != nil {
				os.RemoveAll(dir)
				fmt.Fprintln(os.Stderr, "获取 Go 测试环境失败:", err)
				os.Exit(1)
			}
			os.Setenv(key, strings.TrimSpace(string(out)))
		}
	}
	os.Setenv("HOME", filepath.Join(dir, "home"))
	for _, key := range []string{"XDG_CACHE_HOME", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME"} {
		os.Setenv(key, filepath.Join(dir, key))
	}
	os.Setenv("QQMUSIC_API", fakeScript)
	os.Setenv("QQMUSIC_PYTHON", "python3")
	os.Setenv("GOPROXY", "off")
	os.Setenv("QQMUSIC_TUI_STATE_FILE", filepath.Join(dir, "now.json"))
	// 同理：数据库和旧设置文件也指到临时目录，不碰用户真实的播放记录。
	os.Setenv("QQMUSIC_TUI_DB", filepath.Join(dir, "state.db"))
	os.Setenv("QQMUSIC_TUI_CONFIG", filepath.Join(dir, "config.json"))
	// 封面下载：测试里一律不联网（需要封面的测试自己再换成返回固定图片的版本）。
	fetchCover = func(string) (image.Image, error) { return nil, errors.New("测试里不联网") }
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// 桌面侧 TuiPlayerService.qml 按这些小写 key 解析，把这个契约钉住。
func TestPublisherWritesParseableState(t *testing.T) {
	// 故意用一层还不存在的子目录，顺带验证 Publish 会自建父目录。
	path := filepath.Join(t.TempDir(), "sub", "now.json")

	m := testModel()
	m.pub = &Publisher{path: path}
	m.pos, m.dur, m.paused = 41.3, 180, false
	m.publish()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("状态文件应已写出: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("应写出合法 JSON: %v\n%s", err, raw)
	}

	for _, key := range []string{"pid", "updatedAt", "playing", "paused", "position", "length", "track", "lyrics"} {
		if _, ok := got[key]; !ok {
			t.Errorf("JSON 缺少 key %q\n%s", key, raw)
		}
	}

	track, _ := got["track"].(map[string]any)
	if track == nil || track["title"] != "Midnight Groove" {
		t.Errorf("track 应带上当前曲目，实际 %v", got["track"])
	}
	if pos, _ := got["position"].(float64); pos != 41.3 {
		t.Errorf("position 应为 41.3，实际 %v", got["position"])
	}

	lines, _ := got["lyrics"].([]any)
	if len(lines) != len(demoSongs[0].Lyrics) {
		t.Fatalf("lyrics 应有 %d 行，实际 %d", len(demoSongs[0].Lyrics), len(lines))
	}
	first, _ := lines[0].(map[string]any)
	if first == nil {
		t.Fatal("歌词行应是对象")
	}
	if _, ok := first["time"]; !ok {
		t.Errorf("歌词行应有 time 字段，实际 %v", first)
	}
	if first["text"] != demoSongs[0].Lyrics[0].Text {
		t.Errorf("歌词行 text 应为 %q，实际 %v", demoSongs[0].Lyrics[0].Text, first["text"])
	}
}

func TestPublisherRemove(t *testing.T) {
	path := filepath.Join(t.TempDir(), "now.json")
	p := &Publisher{path: path}
	p.Publish(NowPlaying{Track: PubTrack{Title: "x"}})
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("文件应已写出: %v", err)
	}
	p.Remove()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("Remove 后文件应消失")
	}
}

// nil Publisher 必须是空操作：view_test.go 的 testModel() 直接字面量构造 Model，
// 里面没有 pub。这个测试就是那种情形的回归保护。
func TestNilPublisherIsNoop(t *testing.T) {
	var p *Publisher
	p.Publish(NowPlaying{Track: PubTrack{Title: "x"}})
	p.Remove()

	m := testModel()
	if m.pub != nil {
		t.Fatal("testModel 不应带 publisher")
	}
	m.publish() // 不应 panic
}

// nowPlaying 在 mpv 为 nil 时也不能 panic（字面量 Model 就是这种）。
func TestNowPlayingWithoutMpv(t *testing.T) {
	m := testModel()
	np := m.nowPlaying()

	if np.PID != os.Getpid() {
		t.Errorf("PID 应为当前进程，实际 %d", np.PID)
	}
	if np.Track.Title != demoSongs[0].Title {
		t.Errorf("曲名应取自当前曲目，实际 %q", np.Track.Title)
	}
	if len(np.Lyrics) != len(demoSongs[0].Lyrics) {
		t.Errorf("歌词行数应与曲目一致")
	}
	if np.UpdatedAt <= 0 {
		t.Errorf("UpdatedAt 应为正的 Unix 秒，实际 %v", np.UpdatedAt)
	}
}
