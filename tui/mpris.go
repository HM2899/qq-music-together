package main

import (
	"fmt"
	"math"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"
)

// MPRIS：把 TUI 挂到会话总线上，桌面的灵动岛 / 锁屏媒体卡 / 全局媒体键 / playerctl
// 才看得见它、也才控制得了它。
//
// 为什么走 MPRIS 而不是把 TuiPlayerService 注册进桌面的 MediaManager：
// 那样桌面只能「看」，灵动岛上的暂停 / 切歌 / 拖进度按钮会点了没反应——
// 状态文件是单向的。MPRIS 是双向的，而且 Quickshell 的 Mpris 服务本来就会自动收录。
//
// 并发：D-Bus 方法在 godbus 自己的 goroutine 里被调用，这里**绝不直接碰 mpv 或 Model**，
// 只把请求包成 mprisMsg 经 Program.Send 送回 Update——与「mpv 调用一律留在 Update 里」的规则一致。

const (
	mprisPath       = dbus.ObjectPath("/org/mpris/MediaPlayer2")
	mprisRootIface  = "org.mpris.MediaPlayer2"
	mprisPlayerIfce = "org.mpris.MediaPlayer2.Player"
	mprisBusName    = "org.mpris.MediaPlayer2.qqmusic_tui"
	mprisIdentity   = "QQ音乐 TUI"
)

// mprisMsg 是外部（灵动岛、媒体键……）发来的一条控制请求。
type mprisMsg struct {
	op  string  // playpause / play / pause / stop / next / prev / seek / setpos / volume
	arg float64 // seek: 相对秒数；setpos: 绝对秒数；volume: 0~1
	// trackID 只有 setpos 用：规范要求目标曲目已经换掉时忽略这次请求。
	trackID dbus.ObjectPath
}

// MprisState 是每次同步给总线的快照，全部由 Update goroutine 算好再交进来。
type MprisState struct {
	HasTrack bool
	Paused   bool
	Track    Track
	TrackID  dbus.ObjectPath
	Position float64 // 秒
	Length   float64 // 秒
	Volume   int     // 0~100
	CanPrev  bool
	CanNext  bool
}

// MprisServer 持有总线连接。所有方法对 nil 接收者都是空操作——
// 测试里的 Model 没有它，总线不可用时 main 也拿到 nil，TUI 本身照常能用。
type MprisServer struct {
	conn  *dbus.Conn
	props *prop.Properties
	send  func(tea.Msg)

	// 以下只在 Update goroutine（Sync）里读写。
	last      MprisState
	lastSync  time.Time
	hasSynced bool
}

