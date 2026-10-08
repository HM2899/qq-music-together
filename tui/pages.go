package main

// Page 是顶部的六个页签，顺序与桌面客户端
// （Modules/QQMusic/QQMusicWindow.qml 的 navigationItems）保持一致。
type Page int

const (
	PageExplore Page = iota
	PageSearch
	PageNow
	PageFavorites
	PageLibrary
	PageQueue
	// PageMine 追加在最后而不是插到「音乐库」旁边：插中间会让原来 5、6 两个数字键整体后移。
	PageMine

	pageCount
)

// pageTitles 同时决定页签栏上显示的字和数字快捷键 1..6 的顺序。
var pageTitles = [pageCount]string{"探索", "搜索", "正在播放", "我喜欢", "音乐库", "播放队列", "我的歌单"}

func (p Page) title() string {
	if p < 0 || p >= pageCount {
		return ""
	}
	return pageTitles[p]
}

// Focus 决定按键往哪儿送。这是整个界面唯一的模态状态。
//
// 加焦点层而不是「记住现在是不是在输入」的布尔量，是因为输入框和列表对同一个键
// 的期望是相反的：在搜索框里按 q 应该打出字母 q，在列表里按 q 应该退出程序。
type Focus int

const (
	FocusList     Focus = iota // 普通浏览：单键命令直接生效
	FocusSearch                // 搜索框聚焦：除了少数几个键，其余全部进输入框
	FocusLogin                 // 登录层
	FocusHelp                  // 帮助浮层
	FocusComments              // 评论浮层
	FocusPicker                // 「加入哪个歌单」浮层
	FocusDialog                // 新建歌单 / 删除确认
)

// 搜索类型。字符串要和脚本 --type 的取值、以及 SearchResult.Type 完全一致。
const (
	searchTypeSong     = "song"
	searchTypeSinger   = "singer"
	searchTypeAlbum    = "album"
	searchTypePlaylist = "songlist"
)

// searchTypeOrder 决定 ctrl+t 循环切换的顺序。
var searchTypeOrder = []string{searchTypeSong, searchTypeSinger, searchTypeAlbum, searchTypePlaylist}

var searchTypeLabels = map[string]string{
	searchTypeSong:     "歌曲",
	searchTypeSinger:   "歌手",
	searchTypeAlbum:    "专辑",
	searchTypePlaylist: "歌单",
}

// listState 是一个列表页的滚动状态。
// 每个页面各一份，放在数组里而不是 map 里——Model 是值类型，
// 数组是值语义、复制时各走各的，map 是引用语义，值接收者里改它会在意料之外的地方生效。
type listState struct {
	cursor int // 选中项下标
	offset int // 视口顶部对应的下标
}

// clamp 把游标和偏移夹到合法范围，并保证游标落在视口内。
// 每次列表内容变化（换了搜索结果、删了队列项）之后都要调一次。
func (l *listState) clamp(length, viewport int) {
	if length <= 0 {
		l.cursor = 0
		l.offset = 0
		return
	}
	if l.cursor < 0 {
		l.cursor = 0
	}
	if l.cursor >= length {
		l.cursor = length - 1
	}
	if viewport < 1 {
		viewport = 1
	}
	if l.cursor < l.offset {
		l.offset = l.cursor
	}
	if l.cursor >= l.offset+viewport {
		l.offset = l.cursor - viewport + 1
	}
	maxOffset := length - viewport
	if maxOffset < 0 {
		maxOffset = 0
	}
	if l.offset > maxOffset {
		l.offset = maxOffset
	}
	if l.offset < 0 {
		l.offset = 0
	}
}

// move 上下移动游标。
func (l *listState) move(delta, length, viewport int) {
	l.cursor += delta
	l.clamp(length, viewport)
}

// selected 返回当前选中项的下标，列表为空时返回 -1。
func (l listState) selected(length int) int {
	if length <= 0 || l.cursor < 0 || l.cursor >= length {
		return -1
	}
	return l.cursor
}
