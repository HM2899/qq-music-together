package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
)

// ---------- 音质 ----------
//
// 档位和脚本 resolve --quality 的取值一一对应。实测能真正下载的只有这四档：
// Hi-Res（RS01）会给地址但下载 404，臻品母带（AI00）不给地址，所以不提供。
// 所选档位拿不到时脚本会自动往下降，并告诉我们实际拿到的是哪一档。

var qualityOrder = []string{"std", "128", "320", "flac"}

var qualityLabels = map[string]string{
	"std":     "标准",
	"128":     "128k",
	"320":     "320k",
	"flac":    "无损",
	"default": "默认",
}

// defaultQuality 沿用加音质选择之前的行为（不指定文件名时服务端给的就是标准 m4a）。
const defaultQuality = "std"

func qualityLabel(q string) string {
	if l, ok := qualityLabels[q]; ok {
		return l
	}
	return q
}

func nextQuality(q string) string {
	for i, k := range qualityOrder {
		if k == q {
			return qualityOrder[(i+1)%len(qualityOrder)]
		}
	}
	return defaultQuality
}

// ---------- 旧版设置文件（只读，用于迁移） ----------
//
// 音质设置现在和播放状态一起存在 SQLite 里（见 store.go）。之前的版本存在
// ~/.config/qqmusic-tui/config.json：库里还没有音质时读它一次，之后存盘就进库了。
// QQMUSIC_TUI_CONFIG 可覆盖路径（测试用）。

type tuiConfig struct {
	Quality string `json:"quality"`
}

func configPath() string {
	if p := os.Getenv("QQMUSIC_TUI_CONFIG"); p != "" {
		return p
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "qqmusic-tui", "config.json")
}

func loadConfig() tuiConfig {
	cfg := tuiConfig{Quality: defaultQuality}
	p := configPath()
	if p == "" {
		return cfg
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return cfg
	}
	var saved tuiConfig
	if json.Unmarshal(data, &saved) == nil {
		if _, ok := qualityLabels[saved.Quality]; ok && saved.Quality != "default" {
			cfg.Quality = saved.Quality
		}
	}
	return cfg
}

// cycleQuality 换到下一档音质并存进 SQLite；正在播放的歌按新音质从当前位置接着放。
func (m *Model) cycleQuality() tea.Cmd {
	m.quality = nextQuality(m.quality)
	m.status = "音质：" + qualityLabel(m.quality)
	m.persist(true)

	cur, ok := m.current()
	if !ok || cur.DirectURL != "" || cur.SongMid == "" || m.api == nil || m.mpv == nil {
		return nil // 无法重新解析或测试注入了本地音频：新音质在后续解析时生效
	}
	// 记下接着放的位置，解析回来后在这个位置载入（见 onResolve）。
	m.resumeAt = m.displayPos()
	m.status += "，正在切换…"
	return m.cmdResolve(m.playReq, cur)
}
