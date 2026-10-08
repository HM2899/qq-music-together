package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// LoadAt：mpv 从指定位置开始放（切音质时接着原位置的基础）。
func TestMpvLoadAt(t *testing.T) {
	audio := makeTestAudio(t)
	m, err := StartMpv(70, "--ao=null", "--no-config")
	if err != nil {
		t.Fatalf("启动 mpv 失败: %v", err)
	}
	defer m.Close()
	if err := m.LoadAt(audio, 4); err != nil {
		t.Fatalf("LoadAt 失败: %v", err)
	}
	var pos float64
	for i := 0; i < 30; i++ {
		if pos = m.TimePos(); pos > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if pos < 3.9 {
		t.Fatalf("应从 4 秒处开始放，实际 %.2f", pos)
	}
}

// 旧版 config.json：没有文件时用默认；有就读出来；坏值忽略。
func TestLegacyQualityConfig(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("QQMUSIC_TUI_CONFIG", p)
	if q := loadConfig().Quality; q != defaultQuality {
		t.Errorf("没有设置文件时应为默认 %q，实际 %q", defaultQuality, q)
	}
	_ = os.WriteFile(p, []byte(`{"quality":"flac"}`), 0o644)
	if q := loadConfig().Quality; q != "flac" {
		t.Errorf("应读回 flac，实际 %q", q)
	}
	_ = os.WriteFile(p, []byte(`{"quality":"RS01"}`), 0o644)
	if q := loadConfig().Quality; q != defaultQuality {
		t.Errorf("不认识的档位应忽略，实际 %q", q)
	}
}

// 按 Q 轮换音质：存盘、正在放的歌按新音质从原位置接着放；拿不到所选档位时如实说明降了一档。
func TestQualityCycleResumes(t *testing.T) {
	audio := makeTestAudio(t)
	mpv, err := StartMpv(70, "--ao=null", "--no-config")
	if err != nil {
		t.Fatalf("启动 mpv 失败: %v", err)
	}
	defer mpv.Close()
	store, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	m := fakeModel(t, PageExplore)
	m.store = store
	m.api = fakeAPI(t, "FAKE_AUDIO="+audio, "FAKE_MAX_QUALITY=128")
	m.mpv = mpv
	m.queue = []Track{{ID: "1", SongMid: "0039MnYb0qxYhV", MediaMid: "003Qui1q2u1Zho", Title: "假歌一号", Duration: 10}}
	m.curIndex = 0
	m = runCmd(t, m, m.startCurrent())
	if m.playQuality != "std" {
		t.Fatalf("默认按标准音质解析，实际 %q（status=%q）", m.playQuality, m.status)
	}
	time.Sleep(1500 * time.Millisecond)
	m = m.onTick()
	before := m.displayPos()

	// std → 128：拿得到
	m = press(t, m, "Q")
	if m.quality != "128" || m.playQuality != "128" || !contains(m.status, "已切换到128k") {
		t.Fatalf("应切到 128k，实际 quality=%q play=%q status=%q", m.quality, m.playQuality, m.status)
	}
	if store.LoadQuality() != "128" {
		t.Error("音质设置应存进数据库")
	}
	time.Sleep(300 * time.Millisecond)
	if pos := mpv.TimePos(); pos < before-0.3 {
		t.Errorf("切音质应接着原位置（%.2f）放，实际 %.2f", before, pos)
	}

	// 128 → 320：这首最高只有 128，如实说明
	m = press(t, m, "Q")
	if m.quality != "320" || m.playQuality != "128" || !contains(m.status, "这首没有320k") {
		t.Errorf("拿不到 320k 应降级并说明，实际 quality=%q play=%q status=%q", m.quality, m.playQuality, m.status)
	}
	if v := stripANSI(m.viewPlayerBar()); !contains(v, "128k") {
		t.Errorf("播放栏应显示实际音质:\n%s", v)
	}

	// 连按时旧结果（音质和当前设置对不上）要丢掉
	stale := resolveMsg{req: m.playReq, songMid: "0039MnYb0qxYhV", quality: "std", got: "std", url: audio}
	next, _ := m.Update(stale)
	if next.(Model).playQuality != "128" {
		t.Error("和当前设置不一致的解析结果应被丢弃")
	}
}

// 进度条下的歌词：上一句 / 当前句 / 下一句；「正在播放」页不重复显示；矮终端缩成一行。
func TestMiniLyrics(t *testing.T) {
	m := fakeModel(t, PageExplore)
	m.h = 35
	m.queue = []Track{{Title: "歌", Lyrics: []Lyric{
		{Time: 0, Text: "第一句"}, {Time: 5, Text: "第二句", Translation: "Line two"}, {Time: 10, Text: "第三句"}, {Time: 15, Text: "第四句"},
	}}}
	m.curIndex = 0
	m.pos, m.posWall = 6, 6

	bar := stripANSI(m.viewPlayerBar())
	for _, want := range []string{"第一句", "♪ 第二句", "Line two", "第三句"} {
		if !contains(bar, want) {
			t.Errorf("播放栏下应显示 %q:\n%s", want, bar)
		}
	}
	if contains(bar, "第四句") {
		t.Error("只显示三句")
	}
	// 整体高度不变：多出来的歌词行从内容区里扣
	if got := len(strings.Split(m.View(), "\n")); got != m.h-2 {
		t.Errorf("整屏行数应保持 %d，实际 %d", m.h-2, got)
	}

	m.page = PageNow
	if bar := stripANSI(m.viewPlayerBar()); contains(bar, "♪ 第二句") {
		t.Error("正在播放页本身就是歌词，底下不该再放一份")
	}

	m.page = PageExplore
	m.h = 22
	bar = stripANSI(m.viewPlayerBar())
	if !contains(bar, "♪ 第二句") || contains(bar, "第一句") {
		t.Errorf("矮终端只显示当前一句:\n%s", bar)
	}

	m.queue[0].Lyrics = nil
	if bar := stripANSI(m.viewPlayerBar()); !contains(bar, "暂无歌词") {
		t.Errorf("没有歌词时应占位提示:\n%s", bar)
	}
}
