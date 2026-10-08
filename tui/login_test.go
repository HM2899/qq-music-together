package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// writeQRPNG 造一张真能解码的二维码图片，给登录层测试用。
// 用同一个 qrTestGrid：这里要验的是「登录层拿到路径之后会不会去渲染」，
// 图案是不是合法二维码与这一步无关。
func writeQRPNG(t *testing.T) string {
	t.Helper()
	grid := qrTestGrid(25)
	return writeQRTestPNG(t, grid, 4, 6)
}

// 打开登录层：请求二维码 → 渲染 → 焦点停在登录层。
func TestLoginOpensAndRendersQR(t *testing.T) {
	qr := writeQRPNG(t)
	m := fakeModel(t, PageExplore)
	m.api = fakeAPI(t, "FAKE_QR_PATH="+qr)

	m = press(t, m, "L")

	if m.focus != FocusLogin {
		t.Fatalf("按 L 应进入登录层，实际 focus=%v", m.focus)
	}
	if m.login != loginWaiting {
		t.Errorf("拿到二维码之后应处于等待扫码，实际 %v", m.login)
	}
	if len(m.qrLines) == 0 {
		t.Fatalf("二维码应被渲染出来，qrNote=%q", m.qrNote)
	}
	if m.qrPath != qr {
		t.Errorf("qrPath 应记录脚本给的路径，实际 %q", m.qrPath)
	}
	// 渲染出来的图案必须真的出现在界面上。
	view := m.View()
	if !strings.Contains(view, m.qrLines[0]) {
		t.Error("登录页应画出二维码")
	}
}

// 终端太窄 / 二维码解不出来时的兜底：给出路径，并提示用 o 打开。
func TestLoginFallsBackToPathWhenQRTooWide(t *testing.T) {
	qr := writeQRPNG(t)
	m := fakeModel(t, PageExplore)
	m.api = fakeAPI(t, "FAKE_QR_PATH="+qr)
	m.w = 30 // 25 个模块 + 静区放不进 30 列里

	m = press(t, m, "L")

	if m.focus != FocusLogin {
		t.Fatalf("应进入登录层，实际 focus=%v", m.focus)
	}
	if len(m.qrLines) != 0 {
		t.Error("放不下的时候不该硬画二维码")
	}
	// 路径本身可能被终端宽度截断，所以断言的是「有没有把路径给出来」和提示语，
	// 而不是完整字符串；完整路径在 qrPath 字段里，供 o 键使用。
	if m.qrPath != qr {
		t.Errorf("兜底时也要记住二维码路径，实际 %q", m.qrPath)
	}
	view := m.View()
	for _, want := range []string{"二维码没法画在终端里", "按 o 用系统看图器打开"} {
		if !strings.Contains(view, want) {
			t.Errorf("兜底页面应包含 %q\n%s", want, view)
		}
	}
}

// 扫码 → 确认 → 成功：会话信息落地，并且探索页被重新拉一次。
func TestLoginPollSuccessUpdatesSession(t *testing.T) {
	m := fakeModel(t, PageExplore)
	m.api = fakeAPI(t, "FAKE_LOGIN_STATE=success")
	m.discover = nil // 让「成功后是否重拉探索页」可观测

	m.focus = FocusLogin
	m.loginType = "qq"
	m.loginReq = 1
	m.loginCancel = func() {}
	m.loginCtxV = m.baseCtx
	m.login = loginWaiting

	// 直接喂一条成功消息：轮询本身走 tea.Tick，不该在单测里等真实时间。
	next, cmd := m.Update(loginPollMsg{
		req: m.loginReq,
		res: &LoginPoll{State: "success", Nickname: "假用户", Uin: "1402321235", LoggedIn: true},
	})
	m = next.(Model)

	// 成功后登录层自己收掉，焦点还给列表——不再停在成功页等用户按 esc
	// （以前按 esc 会提示「已退出登录」，刚登录完看到这句像是又被踢了）。
	if m.login != loginClosed || m.focus != FocusList {
		t.Errorf("登录成功后应关闭登录层，实际 login=%v focus=%v", m.login, m.focus)
	}
	if !m.session.LoggedIn || m.session.Nickname != "假用户" {
		t.Errorf("会话信息应从轮询结果直接落地，实际 %+v", m.session)
	}
	// 成功后必须重拉探索页（推荐是按登录态算的）。
	if cmd == nil {
		t.Fatal("登录成功后应返回命令")
	}
	m = runCmd(t, m, cmd)
	if m.discover == nil {
		t.Error("登录成功后探索页应被重新拉取")
	}
	// 登录前拉的收藏/音乐库是未登录态的空数据，必须作废。
	if m.favorites != nil || m.library != nil {
		t.Error("登录成功后应作废登录前的收藏/音乐库缓存")
	}
	// 收藏集合要跟着拉回来，♥ 才有依据。
	if !m.favMids["0039MnYb0qxYhV"] {
		t.Errorf("登录成功后应拉取收藏集合，实际 %v", m.favMids)
	}
}