// StartMpris 连上会话总线并占一个 MPRIS 名字。失败返回 error，调用方降级为不发布。
func StartMpris() (*MprisServer, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, err
	}
	s := &MprisServer{conn: conn}

	if err := conn.Export(mprisRoot{}, mprisPath, mprisRootIface); err != nil {
		conn.Close()
		return nil, err
	}
	// SeekBy 映射成总线上的 Seek：Go 方法名直接叫 Seek 会被 go vet 当成签名写错的 io.Seeker。
	if err := conn.ExportWithMap(mprisPlayer{s}, map[string]string{"SeekBy": "Seek"}, mprisPath, mprisPlayerIfce); err != nil {
		conn.Close()
		return nil, err
	}

	props, err := prop.Export(conn, mprisPath, prop.Map{
		mprisRootIface: {
			"CanQuit":             {Value: false, Emit: prop.EmitFalse},
			"CanRaise":            {Value: false, Emit: prop.EmitFalse},
			"HasTrackList":        {Value: false, Emit: prop.EmitFalse},
			"Identity":            {Value: mprisIdentity, Emit: prop.EmitFalse},
			"SupportedUriSchemes": {Value: []string{}, Emit: prop.EmitFalse},
			"SupportedMimeTypes":  {Value: []string{}, Emit: prop.EmitFalse},
		},
		mprisPlayerIfce: {
			"PlaybackStatus": {Value: "Stopped", Emit: prop.EmitTrue},
			"Rate":           {Value: 1.0, Emit: prop.EmitTrue},
			"MinimumRate":    {Value: 1.0, Emit: prop.EmitTrue},
			"MaximumRate":    {Value: 1.0, Emit: prop.EmitTrue},
			"Metadata":       {Value: map[string]dbus.Variant{}, Emit: prop.EmitTrue},
			"Volume": {Value: 0.7, Writable: true, Emit: prop.EmitTrue,
				Callback: func(c *prop.Change) *dbus.Error {
					v, ok := c.Value.(float64)
					if !ok {
						return prop.ErrInvalidArg
					}
					s.dispatch(mprisMsg{op: "volume", arg: v})
					return nil
				}},
			// Position 按规范**不**发变更信号：客户端自己按 Rate 外推，跳变靠 Seeked 信号通知。
			"Position":      {Value: int64(0), Emit: prop.EmitFalse},
			"CanGoNext":     {Value: false, Emit: prop.EmitTrue},
			"CanGoPrevious": {Value: false, Emit: prop.EmitTrue},
			"CanPlay":       {Value: false, Emit: prop.EmitTrue},
			"CanPause":      {Value: false, Emit: prop.EmitTrue},
			"CanSeek":       {Value: false, Emit: prop.EmitTrue},
			"CanControl":    {Value: true, Emit: prop.EmitFalse},
		},
	})
	if err != nil {
		conn.Close()
		return nil, err
	}
	s.props = props

	node := &introspect.Node{
		Name: string(mprisPath),
		Interfaces: []introspect.Interface{
			introspect.IntrospectData,
			prop.IntrospectData,
			{Name: mprisRootIface, Methods: introspect.Methods(mprisRoot{}), Properties: props.Introspection(mprisRootIface)},
			{Name: mprisPlayerIfce, Methods: mprisPlayerMethods(), Properties: props.Introspection(mprisPlayerIfce),
				Signals: []introspect.Signal{{Name: "Seeked", Args: []introspect.Arg{{Name: "Position", Type: "x"}}}}},
		},
	}
	if err := conn.Export(introspect.NewIntrospectable(node), mprisPath, "org.freedesktop.DBus.Introspectable"); err != nil {
		conn.Close()
		return nil, err
	}

	// 名字最后再占：占到名字的那一刻客户端就会来 GetAll，对象必须已经导出好。
	// 同时开两个 TUI 时第二个退到带 PID 的实例名，规范允许这种写法。
	name := mprisBusName
	reply, err := conn.RequestName(name, dbus.NameFlagDoNotQueue)
	if err == nil && reply != dbus.RequestNameReplyPrimaryOwner {
		name = fmt.Sprintf("%s.instance%d", mprisBusName, os.Getpid())
		reply, err = conn.RequestName(name, dbus.NameFlagDoNotQueue)
	}
	if err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		conn.Close()
		if err == nil {
			err = fmt.Errorf("总线名 %s 被占用", name)
		}
		return nil, err
	}
	return s, nil
}

// Attach 把 Program.Send 接进来。必须在 Program 建好之后、Run 之前调用。
func (s *MprisServer) Attach(send func(tea.Msg)) {
	if s == nil {
		return
	}
	s.send = send
}

func (s *MprisServer) dispatch(msg mprisMsg) {
	if s == nil || s.send == nil {
		return
	}
	s.send(msg)
}

// Close 释放总线连接（退出时调用）。连接一断，名字自动释放，灵动岛随即收掉这一项。
func (s *MprisServer) Close() {
	if s == nil || s.conn == nil {
		return
	}
	s.conn.Close()
}

