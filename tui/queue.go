package main

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

// 队列操作。
//
// 这里的方法**只被 Update goroutine 调用**，而它们会直接调 mpv（Load/Stop/SetPause）。
// 这是刻意的：bubbletea 的 Cmd 跑在并发 goroutine 池里，连按两次回车时两个 goroutine
// 的 loadfile 写入顺序不确定，出声的会是随机那首（writeMu 只保证字节不交错，
// 不保证跨 goroutine 的顺序）。mpv 调用带 2 秒超时、本地 socket 通常亚毫秒，
// 同步发完全可以接受。
//
// 网络请求（resolve / lyrics）则必须异步，返回值通过 tea.Msg 回到这里再落地。

// startCurrent 播放 m.curIndex 指向的那首。
//
// 测试通过 DirectURL 注入的本地音频直接丢给 mpv；真实曲目要先去 resolve 拿直链，
// 那是异步的，所以这里只发命令。
func (m *Model) startCurrent() tea.Cmd {
	cur, ok := m.current()
	if !ok {
		return nil
	}

	// 这一代播放的号码。还在飞的旧 resolve/lyrics 结果会因为这个号码对不上而被丢弃。
	m.playReq++
	req := m.playReq

	m.anchorPos(0)
	m.resumeAt = 0
	m.restorePending = false
	m.resumeRestore = false
	m.playQuality = ""
	m.dur = float64(cur.Duration)
	m.paused = false
	m.lyricFollow = true
	m.lyricScroll = 0

	if cur.DirectURL != "" {
		if m.mpv == nil {
			m.status = "没有可用的播放器（mpv 未启动）"
			return nil
		}
		if err := m.mpv.Load(cur.DirectURL); err != nil {
			m.status = "载入失败: " + err.Error()
			return nil
		}
		_ = m.mpv.SetPause(false)
		m.status = "正在播放: " + cur.Title
		return nil
	}

	if cur.SongMid == "" {
		m.status = "这首歌缺少 songMid，无法解析播放地址"
		return nil
	}
	if m.api == nil {
		m.status = errNoClient.Error()
		return nil
	}
	m.status = "正在解析播放地址: " + cur.Title
	return tea.Batch(m.cmdResolve(req, cur), m.cmdLyrics(req, cur))
}

// playFromList 把 tracks 整段换成播放队列，并从第 index 首开始播。
//
// 语义对齐桌面端的 playTrack(track, sourceQueue, index)：从列表里选中一首歌是
// 「换队列」而不是「往现有队列里插一首」。不可播的曲目在这一步就滤掉，
// 免得播到一半卡住。
func (m *Model) playFromList(tracks []Track, index int) tea.Cmd {
	if index < 0 || index >= len(tracks) {
		return nil
	}
	q := make([]Track, 0, len(tracks))
	srcIdx := -1
	for i, t := range tracks {
		if !t.Playable && t.DirectURL == "" {
			continue
		}
		if i == index {
			srcIdx = len(q)
		}
		q = append(q, t)
	}
	if srcIdx < 0 {
		m.status = "「" + tracks[index].Title + "」当前不可播放（可能需要会员或登录）"
		return nil
	}

	m.queue = q
	m.curIndex = srcIdx
	// 队列换了，队列页的游标跟着重置。
	m.lists[PageQueue] = listState{}
	return m.startCurrent()
}

// playRowsFrom 从当前页面的行数组里抽曲目组队列，并从第 index 行开始。
// 用行数组而不是各页自己的数据源：这样「下一首」的范围就是用户眼前看到的这一屏，
// 跨分节（每日推荐 → 猜你喜欢）也能连着放。
func (m *Model) playRowsFrom(rows []row, index int) tea.Cmd {
	tracks := make([]Track, 0, len(rows))
	srcIdx := -1
	for i, r := range rows {
		if r.kind != rowTrack {
			continue
		}
		if i == index {
			srcIdx = len(tracks)
		}
		tracks = append(tracks, r.track)
	}
	if srcIdx < 0 {
		return nil
	}
	return m.playFromList(tracks, srcIdx)
}

