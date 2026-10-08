package main

import (
	"bufio"
	"os/exec"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/godbus/dbus/v5"
)

// privateBus 起一个只属于本测试的 dbus-daemon，免得测试实例出现在用户真实的灵动岛里。
func privateBus(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("dbus-daemon"); err != nil {
		t.Skip("没有 dbus-daemon")
	}
	cmd := exec.Command("dbus-daemon", "--session", "--nofork", "--print-address=1")
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	addr, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", strings.TrimSpace(addr))
}

// TestMprisRoundTrip：外面读得到正确的播放状态，外面发的控制能以消息形式回到 Update。
func TestMprisRoundTrip(t *testing.T) {
	privateBus(t)

	srv, err := StartMpris()
	if err != nil {
		t.Fatal("StartMpris:", err)
	}
	defer srv.Close()
	got := make(chan tea.Msg, 8)
	srv.Attach(func(m tea.Msg) { got <- m })

	m := Model{
		queue:    []Track{{Title: "晴天", Artist: "周杰伦", Album: "叶惠美", CoverURL: "https://example.com/c.jpg"}, {Title: "下一首"}},
		curIndex: 0,
		dur:      269,
		volume:   70,
		playReq:  3,
	}
	m.pos, m.posWall, m.posAt = 42, 42, time.Now()
	m.paused = true
	srv.Sync(m.mprisState())

	client, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	obj := client.Object(mprisBusName, mprisPath)

	get := func(name string) dbus.Variant {
		t.Helper()
		v, err := obj.GetProperty(mprisPlayerIfce + "." + name)
		if err != nil {
			t.Fatalf("读 %s: %v", name, err)
		}
		return v
	}
	if s := get("PlaybackStatus").Value(); s != "Paused" {
		t.Errorf("PlaybackStatus = %v，应为 Paused", s)
	}
	md := get("Metadata").Value().(map[string]dbus.Variant)
	if md["xesam:title"].Value() != "晴天" || md["mpris:length"].Value() != int64(269e6) {
		t.Errorf("Metadata 不对: %v", md)
	}
	if a := md["xesam:artist"].Value().([]string); len(a) != 1 || a[0] != "周杰伦" {
		t.Errorf("artist 不对: %v", a)
	}
	if p := get("Position").Value().(int64); p != 42e6 {
		t.Errorf("Position = %d，应为 42e6", p)
	}
	if id, _ := obj.GetProperty(mprisRootIface + ".Identity"); id.Value() != mprisIdentity {
		t.Errorf("Identity = %v", id.Value())
	}

	// 状态变化要反映出来
	m.paused = false
	srv.Sync(m.mprisState())
	if s := get("PlaybackStatus").Value(); s != "Playing" {
		t.Errorf("恢复播放后 PlaybackStatus = %v", s)
	}

	// 控制方法：变成 mprisMsg 送回来，而不是在 D-Bus goroutine 里直接动播放器
	call := func(method string, args ...any) mprisMsg {
		t.Helper()
		if c := obj.Call(mprisPlayerIfce+"."+method, 0, args...); c.Err != nil {
			t.Fatalf("调 %s: %v", method, c.Err)
		}
		select {
		case msg := <-got:
			return msg.(mprisMsg)
		case <-time.After(2 * time.Second):
			t.Fatalf("%s 没有回到 Update", method)
		}
		return mprisMsg{}
	}
	if msg := call("PlayPause"); msg.op != "playpause" {
		t.Errorf("PlayPause → %+v", msg)
	}
	if msg := call("Next"); msg.op != "next" {
		t.Errorf("Next → %+v", msg)
	}
	if msg := call("Seek", int64(-5e6)); msg.op != "seek" || msg.arg != -5 {
		t.Errorf("Seek → %+v", msg)
	}
	trackID := m.mprisState().TrackID
	if msg := call("SetPosition", trackID, int64(100e6)); msg.op != "setpos" || msg.arg != 100 || msg.trackID != trackID {
		t.Errorf("SetPosition → %+v", msg)
	}

	// 写 Volume 属性也走消息
	if err := obj.SetProperty(mprisPlayerIfce+".Volume", dbus.MakeVariant(0.3)); err != nil {
		t.Fatal(err)
	}
	select {
	case msg := <-got:
		if mm := msg.(mprisMsg); mm.op != "volume" || mm.arg != 0.3 {
			t.Errorf("Volume → %+v", mm)
		}
	case <-time.After(2 * time.Second):
		t.Error("写 Volume 没有回到 Update")
	}

	// 空队列 → Stopped + NoTrack
	srv.Sync(Model{curIndex: -1}.mprisState())
	if s := get("PlaybackStatus").Value(); s != "Stopped" {
		t.Errorf("空队列 PlaybackStatus = %v", s)
	}
}

// TestOnMpris：消息落到 Update 之后的语义。
func TestOnMpris(t *testing.T) {
	m := Model{queue: []Track{{Title: "a"}}, curIndex: 0, volume: 70}

	next, _ := m.onMpris(mprisMsg{op: "volume", arg: 0.25})
	if next.volume != 25 {
		t.Errorf("volume 0.25 → %d", next.volume)
	}
	// SetPosition 带的曲目 id 已经过期 → 忽略
	m.playReq = 2
	stale, _ := m.onMpris(mprisMsg{op: "setpos", arg: 10, trackID: "/org/qqmusic_tui/track/1"})
	if stale.pos != 0 {
		t.Errorf("过期 trackid 的 SetPosition 应被忽略，pos=%v", stale.pos)
	}
	// 没有曲目时控制请求都是空操作，不 panic
	empty := Model{curIndex: -1}
	for _, op := range []string{"playpause", "play", "pause", "next", "prev", "seek", "setpos"} {
		empty.onMpris(mprisMsg{op: op, arg: 1})
	}
}
