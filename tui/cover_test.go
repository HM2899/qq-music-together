package main

import (
	"errors"
	"image"
	"image/color"
	"strings"
	"testing"
)

// 上红下蓝的 4×4 图：渲染成 2 列 × 1 行时，每格前景（上半）是红、背景（下半）是蓝。
func TestRenderHalfBlocks(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			c := color.RGBA{255, 0, 0, 255}
			if y >= 2 {
				c = color.RGBA{0, 0, 255, 255}
			}
			img.Set(x, y, c)
		}
	}
	lines := renderHalfBlocks(img, 2, 1)
	if len(lines) != 1 {
		t.Fatalf("应渲染 1 行，实际 %d", len(lines))
	}
	want := "\x1b[38;2;255;0;0m\x1b[48;2;0;0;255m▀"
	if strings.Count(lines[0], want) != 2 || !strings.HasSuffix(lines[0], "\x1b[0m") {
		t.Errorf("渲染结果不对: %q", lines[0])
	}
}

func withFakeCover(t *testing.T, fn func(string) (image.Image, error)) *int {
	t.Helper()
	calls := 0
	old := fetchCover
	fetchCover = func(url string) (image.Image, error) {
		calls++
		return fn(url)
	}
	t.Cleanup(func() { fetchCover = old })
	return &calls
}

func coverModel(t *testing.T) Model {
	m := fakeModel(t, PageExplore)
	m.w, m.h = 120, 35
	m.queue = []Track{{Title: "晴天", Artist: "周杰伦", CoverURL: "https://x/cover.jpg",
		Lyrics: []Lyric{{Time: 0, Text: "故事的小黄花"}}}}
	m.curIndex = 0
	return m
}

// 进正在播放页 → 自动下载封面 → 封面画在左边、歌词在右边；同一张只下一次。
func TestCoverOnNowPage(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 30, 30))
	for i := range img.Pix {
		img.Pix[i] = 200
	}
	calls := withFakeCover(t, func(string) (image.Image, error) { return img, nil })

	m := coverModel(t)
	// 别的页不下
	next, cmd := m.Update(keyMsg("1"))
	m = runCmd(t, next.(Model), cmd)
	if *calls != 0 {
		t.Fatal("不在正在播放页时不该下载封面")
	}

	next, cmd = m.Update(keyMsg("3"))
	m = next.(Model)
	if v := stripANSI(m.View()); !contains(v, "封面加载中") {
		t.Errorf("下载中应有占位:\n%s", v)
	}
	m = runCmd(t, m, cmd)
	if *calls != 1 {
		t.Fatalf("应下载一次封面，实际 %d 次", *calls)
	}
	v := m.View()
	if !strings.Contains(v, "\x1b[38;2;200;200;200m") || !strings.Contains(v, "▀") {
		t.Error("封面应以真彩色半块字符画出来")
	}
	plain := stripANSI(v)
	if !contains(plain, "晴天") || !contains(plain, "故事的小黄花") {
		t.Errorf("右边仍应有歌名和歌词:\n%s", plain)
	}
	// 每一行的封面都在歌词左边：找到歌词所在行，前面必须有封面像素或留白
	for _, line := range strings.Split(v, "\n") {
		if strings.Contains(line, "故事的小黄花") && strings.Index(stripANSI(line), "故事的小黄花") < 30 {
			t.Errorf("歌词应在封面右边，实际在第 %d 列", strings.Index(stripANSI(line), "故事的小黄花"))
		}
	}

	// 再触发一次 Update 不会重复下载
	next, cmd = m.Update(tickMsg{})
	if c := next.(Model).ensureCover(); c != nil || *calls != 1 {
		t.Error("已经有的封面不该重复下载")
	}
	_ = cmd
}

// 下载失败：不显示封面也不报错，歌词占满整页；同一张不反复重试。
func TestCoverFailureFallsBack(t *testing.T) {
	calls := withFakeCover(t, func(string) (image.Image, error) { return nil, errors.New("网络不通") })
	m := coverModel(t)
	next, cmd := m.Update(keyMsg("3"))
	m = runCmd(t, next.(Model), cmd)
	v := stripANSI(m.View())
	if contains(v, "封面加载中") || strings.Contains(m.View(), "\x1b[38;2;") {
		t.Errorf("失败后不该留占位或封面:\n%s", v)
	}
	if !contains(v, "故事的小黄花") {
		t.Error("歌词仍应显示")
	}
	if m.ensureCover() != nil || *calls != 1 {
		t.Error("失败过的封面不该反复重试")
	}
}

// 终端太窄：不画封面，也不下载。
func TestCoverSkippedWhenNarrow(t *testing.T) {
	calls := withFakeCover(t, func(string) (image.Image, error) { return image.NewRGBA(image.Rect(0, 0, 2, 2)), nil })
	m := coverModel(t)
	m.w = 60
	next, cmd := m.Update(keyMsg("3"))
	m = runCmd(t, next.(Model), cmd)
	if *calls != 0 || strings.Contains(m.View(), "▀") {
		t.Error("窄终端不该下载或显示封面")
	}
}
