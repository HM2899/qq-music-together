package main

import (
	"context"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// ---------- 登录层 ----------
//
// 登录走扫码：脚本把二维码 PNG 写到缓存目录，这里读出来画到终端里。
// 为什么不用浏览器/外部看图器：二维码有有效期（QQ 侧大约两分钟），
// 让用户切出去找文件、开看图器、再切回来，很容易就过期了。

// loginPollInterval 是两次 login-poll 之间的间隔。
//
// 脚本的 --wait 参数是**解析了却从不使用**的死参数（见 poll_qr_login），
// 每次调用都立即返回。所以轮询节拍必须由这里控制——立刻重发的话，
// 会以「子进程启动速度」的频率轰炸腾讯的登录接口。
const loginPollInterval = 2 * time.Second

type loginState int

const (
	loginClosed   loginState = iota // 没打开
	loginStarting                   // 正在请求二维码
	loginWaiting                    // 二维码已显示，等扫码
	loginScanned                    // 已扫码，等手机确认
	loginSucceed                    // 登录成功
	loginFailed                     // 失败 / 过期 / 出错
	loginAccount                    // 已登录时的账号页：换号 / 退出登录
)

// ---------- 消息 ----------

type loginQRMsg struct {
	req uint64
	res *QRLogin
	err error
}

type loginPollMsg struct {
	req uint64
	res *LoginPoll
	err error
}

// loginPollTickMsg 是「该发下一次轮询了」的闹钟。
// 带 req 是为了让取消登录之后残留的闹钟自己作废。
type loginPollTickMsg struct{ req uint64 }

// ---------- 命令生产者 ----------

// cmdLoginQR 请求一个新的二维码。
// 同时派生一个可取消的 ctx：关掉登录层时用它杀掉可能还在跑的 login-poll 子进程。
func (m *Model) cmdLoginQR() tea.Cmd {
	m.loginReq++
	req := m.loginReq
	m.login = loginStarting
	m.loginErr = nil
	m.loginMsg = ""
	m.qrLines = nil
	m.qrPath = ""
	m.qrNote = ""

	// 上一次登录层留下的 ctx 先取消掉，避免悬着的子进程。
	if m.loginCancel != nil {
		m.loginCancel()
		m.loginCancel = nil
	}

	c, base := m.api, m.baseCtx
	if c == nil {
		m.login = loginFailed
		m.loginErr = errNoClient
		return nil
	}
	ctx, cancel := context.WithCancel(base)
	m.loginCtxV = ctx
	m.loginCancel = cancel
	kind := m.loginType

	return func() tea.Msg {
		res, err := c.LoginQR(ctx, kind)
		return loginQRMsg{req: req, res: res, err: err}
	}
}

// cmdLoginPoll 查一次扫码状态。
func (m *Model) cmdLoginPoll() tea.Cmd {
	req := m.loginReq
	c, ctx := m.api, m.loginCtxV
	if c == nil || ctx == nil {
		return nil
	}
	return func() tea.Msg {
		res, err := c.LoginPoll(ctx)
		return loginPollMsg{req: req, res: res, err: err}
	}
}

// pollTick 安排下一次轮询。用 Timer 而不是在 poll 返回时立刻重发：
// 每条 login-poll 都是「起进程 → 一次 HTTPS → 退出」，立刻重发等于无节流轮询。
func pollTick(req uint64) tea.Cmd {
	return tea.Tick(loginPollInterval, func(time.Time) tea.Msg {
		return loginPollTickMsg{req: req}
	})
}

// ---------- 处理 ----------

func (m Model) onLoginQR(msg loginQRMsg) (tea.Model, tea.Cmd) {
	// 号码对不上 = 这次请求已经被取消或重来了，丢掉。
	if msg.req != m.loginReq || m.focus != FocusLogin {
		return m, nil
	}
	if msg.err != nil {
		m.login = loginFailed
		m.loginErr = msg.err
		return m, nil
	}
	m.login = loginWaiting
	m.qrPath = msg.res.QRPath
	if msg.res.Message != "" {
		m.loginMsg = msg.res.Message
	}

	// 渲染二维码。失败不致命：退化成「显示路径，按 o 打开」。
	maxCols := m.w - 6
	if maxCols < 21 {
		maxCols = 21
	}
	lines, err := RenderQR(msg.res.QRPath, maxCols)
	if err != nil {
		m.qrLines = nil
		m.qrNote = err.Error()
	} else {
		m.qrLines = lines // 缓存起来：登录成功/过期时脚本会删掉那个 PNG，不能每次重画都读盘
		m.qrNote = ""
	}
	// 立刻开轮询（用户可能扫得很快），之后就按 loginPollInterval 的节拍走。
	return m, m.cmdLoginPoll()
}

func (m Model) onLoginPoll(msg loginPollMsg) (tea.Model, tea.Cmd) {
	if msg.req != m.loginReq || m.focus != FocusLogin {
		return m, nil
	}
	if msg.err != nil {
		// 单次轮询失败不终止：网络抖一下不该把用户踢出登录层。
		// 连续失败由下面的状态判断兜底——真登录不上，用户按 r 重来。
		m.loginMsg = "查询扫码状态失败：" + msg.err.Error()
		return m, pollTick(msg.req)
	}

	res := msg.res
	switch res.State {
	case "success":
		m.login = loginSucceed
		m.loginMsg = "登录成功"
		// 直接用轮询返回里的身份，不再多发一次 session-status：
		// 脚本已经落盘了 session.json，这里再问一遍只是多一个子进程。
		m.session.Checked = true
		m.session.LoggedIn = true
		m.session.Nickname = res.Nickname
		m.session.Uin = res.Uin
		// 登录成功就把登录层收掉，焦点还给列表：以前停在「✓ 登录成功」上等用户按 esc，
		// 而 esc 的提示写的是「已退出登录」，刚登录完就看到这句，像是又被踢了。
		m.closeLogin()
		// 登录前（或上一个账号）拉到的「我喜欢」「音乐库」作废，下次进那一页时按新身份重拉。
		m.clearAccountData()
		m.status = "已登录：" + res.Nickname
		m.reflow()
		// 探索页推荐按登录态算，重新拉；收藏集合也要拉，♥ 和 f 才有依据。
		// 登录这一刻 session.json 刚写好、不会触发续期，两条并发不会互相写坏它。
		return m, tea.Batch(m.cmdDiscover(), m.cmdFavMids())

	case "expired":
		m.login = loginFailed
		m.loginErr = nil
		m.loginMsg = "二维码已过期，按 r 重新生成"
		m.qrLines = nil
		m.cancelLogin()
		return m, nil

	case "error":
		m.login = loginFailed
		m.loginErr = nil
		if res.Message != "" {
			m.loginMsg = res.Message
		} else {
			m.loginMsg = "登录失败，按 r 重试"
		}
		m.cancelLogin()
		return m, nil

	case "scanned":
		m.login = loginScanned
		if res.Message != "" {
			m.loginMsg = res.Message
		} else {
			m.loginMsg = "已扫码，请在手机上确认"
		}

	default: // waiting / 未知状态
		m.login = loginWaiting
		if res.Message != "" {
			m.loginMsg = res.Message
		}
	}
	return m, pollTick(msg.req)
}

// cancelLogin 取消在飞的轮询和子进程。
// 不只是把状态改掉——还在跑的那条 login-poll 必须真的被杀掉，
// 否则会有一个孤儿 python 活到它自己的超时。
func (m *Model) cancelLogin() {
	if m.loginCancel != nil {
		m.loginCancel()
		m.loginCancel = nil
	}
	m.loginCtxV = nil
	m.loginReq++ // 号码一变，残留的 poll / tick 结果全部作废
}

// closeLogin 关闭登录层并把焦点还给列表。
func (m *Model) closeLogin() {
	m.cancelLogin()
	m.login = loginClosed
	m.loginErr = nil
	m.loginMsg = ""
	m.qrLines = nil
	m.qrNote = ""
	m.focus = FocusList
}

// ---------- 按键 ----------

func (m Model) handleLoginKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.login == loginAccount {
		switch msg.String() {
		case "esc", "L", "q":
			m.closeLogin()
			return m, nil
		case "s", "r":
			// 换账号：扫码成功后旧会话被新会话覆盖。
			return m, m.cmdLoginQR()
		case "x":
			if m.loggingOut {
				return m, nil
			}
			m.loggingOut = true
			m.status = "正在退出登录…"
			return m, m.cmdLogout()
		}
		return m, nil
	}

	switch msg.String() {
	case "esc", "L":
		m.closeLogin()
		// 只是关掉登录层，账号状态没有任何变化——别写成「已退出登录」。
		if m.session.LoggedIn {
			m.status = "仍以 " + m.session.Nickname + " 登录"
		} else {
			m.status = "已取消登录"
		}
		return m, nil

	case "r":
		// 重新生成二维码（过期了、或者想换个账号）。
		return m, m.cmdLoginQR()

	case "t":
		// 在 QQ / 微信两种登录之间切换。
		if m.loginType == "qq" {
			m.loginType = "wechat"
		} else {
			m.loginType = "qq"
		}
		m.status = "登录方式：" + m.loginTypeLabel()
		return m, m.cmdLoginQR()

	case "o":
		// 二维码在终端里画不出来（太窄/解码失败）时的兜底：用系统看图器打开。
		if m.qrPath == "" {
			m.status = "还没有二维码文件"
			return m, nil
		}
		p := m.qrPath
		return m, func() tea.Msg {
			// 失败就算了：用户能看到路径，自己打开也一样。
			_ = exec.Command("xdg-open", p).Start()
			return nil
		}
	}
	return m, nil
}

