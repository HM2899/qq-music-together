package main

import (
	"path/filepath"
	"testing"
	"time"
)

func openTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.db")
	s, err := OpenStore(path)
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	t.Cleanup(s.Close)
	return s, path
}

// 存进去什么，重新打开数据库读出来就是什么（歌词不进库）。
func TestStoreRoundTrip(t *testing.T) {
	s, path := openTestStore(t)
	if _, ok, err := s.Load(); ok || err != nil {
		t.Fatalf("第一次运行应读不到东西，实际 ok=%v err=%v", ok, err)
	}
	st := savedState{
		Queue: []Track{
			{ID: "1", SongMid: "a", MediaMid: "am", Title: "第一首", Artist: "甲", Duration: 200, CoverURL: "https://x/a.jpg",
				Lyrics: []Lyric{{Text: "不该进库"}}},
			{ID: "2", SongMid: "b", Title: "第二首", Duration: 180},
		},
		CurIndex: 1, Position: 42.5, Volume: 35, Muted: true, Quality: "flac",
	}
	if err := s.Save(st, true); err != nil {
		t.Fatal(err)
	}
	s.Close()

	s2, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	got, ok, err := s2.Load()
	if !ok || err != nil {
		t.Fatalf("应读到上次的状态，ok=%v err=%v", ok, err)
	}
	if len(got.Queue) != 2 || got.Queue[0].Title != "第一首" || got.Queue[0].MediaMid != "am" || got.Queue[0].CoverURL != "https://x/a.jpg" {
		t.Errorf("队列不对: %+v", got.Queue)
	}
	if got.Queue[0].Lyrics != nil {
		t.Error("歌词不该存进数据库")
	}
	if got.CurIndex != 1 || got.Position != 42.5 || got.Volume != 35 || !got.Muted || got.Quality != "flac" {
		t.Errorf("标量不对: %+v", got)
	}
	if s2.LoadQuality() != "flac" {
		t.Error("LoadQuality 应读到 flac")
	}
}

// 节流：只有位置在变时 5 秒内不重复写；切歌之类的变化立刻写。
func TestStoreThrottle(t *testing.T) {
	s, _ := openTestStore(t)
	st := savedState{Queue: []Track{{SongMid: "a"}, {SongMid: "b"}}, CurIndex: 0, Position: 1, Volume: 70, Quality: "std"}
	_ = s.Save(st, false)

	st.Position = 3
	_ = s.Save(st, false)
	if got, _, _ := s.Load(); got.Position != 1 {
		t.Errorf("5 秒内只有位置变化不该写盘，实际位置 %.1f", got.Position)
	}

	st.CurIndex = 1 // 切歌：立刻写
	_ = s.Save(st, false)
	if got, _, _ := s.Load(); got.CurIndex != 1 || got.Position != 3 {
		t.Errorf("切歌应立刻写盘，实际 %+v", got)
	}

	st.Position = 9
	_ = s.Save(st, true) // 退出时强制
	if got, _, _ := s.Load(); got.Position != 9 {
		t.Errorf("强制写盘应生效，实际 %.1f", got.Position)
	}

	// 队列缩短到下标越界：读出来当作没在放，不能越界
	st.Queue = st.Queue[:1]
	_ = s.Save(st, true)
	if got, _, _ := s.Load(); got.CurIndex != -1 {
		t.Errorf("下标越界应回落为 -1，实际 %d", got.CurIndex)
	}
}

// 启动恢复：队列、位置、音量摆回来，暂停着；空闲 mpv 的 tick 不能把位置冲成 0；
// 按空格从存下的位置接着放。
func TestRestoreAndResume(t *testing.T) {
	audio := makeTestAudio(t)
	mpv, err := StartMpv(70, "--ao=null", "--no-config")
	if err != nil {
		t.Fatalf("启动 mpv 失败: %v", err)
	}
	defer mpv.Close()

	s, _ := openTestStore(t)
	_ = s.Save(savedState{
		Queue:    []Track{{ID: "1", SongMid: "0039MnYb0qxYhV", Title: "假歌一号", Duration: 10}},
		CurIndex: 0, Position: 4.2, Volume: 40, Quality: "320",
	}, true)

	m := fakeModel(t, PageExplore)
	m.api = fakeAPI(t, "FAKE_AUDIO="+audio)
	m.mpv = mpv
	m = m.WithStore(s)

	if !m.restorePending || !m.paused || m.curIndex != 0 || m.volume != 40 || m.quality != "320" {
		t.Fatalf("应恢复成暂停态，实际 pending=%v paused=%v idx=%d vol=%d q=%q", m.restorePending, m.paused, m.curIndex, m.volume, m.quality)
	}
	if !contains(m.status, "已恢复上次的播放") || !contains(stripANSI(m.viewPlayerBar()), "00:04") {
		t.Errorf("应提示已恢复并显示存下的位置，status=%q\n%s", m.status, stripANSI(m.viewPlayerBar()))
	}

	m = m.onTick()
	if m.displayPos() < 4 {
		t.Fatalf("空闲 mpv 的 tick 不该把恢复的位置冲掉，实际 %.2f", m.displayPos())
	}

	m = press(t, m, " ")
	if m.restorePending || m.paused || !contains(m.status, "已从 00:04 接着播放") {
		t.Fatalf("按空格应从原位置接着放，实际 pending=%v paused=%v status=%q", m.restorePending, m.paused, m.status)
	}
	time.Sleep(300 * time.Millisecond)
	if pos := mpv.TimePos(); pos < 4 {
		t.Errorf("mpv 应从 4.2 秒附近开始放，实际 %.2f", pos)
	}
}

// 恢复后直接切歌：按正常流程开新的一首，恢复状态作废。
func TestRestoreThenSkip(t *testing.T) {
	s, _ := openTestStore(t)
	_ = s.Save(savedState{
		Queue:    []Track{{SongMid: "a", Title: "甲", Duration: 10}, {SongMid: "b", Title: "乙", Duration: 10}},
		CurIndex: 0, Position: 5, Volume: 70, Quality: "std",
	}, true)
	m := fakeModel(t, PageExplore).WithStore(s)
	next, _ := m.Update(keyMsg("n"))
	m = next.(Model)
	if m.restorePending || m.curIndex != 1 || m.displayPos() > 0.5 {
		t.Errorf("切歌后应从下一首开头放，实际 pending=%v idx=%d pos=%.1f", m.restorePending, m.curIndex, m.displayPos())
	}
}

// 第一次运行（库是空的）：什么都不恢复，也不报错。
func TestRestoreEmpty(t *testing.T) {
	s, _ := openTestStore(t)
	m := fakeModel(t, PageExplore).WithStore(s)
	if m.restorePending || len(m.queue) != 0 || m.status != "" {
		t.Errorf("空库不该恢复任何东西，实际 pending=%v queue=%d status=%q", m.restorePending, len(m.queue), m.status)
	}
}