// Sync 把最新快照同步到总线。只改真正变了的属性，免得每 200ms 刷一遍 PropertiesChanged。
func (s *MprisServer) Sync(st MprisState) {
	if s == nil || s.props == nil {
		return
	}
	prev, first := s.last, !s.hasSynced
	now := time.Now()

	status := "Stopped"
	if st.HasTrack {
		status = "Playing"
		if st.Paused {
			status = "Paused"
		}
	}
	prevStatus := "Stopped"
	if prev.HasTrack {
		prevStatus = "Playing"
		if prev.Paused {
			prevStatus = "Paused"
		}
	}

	// Position 先于 PlaybackStatus 写：客户端收到状态变更时会回头读一次 Position。
	s.props.SetMust(mprisPlayerIfce, "Position", int64(st.Position*1e6))

	if first || st.TrackID != prev.TrackID || st.Length != prev.Length ||
		st.Track.Title != prev.Track.Title || st.Track.CoverURL != prev.Track.CoverURL {
		s.props.SetMust(mprisPlayerIfce, "Metadata", mprisMetadata(st))
	}
	if first || status != prevStatus {
		s.props.SetMust(mprisPlayerIfce, "PlaybackStatus", status)
	}
	if first || st.HasTrack != prev.HasTrack {
		s.props.SetMust(mprisPlayerIfce, "CanPlay", st.HasTrack)
		s.props.SetMust(mprisPlayerIfce, "CanPause", st.HasTrack)
		s.props.SetMust(mprisPlayerIfce, "CanSeek", st.HasTrack)
	}
	if first || st.CanNext != prev.CanNext {
		s.props.SetMust(mprisPlayerIfce, "CanGoNext", st.CanNext)
	}
	if first || st.CanPrev != prev.CanPrev {
		s.props.SetMust(mprisPlayerIfce, "CanGoPrevious", st.CanPrev)
	}
	if first || st.Volume != prev.Volume {
		s.props.SetMust(mprisPlayerIfce, "Volume", float64(st.Volume)/100)
	}

	// 位置跳变（seek、换歌）要发 Seeked，否则客户端会按旧位置一路外推下去。
	// 按「上次位置 + 期间流逝时间」推一个期望值，偏差超过 1 秒就算跳变——
	// 这样不用在每个 seek 入口各插一句，换歌、灵动岛拖进度、键盘快退都覆盖到。
	if !first && st.HasTrack {
		expect := prev.Position
		if prev.HasTrack && !prev.Paused {
			expect += now.Sub(s.lastSync).Seconds()
		}
		if st.TrackID != prev.TrackID || math.Abs(st.Position-expect) > 1 {
			_ = s.conn.Emit(mprisPath, mprisPlayerIfce+".Seeked", int64(st.Position*1e6))
		}
	}

	s.last, s.lastSync, s.hasSynced = st, now, true
}

func mprisMetadata(st MprisState) map[string]dbus.Variant {
	md := map[string]dbus.Variant{}
	if !st.HasTrack {
		md["mpris:trackid"] = dbus.MakeVariant(dbus.ObjectPath("/org/mpris/MediaPlayer2/TrackList/NoTrack"))
		return md
	}
	t := st.Track
	artists := t.Artists
	if len(artists) == 0 && t.Artist != "" {
		artists = []string{t.Artist}
	}
	md["mpris:trackid"] = dbus.MakeVariant(st.TrackID)
	md["xesam:title"] = dbus.MakeVariant(t.Title)
	md["xesam:artist"] = dbus.MakeVariant(artists)
	md["xesam:album"] = dbus.MakeVariant(t.Album)
	if st.Length > 0 {
		md["mpris:length"] = dbus.MakeVariant(int64(st.Length * 1e6))
	}
	if t.CoverURL != "" {
		md["mpris:artUrl"] = dbus.MakeVariant(t.CoverURL)
	}
	return md
}

