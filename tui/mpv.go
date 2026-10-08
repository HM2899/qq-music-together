package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// ipcResp 是 mpv IPC 的一次响应，也可能是异步事件。
type ipcResp struct {
	RequestID int             `json:"request_id"`
	Error     string          `json:"error"`
	Data      json.RawMessage `json:"data"`
	Event     string          `json:"event"`
	Reason    string          `json:"reason"`
	// FileError 只在 end-file 且 reason=="error" 时出现（直链过期、格式不支持……）。
	FileError string `json:"file_error"`
}

// MpvEnd 是「一个文件结束了」的通报。
type MpvEnd struct {
	// Gen 是事件发生那一刻的 Load 代际。上层拿它和当前代际比对，
	// 就能丢掉「已经被手动切歌取代」的那次结束——否则自然播完的信号
	// 会滞留在通道里，等用户手动切歌之后才被处理，于是多跳一首。
	Gen uint64
	// Err 非空表示是失败结束（比如直链过期），空字符串表示正常播完。
	Err string
}

// Mpv 通过 --input-ipc-server 的 unix socket 控制一个后台 mpv 进程。
// 所有方法都是并发安全的；命令带超时，避免 mpv 无响应时卡住 TUI。
type Mpv struct {
	cmd     *exec.Cmd
	conn    net.Conn
	sock    string
	writeMu sync.Mutex

	mu      sync.Mutex
	reqID   int
	pending map[int]chan ipcResp

	// waitOnce 保证子进程只被 Wait 一次，避免残留僵尸进程。
	waitOnce sync.Once
	dead     atomic.Bool

	closed chan struct{}
	// Ended 在「当前曲目结束」时收到一条通报（手动切歌/停止不触发）。
	// 缓冲 4 条：正常最多积压一条，留点余量免得 readLoop 被阻塞。
	Ended chan MpvEnd

	// loadSeq 每次 Load 自增，用来给结束事件打代际标签。
	// 只在 Load 返回**之后**自增：这样在 loadfile 调用期间收到的那条
	// end-file（属于被替换掉的旧文件）拿到的仍是旧代际，会被上层正确丢弃。
	loadSeq atomic.Uint64
}

const mpvCmdTimeout = 2 * time.Second

// StartMpv 启动后台 mpv 并连上它的 IPC socket。
// extra 是追加的 mpv 命令行参数（测试用 --ao=null 跑无头播放）。
func StartMpv(volume int, extra ...string) (*Mpv, error) {
	if _, err := exec.LookPath("mpv"); err != nil {
		return nil, errors.New("没找到 mpv，请先安装（pacman -S mpv）")
	}

	dir, err := os.MkdirTemp("", "qqmusic-tui-")
	if err != nil {
		return nil, err
	}
	sock := filepath.Join(dir, "mpv.sock")

	args := []string{
		"--input-ipc-server=" + sock,
		"--no-video",
		"--idle=yes",
		"--no-terminal",
		"--really-quiet",
		"--cache=yes",
		"--gapless-audio=weak",
		fmt.Sprintf("--volume=%d", volume),
	}
	args = append(args, extra...)

	cmd := exec.Command("mpv", args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		os.RemoveAll(dir)
		return nil, err
	}

	// 等待 socket 出现。
	var conn net.Conn
	deadline := time.Now().Add(5 * time.Second)
	for {
		conn, err = net.Dial("unix", sock)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			os.RemoveAll(dir)
			return nil, fmt.Errorf("连接 mpv IPC 超时: %w", err)
		}
		time.Sleep(50 * time.Millisecond)
	}

	m := &Mpv{
		cmd:     cmd,
		conn:    conn,
		sock:    sock,
		pending: make(map[int]chan ipcResp),
		closed:  make(chan struct{}),
		Ended:   make(chan MpvEnd, 4),
	}
	go m.readLoop()
	return m, nil
}

// readLoop 持续读 socket，把带 request_id 的响应分发给等待者，事件消息直接丢弃。
func (m *Mpv) readLoop() {
	scanner := bufio.NewScanner(m.conn)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		var resp ipcResp
		if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
			continue
		}
		if resp.RequestID == 0 {
			// 异步事件：只在「当前曲目结束」时通知上层。
			//
			// 只看 eof 和 error：
			//   eof      = 自然播完，该自动切下一首
			//   error    = 播不下去了（直链过期等）。以前这里被完全忽略，
			//              结果是 mpv 停住而界面永远显示「正在播放」。
			//   stop     = 我们主动 loadfile replace 停掉的旧文件
			//   quit     = mpv 退出
			//   redirect = 播放列表跳转
			// 后三种都不该触发自动切歌。
			if resp.Event == "end-file" {
				switch resp.Reason {
				case "eof":
					m.signalEnd(MpvEnd{Gen: m.loadSeq.Load()})
				case "error":
					m.signalEnd(MpvEnd{Gen: m.loadSeq.Load(), Err: resp.FileError})
				}
			}
			continue
		}
		m.mu.Lock()
		ch, ok := m.pending[resp.RequestID]
		delete(m.pending, resp.RequestID)
		m.mu.Unlock()
		if ok {
			ch <- resp
		}
	}
	// socket 断开 = mpv 进程没了（或者我们的连接被关掉了）。
	// 先让上层立刻感知断开，再收尸。
	m.dead.Store(true)
	close(m.closed)
	m.waitOnce.Do(func() { _ = m.cmd.Wait() })
}