// playSelected 处理回车：曲目就换队列开播，歌单/专辑就进详情，歌手就搜他的歌。
func (m *Model) playSelected() tea.Cmd {
	r, ok := m.selectedRow()
	if !ok {
		return nil
	}
	switch r.kind {
	case rowTrack:
		return m.playRowsFrom(m.rows(), m.lists[m.page].cursor)
	case rowPlaylist:
		return m.cmdDetail(r.pl)
	case rowSinger:
		// 歌手歌曲列表那个接口（music.web_singer_info/GetSingerSongList）和写接口
		// 一样要 musics.fcg 签名，实测直接 500003/860100005。
		// 退而求其次按歌手名搜歌曲，并在状态栏说清楚这是「按名字搜的」。
		m.status = "按歌手名搜索：" + r.singer.Name
		return m.searchByName(r.singer.Name)
	}
	return nil
}

// enqueueSelected 把选中的歌追加到队尾（不打断当前播放）。
func (m *Model) enqueueSelected() tea.Cmd {
	r, ok := m.selectedRow()
	if !ok || r.kind != rowTrack {
		return nil
	}
	if !r.track.Playable && r.track.DirectURL == "" {
		m.status = "「" + r.track.Title + "」当前不可播放，没有加进队列"
		return nil
	}

	m.queue = append(m.queue, r.track)
	if m.curIndex >= 0 {
		m.status = fmt.Sprintf("已加入队列：%s（队列共 %d 首）", r.track.Title, len(m.queue))
		return nil
	}
	// 队列原本是空的：既然用户主动加了一首，顺手开始播。
	m.curIndex = len(m.queue) - 1
	return m.startCurrent()
}

// removeQueueAt 删掉队列页第 rowIdx 行对应的曲目。rowIdx 是行数组下标（含分节标题行）。
func (m *Model) removeQueueAt(rowIdx int) tea.Cmd {
	rows := m.rows()
	if rowIdx < 0 || rowIdx >= len(rows) || rows[rowIdx].kind != rowTrack {
		return nil
	}
	// 行里的曲目与队列一一对应（队列页只有「分节标题 + 曲目」两种行）。
	qIdx := -1
	n := 0
	for i, r := range rows {
		if r.kind != rowTrack {
			continue
		}
		if i == rowIdx {
			qIdx = n
			break
		}
		n++
	}
	if qIdx < 0 || qIdx >= len(m.queue) {
		return nil
	}

	title := m.queue[qIdx].Title
	m.queue = append(m.queue[:qIdx], m.queue[qIdx+1:]...)
	if m.page == PageQueue {
		m.lists[PageQueue].clamp(len(m.rows()), m.listH())
	}

	switch {
	case len(m.queue) == 0:
		m.curIndex = -1
		m.dur = 0
		m.anchorPos(0)
		m.paused = true
		if m.mpv != nil {
			_ = m.mpv.Stop()
		}
		m.playReq++ // 作废还在飞的那次 resolve
		m.status = "队列已空"
		return nil
	case qIdx < m.curIndex:
		// 删的是当前曲目之前的：索引要跟着前移，否则会播到别的歌。
		m.curIndex--
		m.status = "已移除：" + title
		return nil
	case qIdx == m.curIndex:
		// 删掉的正是当前这首：位置不动，播列表里的下一首（到尾部就退回最后一首）。
		if m.curIndex >= len(m.queue) {
			m.curIndex = len(m.queue) - 1
		}
		m.status = "已移除：" + title
		return m.startCurrent()
	default:
		m.status = "已移除：" + title
		return nil
	}
}

// clearQueue 清空队列并停止播放。
func (m *Model) clearQueue() tea.Cmd {
	if len(m.queue) == 0 {
		return nil
	}
	if m.mpv != nil {
		// 用 stop 而不是 load 一个空 URL：后者会报错，而且会多发一条 end-file。
		_ = m.mpv.Stop()
	}
	m.queue = nil
	m.curIndex = -1
	m.dur = 0
	m.anchorPos(0)
	m.paused = true
	m.playReq++
	m.lists[PageQueue] = listState{}
	m.status = "队列已清空"
	return nil
}
