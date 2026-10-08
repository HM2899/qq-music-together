package main

import (
	"fmt"
	"image"
	_ "image/jpeg" // QQ 音乐的封面是 jpg
	_ "image/png"
	"io"
	"net/http"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// ---------- 专辑封面 ----------
//
// 用半块字符 ▀ 画：每个字符格上半是前景色、下半是背景色，等于两个像素，
// 终端字符大约 1:2 的宽高比于是正好得到方形像素。颜色直接写 24 位真彩色转义——
// 不走 lipgloss，免得它按终端能力把颜色降采样成 256 色（封面就糊了）。
//
// 为什么不用 kitty 图形协议：bubbletea 是差分渲染、按行截断，图片转义序列会被切坏或残留。

// coverMsg 是一张封面下载 + 解码的结果。
type coverMsg struct {
	url string
	img image.Image
	err error
}

// coverCache 存解码后的图和渲染好的行。指针类型：Model 是值，复制时共用同一份缓存；
// 只有 Update goroutine 会改它（下载在命令 goroutine 里，结果经 coverMsg 回到 Update 才入库）。
type coverCache struct {
	imgs     map[string]image.Image
	failed   map[string]bool
	loading  map[string]bool
	rendered map[string][]string // url + 尺寸 → 渲染好的行
}

func newCoverCache() *coverCache {
	return &coverCache{
		imgs:     map[string]image.Image{},
		failed:   map[string]bool{},
		loading:  map[string]bool{},
		rendered: map[string][]string{},
	}
}

// fetchCover 下载并解码一张封面。是变量而不是函数：测试里换成不联网的版本。
var fetchCover = func(url string) (image.Image, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Referer", "https://y.qq.com/")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("封面下载失败（HTTP %d）", resp.StatusCode)
	}
	img, _, err := image.Decode(io.LimitReader(resp.Body, 8<<20))
	return img, err
}

// coverWanted 返回当前该显示（因而该去下载）的封面地址：只有在「正在播放」页、
// 当前曲目有封面、终端也放得下时才要。
func (m Model) coverWanted() string {
	if m.page != PageNow || m.focus != FocusList {
		return ""
	}
	cur, ok := m.current()
	if !ok || strings.TrimSpace(cur.CoverURL) == "" {
		return ""
	}
	if cols, _ := m.coverSize(m.w-2, m.bodyH()-2); cols == 0 {
		return ""
	}
	return cur.CoverURL
}

// ensureCover 需要且还没有的封面就发起下载。每次 Update 之后调一次。
func (m Model) ensureCover() tea.Cmd {
	url := m.coverWanted()
	if url == "" || m.covers == nil {
		return nil
	}
	c := m.covers
	if c.imgs[url] != nil || c.failed[url] || c.loading[url] {
		return nil
	}
	c.loading[url] = true
	return func() tea.Msg {
		img, err := fetchCover(url)
		return coverMsg{url: url, img: img, err: err}
	}
}

func (m Model) onCover(msg coverMsg) Model {
	if m.covers == nil {
		return m
	}
	delete(m.covers.loading, msg.url)
	if msg.err != nil || msg.img == nil {
		m.covers.failed[msg.url] = true // 失败就不再重试这张（不然每次重绘都去下）
		return m
	}
	m.covers.imgs[msg.url] = msg.img
	return m
}

// coverSize 决定封面占多少列 × 行（行数 = 列数 / 2，正好方形）。放不下返回 0。
func (m Model) coverSize(w, h int) (cols, rows int) {
	if w < 70 || h < 8 {
		return 0, 0
	}
	cols = w * 3 / 10
	if cols > 36 {
		cols = 36
	}
	rows = cols / 2
	if rows > h-1 {
		rows = h - 1
		cols = rows * 2
	}
	if cols < 12 {
		return 0, 0
	}
	return cols, rows
}

// coverLines 返回当前曲目封面渲染好的行；还没下载好返回占位框，没有封面返回 nil。
func (m Model) coverLines(cols, rows int) []string {
	cur, ok := m.current()
	if !ok || m.covers == nil || cur.CoverURL == "" {
		return nil
	}
	c := m.covers
	img := c.imgs[cur.CoverURL]
	if img == nil {
		if c.failed[cur.CoverURL] {
			return nil
		}
		return coverPlaceholder(cols, rows)
	}
	key := fmt.Sprintf("%s|%dx%d", cur.CoverURL, cols, rows)
	if lines, ok := c.rendered[key]; ok {
		return lines
	}
	lines := renderHalfBlocks(img, cols, rows)
	c.rendered[key] = lines
	return lines
}

// coverPlaceholder 是下载中的占位：同样大小的暗色方块，免得封面到了之后右边的文字跳位置。
func coverPlaceholder(cols, rows int) []string {
	out := make([]string, rows)
	for i := range out {
		out[i] = styleDim.Render(strings.Repeat("░", cols))
	}
	if rows > 0 {
		label := "封面加载中"
		pad := (cols - 10) / 2
		if pad > 0 {
			out[rows/2] = styleDim.Render(strings.Repeat("░", pad) + label + strings.Repeat("░", cols-pad-10))
		}
	}
	return out
}

// renderHalfBlocks 把图片缩放到 cols × rows*2 个像素（每个目标像素取源图对应区域的平均色），
// 再两行像素拼成一行 ▀。
func renderHalfBlocks(img image.Image, cols, rows int) []string {
	b := img.Bounds()
	if b.Dx() == 0 || b.Dy() == 0 || cols <= 0 || rows <= 0 {
		return nil
	}
	ph := rows * 2
	pixel := func(x, y int) (r, g, bl uint32) {
		x0 := b.Min.X + x*b.Dx()/cols
		x1 := b.Min.X + (x+1)*b.Dx()/cols
		y0 := b.Min.Y + y*b.Dy()/ph
		y1 := b.Min.Y + (y+1)*b.Dy()/ph
		if x1 <= x0 {
			x1 = x0 + 1
		}
		if y1 <= y0 {
			y1 = y0 + 1
		}
		var sr, sg, sb, n uint64
		for yy := y0; yy < y1; yy++ {
			for xx := x0; xx < x1; xx++ {
				cr, cg, cb, _ := img.At(xx, yy).RGBA()
				sr += uint64(cr)
				sg += uint64(cg)
				sb += uint64(cb)
				n++
			}
		}
		return uint32(sr / n >> 8), uint32(sg / n >> 8), uint32(sb / n >> 8)
	}
	lines := make([]string, rows)
	var sb strings.Builder
	for row := 0; row < rows; row++ {
		sb.Reset()
		for x := 0; x < cols; x++ {
			tr, tg, tb := pixel(x, row*2)
			br, bg, bb := pixel(x, row*2+1)
			fmt.Fprintf(&sb, "\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm▀", tr, tg, tb, br, bg, bb)
		}
		sb.WriteString("\x1b[0m")
		lines[row] = sb.String()
	}
	return lines
}
