package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// 这里**不**引入二维码编码库：要测的是「decodeQRImage 能不能把一张图片还原成模块网格」，
// 而不是「这个网格是不是合法二维码」。所以自己画一张有定位图案的网格就够了——
// 顺带还能测出「模块数猜错」这类错误，因为猜错的话还原出来的网格必然对不上。

// qrTestGrid 造一个 size×size 的假二维码网格：
// 三个角放定位图案（真二维码也是这样，正是它们保证了深色像素的包围盒等于符号边界）。
func qrTestGrid(size int) [][]bool {
	g := make([][]bool, size)
	for r := range g {
		g[r] = make([]bool, size)
	}
	// 定位图案：7×7 外框黑 → 5×5 白 → 3×3 黑
	finder := func(r0, c0 int) {
		for r := 0; r < 7; r++ {
			for c := 0; c < 7; c++ {
				on := r == 0 || r == 6 || c == 0 || c == 6 ||
					(r >= 2 && r <= 4 && c >= 2 && c <= 4)
				if r0+r < size && c0+c < size {
					g[r0+r][c0+c] = on
				}
			}
		}
	}
	finder(0, 0)
	finder(0, size-7)
	finder(size-7, 0)

	// 其余部分填一个确定的伪随机图案（避开定位图案和它们周围的隔离带）。
	seed := uint32(0x9e3779b9)
	for r := 0; r < size; r++ {
		for c := 0; c < size; c++ {
			nearFinder := (r < 8 && c < 8) || (r < 8 && c >= size-8) || (r >= size-8 && c < 8)
			if nearFinder {
				continue
			}
			seed = seed*1664525 + 1013904223
			g[r][c] = seed>>31 == 1
		}
	}
	return g
}

