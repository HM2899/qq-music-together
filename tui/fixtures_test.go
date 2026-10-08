package main

// fixtureDiscover 给探索页一份不联网的假数据。
// 探索页现在只显示接口返回的东西，所以需要探索页有行的测试都得先摆上它。
func fixtureDiscover() *DiscoverResult {
	return &DiscoverResult{
		DailyPlaylist: Playlist{ID: "p1", Kind: "playlist", Title: "每日推荐"},
		DailyTracks: []Track{
			{ID: "d1", SongMid: "d1", Title: "Midnight Groove", Artist: "Nightowls", Playable: true},
			{ID: "d2", SongMid: "d2", Title: "Neon Architect", Artist: "Cybergrid", Playable: true},
		},
	}
}

// demoSongs 是测试用的假曲目。产品代码里已经没有「离线演示曲」了——
// 探索页只显示接口返回的内容，所以这份数据只服务于渲染/发布相关的单测。
//
// 带歌词是刻意的：publish、View 的歌词滚动、手动浏览这些测试都要有歌词才走得通。
// DirectURL 不填，避免哪天有测试顺手把它当成真的能播。
var demoSongs = []Track{
	{
		ID:       "1",
		SongMid:  "demo-1",
		Title:    "Midnight Groove",
		Artist:   "Nightowls",
		Album:    "Lofi Chill Beats",
		Duration: 200,
		Playable: true,
		Lyrics: []Lyric{
			{Time: 0, Text: "🎵 [智能音效已开启 - 纯音乐前奏] 🎵"},
			{Time: 5, Text: "夜色渐深，城市里霓虹在闪烁"},
			{Time: 10, Text: "戴上耳机，听旋律在星空下漂泊"},
			{Time: 16, Text: "有些故事，在音符里轻轻诉说"},
			{Time: 22, Text: "跟着这首歌，感受这瞬间的温热"},
			{Time: 29, Text: "🎵 [深夜旋律渐入佳境] 🎵"},
			{Time: 35, Text: "流淌的电音，带走白天的所有疲惫"},
			{Time: 41, Text: "我们心跳，在虚空信号里交汇"},
			{Time: 47, Text: "哪怕隔着千山万水，听着同一个音轨"},
			{Time: 53, Text: "这就是，我们专属的音乐包围"},
			{Time: 60, Text: "让忧伤随风，让快乐常留心底"},
			{Time: 67, Text: "听完这首歌，晚安，亲爱的你"},
			{Time: 75, Text: "🎵 [智能降噪 - 音乐淡出] 🎵"},
		},
	},
	{
		ID:       "2",
		SongMid:  "demo-2",
		Title:    "Neon Architect",
		Artist:   "Cybergrid",
		Album:    "Synthwave Sunset",
		Duration: 200,
		Playable: true,
		Lyrics: []Lyric{
			{Time: 0, Text: "⚡ [赛博朋克极速前奏] ⚡"},
			{Time: 6, Text: "穿梭在赛博霓虹，迎着八十年代的晚风"},
			{Time: 12, Text: "数字太阳缓缓下沉，落入电子地平线中"},
			{Time: 18, Text: "光流在指尖跃动，音浪将理智放空"},
			{Time: 25, Text: "这极速的节奏，拉近你我的时空"},
			{Time: 31, Text: "⚡ [电吉他合成器独奏] ⚡"},
			{Time: 38, Text: "正版无损音质，享受极致的纯净耳道"},
			{Time: 44, Text: "房间里闪耀着绿光，这是夜行的讯号"},
			{Time: 50, Text: "跟随着电子节拍，把所有烦恼都撕掉"},
			{Time: 56, Text: "在这虚拟城市，我们就是主角"},
			{Time: 63, Text: "⚡ [高频电音震撼收尾] ⚡"},
		},
	},
	{
		ID:       "3",
		SongMid:  "demo-3",
		Title:    "Acoustic Sunsets",
		Artist:   "Willow & Wind",
		Album:    "Summer Breeze",
		Duration: 200,
		Playable: true,
		Lyrics: []Lyric{
			{Time: 0, Text: "🍃 [清脆民谣木吉他前奏] 🍃"},
			{Time: 4, Text: "微风吹拂着山谷，稻浪在夕阳下起舞"},
			{Time: 9, Text: "回家的石子小路，盛开着蓝色的风铃草"},
			{Time: 15, Text: "你静静靠在窗前，听风中传来的童谣"},
			{Time: 21, Text: "生活虽然有些忙碌，这首歌陪你笑一笑"},
			{Time: 27, Text: "🍃 [温柔口琴和弦插入] 🍃"},
			{Time: 33, Text: "纯真的歌声，是不灭的温暖灯火"},
			{Time: 39, Text: "正版的高清音质，把自然搬进耳朵"},
			{Time: 45, Text: "谢谢此刻的陪伴，让孤单走远一点"},
			{Time: 51, Text: "让这份美好，永不散场，永不孤单"},
			{Time: 58, Text: "🍃 [风铃声伴随吉他渐弱] 🍃"},
		},
	},
}