// mprisState 从 Model 整理出要同步的快照。
func (m Model) mprisState() MprisState {
	st := MprisState{Volume: m.volume}
	if m.muted {
		st.Volume = 0
	}
	cur, ok := m.current()
	if !ok || (m.mpv != nil && m.mpv.Dead()) {
		return st
	}
	st.HasTrack = true
	st.Paused = m.paused
	st.Track = cur
	// playReq 每次 startCurrent 自增，正好当「这一次播放」的唯一标识；
	// 同一首歌重播也算新曲目，客户端会重置进度。
	st.TrackID = dbus.ObjectPath(fmt.Sprintf("/org/qqmusic_tui/track/%d", m.playReq))
	st.Position = m.displayPos()
	st.Length = m.dur
	st.CanNext = len(m.queue) > 1
	st.CanPrev = len(m.queue) > 1
	return st
}

// onMpris 在 Update goroutine 里执行外部发来的控制请求。
func (m Model) onMpris(msg mprisMsg) (Model, tea.Cmd) {
	if _, ok := m.current(); !ok && msg.op != "volume" {
		return m, nil
	}
	var cmd tea.Cmd
	switch msg.op {
	case "playpause", "play":
		if m.restorePending {
			cmd = m.resumeRestored()
		} else if msg.op == "playpause" || m.paused {
			m.togglePlay()
		}
	case "pause", "stop":
		if !m.paused {
			m.togglePlay()
		}
	case "next":
		cmd = m.nextSong()
	case "prev":
		cmd = m.prevSong()
	case "seek":
		m.seekBy(msg.arg)
	case "setpos":
		if msg.trackID != m.mprisState().TrackID {
			return m, nil // 规范：目标曲目已经不是当前这首了，忽略
		}
		if msg.arg < 0 || (m.dur > 0 && msg.arg > m.dur) {
			return m, nil
		}
		m.seekBy(msg.arg - m.displayPos())
	case "volume":
		m.setVolume(int(math.Round(msg.arg * 100)))
	}
	return m, cmd
}

// ---------- 导出到总线的方法 ----------

type mprisRoot struct{}

func (mprisRoot) Raise() *dbus.Error { return nil }
func (mprisRoot) Quit() *dbus.Error  { return nil }

type mprisPlayer struct{ s *MprisServer }

func (p mprisPlayer) Next() *dbus.Error      { p.s.dispatch(mprisMsg{op: "next"}); return nil }
func (p mprisPlayer) Previous() *dbus.Error  { p.s.dispatch(mprisMsg{op: "prev"}); return nil }
func (p mprisPlayer) Pause() *dbus.Error     { p.s.dispatch(mprisMsg{op: "pause"}); return nil }
func (p mprisPlayer) PlayPause() *dbus.Error { p.s.dispatch(mprisMsg{op: "playpause"}); return nil }
func (p mprisPlayer) Stop() *dbus.Error      { p.s.dispatch(mprisMsg{op: "stop"}); return nil }
func (p mprisPlayer) Play() *dbus.Error      { p.s.dispatch(mprisMsg{op: "play"}); return nil }

// SeekBy 即总线上的 Seek，参数是相对偏移，单位微秒。
func (p mprisPlayer) SeekBy(offset int64) *dbus.Error {
	p.s.dispatch(mprisMsg{op: "seek", arg: float64(offset) / 1e6})
	return nil
}

// SetPosition 的参数是绝对位置，单位微秒。
func (p mprisPlayer) SetPosition(track dbus.ObjectPath, pos int64) *dbus.Error {
	p.s.dispatch(mprisMsg{op: "setpos", arg: float64(pos) / 1e6, trackID: track})
	return nil
}

func (p mprisPlayer) OpenUri(string) *dbus.Error { return nil }

// mprisPlayerMethods 是给自省用的方法表；SeekBy 改名回 Seek，与 ExportWithMap 的映射一致。
func mprisPlayerMethods() []introspect.Method {
	methods := introspect.Methods(mprisPlayer{})
	for i := range methods {
		if methods[i].Name == "SeekBy" {
			methods[i].Name = "Seek"
		}
	}
	return methods
}