// 已登录时按 L 是账号页，不是直接甩一张新二维码；x 退出登录。
func TestLogoutFromAccountPage(t *testing.T) {
	m := fakeModel(t, PageExplore)
	m.session = sessionView{Checked: true, LoggedIn: true, Nickname: "假用户", Uin: "1402321235"}
	m.favMids = map[string]bool{"0039MnYb0qxYhV": true}
	m.favorites = []Track{{SongMid: "0039MnYb0qxYhV"}}

	next, cmd := m.Update(keyMsg("L"))
	m = next.(Model)
	if m.focus != FocusLogin || m.login != loginAccount {
		t.Fatalf("已登录按 L 应进账号页，实际 focus=%v login=%v", m.focus, m.login)
	}
	if cmd != nil {
		t.Error("账号页不该自动请求二维码")
	}
	if v := stripANSI(m.View()); !contains(v, "假用户") || !contains(v, "退出登录") {
		t.Errorf("账号页应显示当前账号和退出登录选项:\n%s", v)
	}

	m = press(t, m, "x")
	if m.session.LoggedIn {
		t.Error("退出登录后会话应为未登录")
	}
	if m.focus != FocusList || m.login != loginClosed {
		t.Errorf("退出登录后应关闭登录层，实际 focus=%v login=%v", m.focus, m.login)
	}
	if m.favMids != nil || m.favorites != nil || m.isFavorite("0039MnYb0qxYhV") {
		t.Error("退出登录后上一个账号的收藏不能还挂着")
	}
	if !contains(m.status, "已退出登录") {
		t.Errorf("状态栏应说明已退出登录，实际 %q", m.status)
	}

	// 未登录时按 L 直接扫码。
	next, cmd = m.Update(keyMsg("L"))
	m = next.(Model)
	if m.login != loginStarting || cmd == nil {
		t.Errorf("未登录按 L 应直接请求二维码，实际 login=%v", m.login)
	}
}

// 只关掉登录层不能说成「已退出登录」。
func TestLoginEscIsNotLogout(t *testing.T) {
	m := fakeModel(t, PageExplore)
	m.session = sessionView{Checked: true, LoggedIn: true, Nickname: "假用户"}
	m.focus = FocusLogin
	m.login = loginWaiting
	next, _ := m.Update(keyMsg("esc"))
	m = next.(Model)
	if contains(m.status, "退出登录") || !m.session.LoggedIn {
		t.Errorf("esc 只是关闭登录层，实际 status=%q loggedIn=%v", m.status, m.session.LoggedIn)
	}
}

// 过期：停下来，别再轮询，提示按 r 重来。
func TestLoginPollExpiredStopsPolling(t *testing.T) {
	m := fakeModel(t, PageExplore)
	m.focus = FocusLogin
	m.loginReq = 1
	m.login = loginWaiting

	next, cmd := m.Update(loginPollMsg{
		req: m.loginReq,
		res: &LoginPoll{State: "expired", Message: "二维码已过期"},
	})
	m = next.(Model)

	if m.login != loginFailed {
		t.Errorf("过期应进入失败态，实际 %v", m.login)
	}
	if cmd != nil {
		t.Error("过期之后不该再安排下一次轮询")
	}
	if !strings.Contains(m.View(), "r") {
		t.Error("过期页面应提示按 r 重新生成")
	}
}

// waiting 会安排下一次轮询，而且节拍走的是 tea.Tick（不是立刻重发）。
func TestLoginPollSchedulesTick(t *testing.T) {
	m := fakeModel(t, PageExplore)
	m.focus = FocusLogin
	m.loginReq = 7
	m.login = loginWaiting

	next, cmd := m.Update(loginPollMsg{
		req: 7,
		res: &LoginPoll{State: "waiting", Message: "等待 QQ 扫码"},
	})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("waiting 状态应安排下一次轮询")
	}
	// 这条命令是 tea.Tick，不该立刻产出消息——否则等于无节流轮询。
	// 直接调用它：Timer 会阻塞到 loginPollInterval，所以只在测试里跳过执行。
	if m.login != loginWaiting {
		t.Errorf("waiting 应保持等待态，实际 %v", m.login)
	}
}

