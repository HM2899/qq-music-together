package main

import (
	"errors"
	"fmt"
	"image"
	_ "image/gif"  // 微信那个 qrcode 接口有时回 JPEG/PNG 不定
	_ "image/jpeg" // 注册解码器，Decode 才能认出来
	_ "image/png"
	"math"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// 二维码渲染成终端字符。为什么值得做：脚本只写出一个 PNG 文件路径，
// 而「打开图片去看」意味着用户要切出去、找到文件、用看图器打开、再切回来，
// 这段时间里二维码很可能已经过期了。直接画在终端里，手机对着屏幕就能扫。

// errQRTooWide 表示二维码模块数超过终端能放的列数。
// 不是致命错误：调用方会退化成「显示文件路径 + 按 o 打开」。
var errQRTooWide = errors.New("终端宽度放不下这个二维码")

// qrQuietZone 是二维码四周必须留的空白模块数。
//
// 标准要求 4 个模块。这里只留 1 个，是因为 4 个会把宽度撑掉 8 列，
// 而终端里每个模块只有 1 列宽、对比度又极高（纯黑白、无抗锯齿、无压缩噪声），
// 1 格静区就足以让扫码器定位了。真出问题的话用户还可以按 o 用图片打开。
const qrQuietZone = 1

// qrMaxVersion 是 QR 标准的最大版本号（40 → 177×177 模块）。
const qrMaxVersion = 40

// qrMatrix 是解码出来的模块网格。dark[r][c] 为真表示第 r 行第 c 列是深色模块。
type qrMatrix struct {
	size int
	dark [][]bool
}

// decodeQRImage 读一张二维码图片，还原成模块网格。
//
// 之所以能做到「不认识二维码格式也能还原」：二维码的四角必有定位图案，
// 所以深色像素的包围盒外边界就是符号边界——静区、白边、多余的留白全部自动裁掉。
// 剩下只需要知道一行有多少个模块。
func decodeQRImage(path string) (qrMatrix, error) {
	f, err := os.Open(path)
	if err != nil {
		return qrMatrix{}, err
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		return qrMatrix{}, fmt.Errorf("二维码图片无法解码: %w", err)
	}

	box := darkBounds(img)
	if box.Empty() {
		return qrMatrix{}, errors.New("二维码图片里没有找到图案")
	}

	n, err := inferModuleCount(box.Dx(), box.Dy())
	if err != nil {
		return qrMatrix{}, err
	}

	// 逐模块取**中心像素**判定颜色。中心采样对缩放插值和图片压缩最鲁棒：
	// 边界像素会被抗锯齿糊掉，中心不会。
	unitX := float64(box.Dx()) / float64(n)
	unitY := float64(box.Dy()) / float64(n)
	m := qrMatrix{size: n, dark: make([][]bool, n)}
	for r := 0; r < n; r++ {
		row := make([]bool, n)
		y := box.Min.Y + int((float64(r)+0.5)*unitY)
		for c := 0; c < n; c++ {
			x := box.Min.X + int((float64(c)+0.5)*unitX)
			row[c] = isDarkPixel(img, x, y)
		}
		m.dark[r] = row
	}
	return m, nil
}

// darkBounds 返回深色像素的包围盒。
func darkBounds(img image.Image) image.Rectangle {
	b := img.Bounds()
	minX, minY := b.Max.X, b.Max.Y
	maxX, maxY := b.Min.X-1, b.Min.Y-1
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if !isDarkPixel(img, x, y) {
				continue
			}
			if x < minX {
				minX = x
			}
			if y < minY {
				minY = y
			}
			if x > maxX {
				maxX = x
			}
			if y > maxY {
				maxY = y
			}
		}
	}
	if maxX < minX || maxY < minY {
		return image.Rectangle{}
	}
	return image.Rect(minX, minY, maxX+1, maxY+1) // Rect 的 Max 是开区间
}