// ---------- 渲染 ----------

func (m Model) loginTypeLabel() string {
	if m.loginType == "wechat" {
		return "微信"
	}
	return "QQ"
}

func (m Model) viewLoginPage(w, h int) string {
	line := func(k, d string) string {
		return styleKeyBadge.Render(" "+k+" ") + " " + styleText.Render(d)
	}

	if m.login == loginAccount {
		cardW := 54
		if cardW > w-4 {
			cardW = w - 4
		}
		if cardW < 24 {
			cardW = w - 2
		}
		title := stylePink.Render("👤 QQ 音乐账号")
		var bodyLines []string
		bodyLines = append(bodyLines,
			"",
			"  "+styleActive.Render("✓ 已登录"),
			"",
		)
		who := m.session.Nickname
		if m.session.Uin != "" {
			who += "（" + m.session.Uin + "）"
		}
		bodyLines = append(bodyLines, "  "+styleText.Render("当前账号：")+styleActive.Render(who), "")
		if m.loggingOut {
			bodyLines = append(bodyLines, "  "+styleWarn.Render("正在退出登录…"), "")
		}
		bodyLines = append(bodyLines,
			"",
			"  "+line("s", "扫码换一个账号"),
			"  "+line("x", "退出登录"),
			"  "+line("esc", "关闭"),
		)
		return renderModalCard(w, h, cardW, title, bodyLines, "", colPink)
	}

	title := stylePink.Render("📱 扫码登录 " + m.loginTypeLabel() + " 音乐")

	// 扫码模式且有二维码
	if len(m.qrLines) > 0 {
		qrW := lipgloss.Width(m.qrLines[0])
		msg := m.loginMsg
		if msg == "" {
			msg = "请用手机扫描二维码"
		}
		if m.login == loginScanned {
			msg = "✓ " + m.loginMsg
		}

		// 宽屏模式：左右并排（左侧二维码，右侧状态与操作按键）
		if w >= 74 {
			cardW := qrW + 36
			if cardW > w-4 {
				cardW = w - 4
			}
			rightW := cardW - qrW - 6
			rightLines := []string{
				styleActive.Render("QQ 音乐 · 扫码登录"),
				"",
				styleText.Render(msg),
				"",
				line("r", "重新生成二维码"),
				line("t", "切换 QQ / 微信登录"),
				line("o", "用系统看图器打开图片"),
				line("esc", "关闭（不登录）"),
				"",
				styleDim.Render("二维码过期后按 r 重新生成。"),
			}

			maxRows := len(m.qrLines)
			if len(rightLines) > maxRows {
				maxRows = len(rightLines)
			}
			var bodyLines []string
			for i := 0; i < maxRows; i++ {
				lStr, rStr := "", ""
				if i < len(m.qrLines) {
					lStr = m.qrLines[i]
				} else {
					lStr = strings.Repeat(" ", qrW)
				}
				if i < len(rightLines) {
					rStr = rightLines[i]
				}
				bodyLines = append(bodyLines, lStr+"  "+ansi.Truncate(rStr, rightW, "…"))
			}
			return renderModalCard(w, h, cardW, title, bodyLines, "", colPink)
		}

		// 较窄屏模式：垂直排布，紧凑排列
		cardW := qrW + 6
		if cardW > w-4 {
			cardW = w - 4
		}
		if cardW < 24 {
			cardW = w - 2
		}
		var bodyLines []string
		bodyLines = append(bodyLines, m.qrLines...)
		bodyLines = append(bodyLines,
			"  "+styleText.Render(msg),
			"  "+line("r", "重新生成二维码"),
			"  "+line("t", "切换平台"),
			"  "+line("o", "看图器打开"),
			"  "+line("esc", "关闭（不登录）"),
		)
		return renderModalCard(w, h, cardW, title, bodyLines, "", colPink)
	}

	// 其它状态（获取中、失败、成功或无二维码降级）
	cardW := 56
	if cardW > w-4 {
		cardW = w - 4
	}
	if cardW < 24 {
		cardW = w - 2
	}
	var bodyLines []string
	switch m.login {
	case loginStarting:
		bodyLines = append(bodyLines, "", "  "+styleText.Render("正在获取二维码…"), "")
	case loginFailed:
		bodyLines = append(bodyLines, "", "  "+styleWarn.Render("✗ "+m.loginFailureText()))
		if m.loginErr != nil {
			bodyLines = append(bodyLines, "    "+styleDim.Render(m.loginErr.Error()))
		}
		bodyLines = append(bodyLines, "")
	case loginSucceed:
		bodyLines = append(bodyLines, "", "  "+styleActive.Render("✓ 登录成功"))
		if m.session.Nickname != "" {
			bodyLines = append(bodyLines, "    "+styleText.Render(m.session.Nickname))
		}
		bodyLines = append(bodyLines, "")
	default:
		bodyLines = append(bodyLines,
			"  "+styleWarn.Render("二维码没法画在终端里："+m.qrNote),
			"  "+styleText.Render("文件："+m.qrPath),
			"  "+styleDim.Render("按 o 用系统看图器打开"),
			"",
		)
	}
	bodyLines = append(bodyLines,
		"  "+line("r", "重新生成二维码"),
		"  "+line("t", "切换 QQ / 微信登录"),
		"  "+line("o", "用系统看图器打开图片"),
		"  "+line("esc", "关闭（不登录）"),
	)
	return renderModalCard(w, h, cardW, title, bodyLines, "", colPink)
}

func (m Model) loginFailureText() string {
	if strings.TrimSpace(m.loginMsg) != "" {
		return m.loginMsg
	}
	return "登录失败，按 r 重试"
}
