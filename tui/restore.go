package main

import (
	tea "github.com/charmbracelet/bubbletea"
)

// ---------- 恢复上次的播放状态 ----------
//
// 启动时把上次的队列和位置原样摆回来，但**暂停**着：不该一开终端就突然出声。
// 这时 mpv 里什么都没载入（播放地址有时效，存了也没用），所以 restorePending 期间：
//   - 进度条显示存下来的位置，onTick 不去读空闲 mpv 的 0 秒把它冲掉；
//   - 按空格 / 灵动岛播放 / 媒体键 → 现取播放地址，从存下的位置接着放；
//   - 切歌、在列表里回车 → 正常开新的一首，恢复状态作废。

// WithStore 挂上数据库，并按库里的内容恢复上次的状态。
func (m Model) WithStore(s *Store) Model {
	m.store = s
	if s == nil {
		return m
	}
	// 音质：库里有就用库里的；没有就沿用旧版本 config.json 里的（一次性迁移，下次存盘就进库了）。
	if q := s.LoadQuality(); q != "" {
		if _, ok := qualityLabels[q]; ok && q != "default" {
			m.quality = q
		}
	}
	st, ok, err := s.Load()
	if err != nil {
		m.status = "读取上次的播放状态失败：" + err.Error()
		return m
	}
	if ok {
		m.applyRestore(st)
	}
	return m
}

func (m *Model) applyRestore(st savedState) {
	if st.Volume >= 0 && st.Volume <= 100 {
		m.volume = st.Volume
	}
	m.muted = st.Muted
	if m.mpv != nil {
		v := m.volume
		if m.muted {
			v = 0
		}
		_ = m.mpv.SetVolume(v)
	}
	if len(st.Queue) == 0 {
		return
	}
	m.queue = st.Queue
	m.curIndex = st.CurIndex
	cur, ok := m.current()
	if !ok {
		return
	}
	m.dur = float64(cur.Duration)
	pos := st.Position
	if pos < 0 || (m.dur > 0 && pos > m.dur) {
		pos = 0
	}
	m.anchorPos(pos)
	m.paused = true
	m.restorePending = true
	m.lists[PageQueue].cursor = m.curIndex + 1 // 队列页第 0 行是分节标题
	m.status = "已恢复上次的播放：" + cur.Title + "（" + formatTime(pos) + "），按空格继续"
}

// snapshot 是要存盘的那份状态。
func (m Model) snapshot() savedState {
	return savedState{
		Queue:    m.queue,
		CurIndex: m.curIndex,
		Position: m.displayPos(),
		Volume:   m.volume,
		Muted:    m.muted,
		Quality:  m.quality,
	}
}

// persist 存盘。force=false 时由 Store 节流（只有位置在变就每 5 秒一次）。
func (m Model) persist(force bool) {
	_ = m.store.Save(m.snapshot(), force)
}

// resumeRestored 从恢复出来的位置真正开始放。
func (m *Model) resumeRestored() tea.Cmd {
	cur, ok := m.current()
	m.restorePending = false
	if !ok {
		return nil
	}
	if m.pos < 0.5 || cur.DirectURL != "" {
		return m.startCurrent()
	}
	if m.api == nil {
		m.status = errNoClient.Error()
		return nil
	}
	m.playReq++
	req := m.playReq
	m.resumeAt = m.pos
	m.resumeRestore = true
	m.paused = false
	m.status = "正在恢复播放：" + cur.Title + "…"
	cmds := []tea.Cmd{m.cmdResolve(req, cur)}
	if len(cur.Lyrics) == 0 {
		cmds = append(cmds, m.cmdLyrics(req, cur))
	}
	return tea.Batch(cmds...)
}
