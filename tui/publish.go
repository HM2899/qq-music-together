package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// PubTrack 是发布给桌面 shell 的曲目元数据。
// 刻意不复用接口那边的 Track：那个是「服务端返回了什么」，
// 这个是「桌面顶栏要显示什么」，两者的 JSON 形状本来就不同
// （这里的小写 key 是 TuiPlayerService.qml 认的契约，改名时绝不能动）。
type PubTrack struct {
	Title  string `json:"title"`
	Artist string `json:"artist"`
	Album  string `json:"album"`
}

// NowPlaying 是 TUI 每次 tick 写给桌面 shell 的当前播放状态。
// 字段名就是 JSON key，桌面侧 Services/TuiPlayerService.qml 按同一套名字解析。
type NowPlaying struct {
	PID       int      `json:"pid"`
	UpdatedAt float64  `json:"updatedAt"` // Unix 秒（带小数）；桌面侧靠它判活
	Playing   bool     `json:"playing"`
	Paused    bool     `json:"paused"`
	Position  float64  `json:"position"`
	Length    float64  `json:"length"`
	Track     PubTrack `json:"track"`
	Lyrics    []Lyric  `json:"lyrics"`
}

// Publisher 把播放状态原子地写到桌面 shell 读取的状态文件。
// 每 200ms 一次（跟 UI 的 tick 同频）、文件不到 2KB，同步写足够，
// 不值得再开 goroutine 和 channel。
//
// 桌面侧不再假设「position 就是此刻的位置」：它拿 updatedAt 按墙钟外推
// （见 Services/TuiPlayerService.qml 的 livePosition），所以这里只发布原始采样值。
type Publisher struct {
	path string
}

// NewPublisher 决定状态文件路径。
// QQMUSIC_TUI_STATE_FILE 可覆盖——测试和无头跑用它指到临时目录，不污染用户真实的 ~/.cache。
// 默认路径必须与桌面侧 Common/Paths.qml 的 qqmusicTuiState 完全一致：
// 两边都拼 ~/.cache，而不是各自的 XDG 变量，否则用户改了 XDG_CACHE_HOME 就对不上了。
func NewPublisher() *Publisher {
	if p := os.Getenv("QQMUSIC_TUI_STATE_FILE"); p != "" {
		return &Publisher{path: p}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil // 拿不到家目录就干脆不发布，TUI 本身照常能用
	}
	return &Publisher{path: filepath.Join(home, ".cache", "qqmusic-tui", "now.json")}
}

// Publish 原子写入：先写同目录的临时文件再 rename。
// rename 在同一文件系统内是原子的，读端因此永远看不到写了一半的 JSON。
func (p *Publisher) Publish(np NowPlaying) {
	if p == nil {
		return
	}
	data, err := json.Marshal(np)
	if err != nil {
		return
	}
	dir := filepath.Dir(p.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	tmp, err := os.CreateTemp(dir, "now-*.json")
	if err != nil {
		return
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return
	}
	if err := os.Rename(name, p.path); err != nil {
		os.Remove(name)
	}
}

// Remove 删掉状态文件（正常退出时调用）。
func (p *Publisher) Remove() {
	if p == nil {
		return
	}
	_ = os.Remove(p.path)
}

// nowPlaying 把当前模型状态整理成对外发布的结构。
//
// 队列空 / 还没选歌时发布一个「没在播」的空信封：桌面顶栏靠 track.title 为空
// 加上 playing=false 把那条胶囊收掉。以前这里直接索引 m.songs[m.idx]，
// 接了真实歌库之后空队列是常态，那样会 panic。
func (m Model) nowPlaying() NowPlaying {
	np := NowPlaying{
		PID:       os.Getpid(),
		UpdatedAt: float64(time.Now().UnixNano()) / 1e9,
		Playing:   true,
		Paused:    m.paused,
		Position:  m.pos,
		Length:    m.dur,
	}
	if cur, ok := m.current(); ok {
		np.Track = PubTrack{Title: cur.Title, Artist: cur.Artist, Album: cur.Album}
		np.Lyrics = cur.Lyrics
	} else {
		np.Playing = false
		np.Paused = true
	}
	// mpv 没了就别再声称在播：桌面侧据此把顶栏那条收掉。
	if m.mpv != nil && m.mpv.Dead() {
		np.Playing = false
		np.Paused = true
	}
	return np
}

// publish 把当前状态推给桌面 shell：状态文件给顶栏歌词，MPRIS 给灵动岛 / 锁屏 / 媒体键。
// pub / mpris 为 nil（测试里的字面量 Model）时各自是空操作。
// 顺便按节流把播放状态存进 SQLite（见 Store.Save）。
func (m Model) publish() {
	m.pub.Publish(m.nowPlaying())
	m.mpris.Sync(m.mprisState())
	m.persist(false)
}