// isDarkPixel 按感知亮度判深浅。
// 用 0.299/0.587/0.114 这组系数（而不是取平均）是因为人眼对绿色最敏感——
// 二维码虽是纯黑白，但扫描件/截图可能带色偏，加权算更接近「看上去是黑还是白」。
func isDarkPixel(img image.Image, x, y int) bool {
	r, g, b, _ := img.At(x, y).RGBA() // 各分量是 0..65535 的预乘值
	lum := (299*r + 587*g + 114*b) / 1000
	return lum < 0x8000
}

// inferModuleCount 从像素宽高推断二维码的模块数（边长）。
//
// 合法边长只有 21 + 4k（21、25、29……177）。图片通常是整数倍缩放，
// 所以真正的那个 N 会同时满足「边长/N 接近整数」和「宽高算出来的一致」。
// 取误差最小的候选；误差过大说明这图根本不是二维码。
func inferModuleCount(w, h int) (int, error) {
	if w <= 0 || h <= 0 {
		return 0, errors.New("二维码尺寸异常")
	}
	side := float64(w)
	if h > w {
		side = float64(h)
	}
	best, bestErr := 0, math.Inf(1)
	for k := 0; k <= (qrMaxVersion-1)/4; k++ {
		n := 21 + 4*k
		ux := float64(w) / float64(n)
		uy := float64(h) / float64(n)
		// 三项误差相加：宽高各自离整数倍多远（缩放是否干净），
		// 以及宽高算出的模块边长是否一致（图片是不是方的）。
		err := math.Abs(ux-math.Round(ux)) +
			math.Abs(uy-math.Round(uy)) +
			math.Abs(ux-uy)
		if err < bestErr {
			best, bestErr = n, err
		}
	}
	// 阈值取得很松：这里只负责挡住「传进来的压根不是二维码」，
	// 真正的正确性由下面的中心采样保证——猜错模块数会画出明显错乱的图案，用户一眼能看出来。
	if best == 0 || bestErr > 1.5 || float64(best) > side {
		return 0, errors.New("这张图看起来不是二维码")
	}
	return best, nil
}

// RenderQR 把二维码图片渲染成终端行。
//
// maxCols 是能用的列数。返回的每一行宽度都不超过 maxCols。
// 放不下或者解不出来时返回错误，由调用方退化成「显示路径」。
func RenderQR(path string, maxCols int) ([]string, error) {
	m, err := decodeQRImage(path)
	if err != nil {
		return nil, err
	}

	logical := m.size + 2*qrQuietZone // 加上静区之后的模块边长
	if maxCols > 0 && logical > maxCols {
		return nil, errQRTooWide
	}
	// 半块字符拼两行模块，所以模块行数必须是偶数。
	if logical%2 != 0 {
		logical++
	}
	// 静区把原图整体往右下推 qrQuietZone 格。
	at := func(r, c int) bool {
		r -= qrQuietZone
		c -= qrQuietZone
		if r < 0 || c < 0 || r >= m.size || c >= m.size {
			return false // 静区是白的
		}
		return m.dark[r][c]
	}

	// 颜色写死纯黑纯白。不能用 lipgloss.Color("0")/("15")：
	// 很多配色方案把 "white" 映射成灰或米色、"black" 映射成深灰，
	// 对比度一掉，手机就扫不出来了。
	const (
		black = lipgloss.Color("#000000")
		white = lipgloss.Color("#FFFFFF")
	)

	lines := make([]string, 0, logical/2)
	var sb strings.Builder
	for r := 0; r < logical; r += 2 {
		sb.Reset()
		for c := 0; c < logical; c++ {
			// ▀（上半块）的上半部分用前景色、下半部分用背景色，
			// 于是 1 个字符格 = 1 列 × 2 行模块。
			color := white
			if at(r, c) {
				color = black
			}
			bg := white
			if at(r+1, c) {
				bg = black
			}
			sb.WriteString(lipgloss.NewStyle().
				Foreground(color).
				Background(bg).
				Render("▀"))
		}
		lines = append(lines, sb.String())
	}
	return lines, nil
}
