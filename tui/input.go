package main

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"
)

// textInput 是一个手写的单行输入框。
//
// 为什么不直接用 bubbles/textinput：那会把 bubbles 整条依赖线拉进来
// （bubbletea v1.3 对应的是另一条版本线，还会带 muesli/reflow 等传递依赖），
// 而这里真正需要的只有「收字符、光标移动、退格、删词」。
// 它多出来的主要是光标闪烁和补全建议——前者要再挂一个 tick（和现有的 500ms tick 打架），
// 后者用不上。
//
// 关于中文：终端在 IME 层就完成了组合，应用收到的只有已提交的 UTF-8 字符，
// 所以拼音输入不需要任何特殊处理。真正要注意的是**光标定位必须按显示宽度算**
// （lipgloss.Width），按字节数或 rune 数算的话，中英混排时反色块会偏位。
type textInput struct {
	runes  []rune
	cursor int // 0..len(runes)，单位是 rune
}

func (t *textInput) SetValue(s string) {
	t.runes = []rune(s)
	t.cursor = len(t.runes)
}

func (t textInput) Value() string { return string(t.runes) }

func (t textInput) Empty() bool { return len(t.runes) == 0 }

func (t *textInput) Clear() {
	t.runes = t.runes[:0]
	t.cursor = 0
}

// Insert 在光标处插入若干字符。
func (t *textInput) Insert(chars ...rune) {
	if len(chars) == 0 {
		return
	}
	rest := append([]rune(nil), t.runes[t.cursor:]...)
	t.runes = append(t.runes[:t.cursor], chars...)
	t.runes = append(t.runes, rest...)
	t.cursor += len(chars)
}

// Backspace 删掉光标前的一个字符。
func (t *textInput) Backspace() {
	if t.cursor == 0 {
		return
	}
	t.runes = append(t.runes[:t.cursor-1], t.runes[t.cursor:]...)
	t.cursor--
}

// Delete 删掉光标处的一个字符。
func (t *textInput) Delete() {
	if t.cursor >= len(t.runes) {
		return
	}
	t.runes = append(t.runes[:t.cursor], t.runes[t.cursor+1:]...)
}

// DeleteWord 删掉光标前的一个词（Ctrl+W）。
// 中文没有空格分词，所以这里退化成「先吃掉空白，再吃掉一串同类字符」，
// 对中文输入来说表现为一次删掉连续的一个汉字块，符合直觉。
func (t *textInput) DeleteWord() {
	for t.cursor > 0 && unicode.IsSpace(t.runes[t.cursor-1]) {
		t.runes = append(t.runes[:t.cursor-1], t.runes[t.cursor:]...)
		t.cursor--
	}
	if t.cursor == 0 {
		return
	}
	last := t.runes[t.cursor-1]
	sameKind := func(r rune) bool {
		switch {
		case unicode.IsDigit(last):
			return unicode.IsDigit(r)
		case isCJK(last):
			return isCJK(r)
		default:
			return !unicode.IsSpace(r) && !unicode.IsDigit(r) && !isCJK(r)
		}
	}
	for t.cursor > 0 && sameKind(t.runes[t.cursor-1]) {
		t.runes = append(t.runes[:t.cursor-1], t.runes[t.cursor:]...)
		t.cursor--
	}
}

func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r)
}

func (t *textInput) MoveLeft() {
	if t.cursor > 0 {
		t.cursor--
	}
}

func (t *textInput) MoveRight() {
	if t.cursor < len(t.runes) {
		t.cursor++
	}
}

func (t *textInput) Home() { t.cursor = 0 }
func (t *textInput) End()  { t.cursor = len(t.runes) }

// inputHint 是输入框上面那行的按键提示。
// 特意写明 Tab：输入框是 fzf 语义，回车之后焦点不会自己离开，
// 不知道 Tab 能翻页的用户会觉得「被困在搜索框里了」。
const inputHint = "回车搜索   Esc 退出输入   Tab 翻页   Ctrl+W 删词   Ctrl+U 清空"

// View 渲染输入框，总宽度正好是 width 列。
//
// 横向滚动：文本比框宽时，从光标处往回数够一屏，保证光标永远可见。
func (t textInput) View(width int, placeholder string, focused bool) string {
	if width < 4 {
		width = 4
	}
	inner := width - 2 // 两侧各留一个空格

	body := t.render(inner, placeholder, focused)
	pad := inner - lipgloss.Width(body)
	if pad < 0 {
		pad = 0
	}

	line := " " + body + strings.Repeat(" ", pad) + " "
	border := colDim
	if focused {
		border = colGreen
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(border).
		Width(width - 2).
		Render(line)
}

func (t textInput) render(width int, placeholder string, focused bool) string {
	// 空且未聚焦：显示占位符，不做光标。
	if len(t.runes) == 0 {
		if !focused {
			return styleDim.Render(placeholder)
		}
		return renderCursor(" ", styleActive)
	}

	// 从光标往前凑够一屏，保证光标可见（超出的左侧部分裁掉）。
	start := 0
	if focused {
		used := 0
		start = t.cursor
		for start > 0 {
			w := runeWidth(t.runes[start-1])
			if used+w > width-1 { // 留一格给光标
				break
			}
			used += w
			start--
		}
	}

	visible := []rune(t.runes[start:])
	if !focused {
		return styleText.Render(truncateRunes(visible, width))
	}

	// 光标落在第 cursor-start 个 rune 上。
	idx := t.cursor - start
	if idx >= len(visible) {
		// 光标在末尾：给一个反色的空格块
		return styleText.Render(string(visible)) + renderCursor(" ", styleActive)
	}
	before := styleText.Render(string(visible[:idx]))
	cursorChar := string(visible[idx])
	after := styleText.Render(string(visible[idx+1:]))
	return before + renderCursor(cursorChar, styleActive) + after
}

// renderCursor 用反色画一个光标块。
// 用反色而不是下划线/竖线：中文字符宽度是 2，竖线画不准，反色块跟着字符走永远对齐。
func renderCursor(ch string, style lipgloss.Style) string {
	return style.Reverse(true).Render(ch)
}

func runeWidth(r rune) int {
	return lipgloss.Width(string(r))
}

// truncateRunes 按显示宽度截断 rune 序列。
func truncateRunes(rs []rune, width int) string {
	used := 0
	for i, r := range rs {
		w := runeWidth(r)
		if used+w > width {
			return string(rs[:i])
		}
		used += w
	}
	return string(rs)
}