// writeQRTestPNG 把网格写成 PNG 文件：每个模块放大 scale 倍，四周留 margin 像素白边。
// margin 模拟真实图片里的静区——正是它让「包围盒裁静区」这一步有意义。
func writeQRTestPNG(t *testing.T, grid [][]bool, scale, margin int) string {
	t.Helper()
	n := len(grid)
	side := n*scale + 2*margin
	img := image.NewRGBA(image.Rect(0, 0, side, side))
	// 先铺白（RGBA 零值是透明黑，不铺的话整张图都是「深色」）
	for y := 0; y < side; y++ {
		for x := 0; x < side; x++ {
			img.Set(x, y, color.RGBA{255, 255, 255, 255})
		}
	}
	for r := 0; r < n; r++ {
		for c := 0; c < n; c++ {
			if !grid[r][c] {
				continue
			}
			for dy := 0; dy < scale; dy++ {
				for dx := 0; dx < scale; dx++ {
					img.Set(margin+c*scale+dx, margin+r*scale+dy, color.RGBA{0, 0, 0, 255})
				}
			}
		}
	}
	path := filepath.Join(t.TempDir(), "qr.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	return path
}

// 逐模块还原：各种版本 × 各种缩放/静区组合都必须一模一样。
func TestDecodeQRImageRecoversModules(t *testing.T) {
	cases := []struct {
		size, scale, margin int
	}{
		{21, 1, 0}, {21, 4, 10}, {21, 3, 1},
		{25, 5, 0}, {29, 4, 7}, {33, 6, 2}, {41, 3, 9},
	}
	for _, tc := range cases {
		grid := qrTestGrid(tc.size)
		path := writeQRTestPNG(t, grid, tc.scale, tc.margin)

		got, err := decodeQRImage(path)
		if err != nil {
			t.Errorf("size=%d scale=%d margin=%d: 解码失败: %v",
				tc.size, tc.scale, tc.margin, err)
			continue
		}
		if got.size != tc.size {
			t.Errorf("size=%d scale=%d margin=%d: 模块数应为 %d，实际 %d",
				tc.size, tc.scale, tc.margin, tc.size, got.size)
			continue
		}
		bad := 0
		for r := 0; r < tc.size; r++ {
			for c := 0; c < tc.size; c++ {
				if got.dark[r][c] != grid[r][c] {
					bad++
				}
			}
		}
		if bad != 0 {
			t.Errorf("size=%d scale=%d margin=%d: 有 %d 个模块还原错了",
				tc.size, tc.scale, tc.margin, bad)
		}
	}
}

// 渲染出来的每一行宽度都不能超过 maxCols，行数也要对得上（每两行模块合成一行字符）。
func TestRenderQRFitsWidth(t *testing.T) {
	grid := qrTestGrid(29)
	path := writeQRTestPNG(t, grid, 4, 8)

	lines, err := RenderQR(path, 200)
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	// 29 个模块 + 两侧各 1 格静区 = 31，是奇数，会被补成 32 → 16 行字符。
	wantRows := (29 + 2*qrQuietZone + 1) / 2
	if len(lines) != wantRows {
		t.Errorf("应有 %d 行，实际 %d 行", wantRows, len(lines))
	}
	cols := 29 + 2*qrQuietZone + 1 // 补齐后的模块列数
	for i, l := range lines {
		if w := lipgloss.Width(l); w != cols {
			t.Errorf("第 %d 行宽度应为 %d 列，实际 %d", i, cols, w)
		}
	}
}

// 终端太窄时必须报 errQRTooWide，好让调用方退化成「显示文件路径」。
func TestRenderQRTooWide(t *testing.T) {
	grid := qrTestGrid(33)
	path := writeQRTestPNG(t, grid, 2, 4)

	if _, err := RenderQR(path, 20); err != errQRTooWide {
		t.Errorf("放不下时应返回 errQRTooWide，实际 %v", err)
	}
	// 刚好放得下就不该报错：33 + 2 静区 = 35 列。
	if _, err := RenderQR(path, 35); err != nil {
		t.Errorf("宽度刚好够时不该报错: %v", err)
	}
}

// 坏输入不能 panic，必须老老实实返回 error。
func TestRenderQRBadInput(t *testing.T) {
	if _, err := RenderQR(filepath.Join(t.TempDir(), "不存在.png"), 200); err == nil {
		t.Error("文件不存在时应报错")
	}

	// 一张纯白图：没有深色像素，找不到图案。
	white := image.NewRGBA(image.Rect(0, 0, 60, 60))
	for y := 0; y < 60; y++ {
		for x := 0; x < 60; x++ {
			white.Set(x, y, color.RGBA{255, 255, 255, 255})
		}
	}
	path := filepath.Join(t.TempDir(), "white.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, white); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if _, err := RenderQR(path, 200); err == nil {
		t.Error("纯白图应报「找不到图案」")
	}

	// 一个不是图片的文件。
	junk := filepath.Join(t.TempDir(), "junk.png")
	if err := os.WriteFile(junk, []byte("这不是图片"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := RenderQR(junk, 200); err == nil {
		t.Error("非图片文件应报错")
	}
}

// 模块数推断：合法边长只有 21+4k，且必须容忍宽高不一致的脏输入。
func TestInferModuleCount(t *testing.T) {
	ok := []struct{ w, h, want int }{
		{21, 21, 21},
		{84, 84, 21},   // 4 倍缩放
		{290, 290, 29}, // 10 倍缩放
		{132, 132, 33}, // 4 倍缩放
		{29 * 7, 29 * 7, 29},
		{84, 85, 21}, // 差一个像素
	}
	for _, tc := range ok {
		got, err := inferModuleCount(tc.w, tc.h)
		if err != nil {
			t.Errorf("%dx%d: 不该报错: %v", tc.w, tc.h, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%dx%d: 应为 %d，实际 %d", tc.w, tc.h, tc.want, got)
		}
	}

	if _, err := inferModuleCount(0, 0); err == nil {
		t.Error("零尺寸应报错")
	}
}