// signalEnd 非阻塞地把结束通报投给上层。通道满了就丢掉最新的那条——
// 说明上层已经积压了没处理完的结束事件，再塞只会让 readLoop 卡住。
func (m *Mpv) signalEnd(end MpvEnd) {
	select {
	case m.Ended <- end:
	default:
	}
}

// Dead 报告 mpv 进程是否已经退出（无论是被外部杀掉还是自己崩了）。
func (m *Mpv) Dead() bool { return m.dead.Load() }

// LoadSeq 是当前代际，测试和上层判断「这条结束通报还算不算数」时用。
func (m *Mpv) LoadSeq() uint64 { return m.loadSeq.Load() }

// call 发一条 IPC 命令并等响应。
func (m *Mpv) call(args ...any) (json.RawMessage, error) {
	m.mu.Lock()
	m.reqID++
	id := m.reqID
	ch := make(chan ipcResp, 1)
	m.pending[id] = ch
	m.mu.Unlock()

	payload, err := json.Marshal(map[string]any{
		"command":    args,
		"request_id": id,
	})
	if err != nil {
		return nil, err
	}

	m.writeMu.Lock()
	_, err = m.conn.Write(append(payload, '\n'))
	m.writeMu.Unlock()
	if err != nil {
		return nil, err
	}

	select {
	case resp := <-ch:
		if resp.Error != "" && resp.Error != "success" {
			return nil, fmt.Errorf("mpv: %s", resp.Error)
		}
		return resp.Data, nil
	case <-time.After(mpvCmdTimeout):
		m.mu.Lock()
		delete(m.pending, id)
		m.mu.Unlock()
		return nil, errors.New("mpv 响应超时")
	case <-m.closed:
		return nil, errors.New("mpv 已退出")
	}
}

// Load 载入并立即播放指定 URL（替换当前曲目）。
//
// 代际在调用**返回之后**才自增，见 loadSeq 的注释：这样调用期间收到的
// end-file（属于被替换掉的旧文件）拿到的还是旧代际，会被上层丢掉。
func (m *Mpv) Load(url string) error {
	_, err := m.call("loadfile", url, "replace")
	m.loadSeq.Add(1)
	return err
}

// LoadAt 载入并从 start 秒开始放（切音质时接着原位置）。
// mpv 0.38 起 loadfile 的第三个参数是插入位置 index，选项在第四个；replace 模式下 index 填 -1。
func (m *Mpv) LoadAt(url string, start float64) error {
	_, err := m.call("loadfile", url, "replace", -1, fmt.Sprintf("start=%.3f", start))
	m.loadSeq.Add(1)
	return err
}

// SetPause 设置暂停状态。
func (m *Mpv) SetPause(paused bool) error {
	_, err := m.call("set_property", "pause", paused)
	return err
}

// Stop 停止播放并回到空闲态（队列清空时用）。
// 注意 mpv 会为此发一条 end-file（reason=="stop"），上面按设计忽略了它，
// 所以不会触发「自动切下一首」。
func (m *Mpv) Stop() error {
	_, err := m.call("stop")
	return err
}

// SeekAbs 跳到绝对时间（秒）。
func (m *Mpv) SeekAbs(seconds float64) error {
	if seconds < 0 {
		seconds = 0
	}
	_, err := m.call("seek", seconds, "absolute")
	return err
}

// SetVolume 设置音量（0-100）。
func (m *Mpv) SetVolume(v int) error {
	if v < 0 {
		v = 0
	}
	if v > 100 {
		v = 100
	}
	_, err := m.call("set_property", "volume", v)
	return err
}

// numProp 读一个数值属性；属性不可用时（比如没载入文件）返回 0。
func (m *Mpv) numProp(name string) float64 {
	data, err := m.call("get_property", name)
	if err != nil || len(data) == 0 || string(data) == "null" {
		return 0
	}
	var f float64
	if err := json.Unmarshal(data, &f); err != nil {
		return 0
	}
	return f
}

// TimePos 当前播放位置（秒）。
func (m *Mpv) TimePos() float64 { return m.numProp("time-pos") }

// Duration 当前曲目总时长（秒）。
func (m *Mpv) Duration() float64 { return m.numProp("duration") }

// Paused 当前是否暂停。mpv 空闲（无文件）时按暂停处理。
func (m *Mpv) Paused() bool {
	data, err := m.call("get_property", "pause")
	if err != nil || len(data) == 0 || string(data) == "null" {
		return true
	}
	var b bool
	if err := json.Unmarshal(data, &b); err != nil {
		return true
	}
	return b
}

// Close 关掉 mpv 进程并清理 socket 目录。
func (m *Mpv) Close() {
	if m.conn != nil {
		_ = m.conn.Close()
	}
	// 无条件尝试 Kill：不能拿 dead 标志当判断依据——readLoop 可能刚置位 dead
	// （因为我们关了连接）但进程还活着，那样就会漏杀，而 readLoop 正阻塞在
	// Wait 上等一个永不退出的进程，两边互等成死锁。
	// 对已经退出的进程 Kill 只会返回 error，忽略即可。
	if m.cmd != nil && m.cmd.Process != nil {
		_ = m.cmd.Process.Kill()
	}
	// 必须 Wait，否则子进程会变成僵尸。用 Once 保证只收一次尸。
	m.waitOnce.Do(func() {
		if m.cmd != nil {
			_ = m.cmd.Wait()
		}
	})
	if m.sock != "" {
		_ = os.RemoveAll(filepath.Dir(m.sock))
	}
}