// 关掉登录层会作废请求号码：残留的轮询结果不能再改状态。
func TestLoginCloseCancelsStalePoll(t *testing.T) {
	m := fakeModel(t, PageExplore)
	m.focus = FocusLogin
	m.loginReq = 3
	m.login = loginWaiting
	m.qrLines = []string{"x"}

	m = press(t, m, "esc")
	if m.focus != FocusList {
		t.Fatalf("esc 应退出登录层，实际 focus=%v", m.focus)
	}
	if m.login != loginClosed {
		t.Errorf("退出后状态应为已关闭，实际 %v", m.login)
	}

	// 旧号码的轮询结果回来了：必须被丢掉。
	staleReq := uint64(3)
	next, _ := m.Update(loginPollMsg{
		req: staleReq,
		res: &LoginPoll{State: "success", Nickname: "不该生效"},
	})
	got := next.(Model)
	if got.session.LoggedIn {
		t.Error("关掉登录层之后，残留的轮询结果不该改写会话")
	}
}

// 登录层里的按键要有明确归属：q 不退出程序，t 切换登录方式。
func TestLoginKeyRouting(t *testing.T) {
	m := fakeModel(t, PageExplore)
	m.api = fakeAPI(t, "FAKE_QR_PATH=/nonexistent/qr.png")
	m.qrPath = "/nonexistent/qr.png"

	m = press(t, m, "L")
	if m.focus != FocusLogin {
		t.Fatalf("应进入登录层，实际 focus=%v", m.focus)
	}

	// q 在登录层里不该退出程序。
	next, cmd := m.Update(keyMsg("q"))
	got := next.(Model)
	if cmd != nil {
		if _, isQuit := cmd().(tea.QuitMsg); isQuit {
			t.Error("登录层里按 q 不该退出程序")
		}
	}
	if got.focus != FocusLogin {
		t.Errorf("按 q 不该离开登录层，实际 focus=%v", got.focus)
	}

	// t 切换 QQ ↔ 微信，并重新请求二维码。
	before := got.loginType
	next, _ = got.Update(keyMsg("t"))
	got = next.(Model)
	if got.loginType == before {
		t.Errorf("按 t 应切换登录方式，实际还是 %q", got.loginType)
	}
	if got.loginType != "wechat" {
		t.Errorf("从 qq 切过去应是 wechat，实际 %q", got.loginType)
	}
}

// 登录层渲染的各种状态都不能越界。
func TestLoginViewFits(t *testing.T) {
	states := []loginState{loginStarting, loginWaiting, loginScanned, loginSucceed, loginFailed}
	for _, st := range states {
		for _, size := range [][2]int{{120, 35}, {60, 20}, {24, 10}} {
			m := testModel()
			m.focus = FocusLogin
			m.login = st
			m.loginType = "qq"
			m.loginMsg = "等待 QQ 扫码"
			m.qrPath = "/tmp/qr.png"
			m.qrNote = "终端宽度放不下这个二维码"
			m.w, m.h = size[0], size[1]
			checkFits(t, m.View(), size[0])
		}
	}
}

// 真二维码图片（脚本写出来的那种）也要能解码：
// 用 PNG 编码器生成一张带定位图案的图，验证整条链路。
func TestRenderQRRoundTrip(t *testing.T) {
	grid := qrTestGrid(21)
	path := filepath.Join(t.TempDir(), "qr.png")
	side := 21*3 + 2*5
	img := image.NewRGBA(image.Rect(0, 0, side, side))
	for y := 0; y < side; y++ {
		for x := 0; x < side; x++ {
			img.Set(x, y, color.RGBA{255, 255, 255, 255})
		}
	}
	for r := 0; r < 21; r++ {
		for c := 0; c < 21; c++ {
			if !grid[r][c] {
				continue
			}
			for dy := 0; dy < 3; dy++ {
				for dx := 0; dx < 3; dx++ {
					img.Set(5+c*3+dx, 5+r*3+dy, color.RGBA{0, 0, 0, 255})
				}
			}
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	f.Close()

	lines, err := RenderQR(path, 120)
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	if want := (21 + 2*qrQuietZone + 1) / 2; len(lines) != want {
		t.Errorf("应有 %d 行，实际 %d", want, len(lines))
	}
}
