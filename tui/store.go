package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite" // 纯 Go 实现，不需要 cgo / gcc
)

// ---------- 播放状态持久化（SQLite） ----------
//
// 退出时把「队列 + 第几首 + 播到哪 + 音量 + 音质」存下来，下次启动原样恢复（暂停在原位置，
// 按空格从那里接着放）。数据库默认在 ~/.local/share/qqmusic-tui/state.db，
// QQMUSIC_TUI_DB 可覆盖（测试用，免得碰用户的真库）。
//
// 两张表：
//   settings(key, value)        —— 零散的标量（当前下标、位置、音量……）
//   queue(pos, song_mid, track) —— 播放队列，一首一行，track 是曲目的 JSON
// 队列只有内容变了才整表重写；位置这类高频变化每隔几秒落一次盘。

const storeSchema = `
CREATE TABLE IF NOT EXISTS settings (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS queue (
	pos      INTEGER PRIMARY KEY,
	song_mid TEXT NOT NULL,
	track    TEXT NOT NULL
);
`

// savedState 是存 / 读的一份快照。
type savedState struct {
	Queue    []Track
	CurIndex int
	Position float64
	Volume   int
	Muted    bool
	Quality  string
}

// Store 包一个 SQLite 连接。所有方法对 nil 接收者都是空操作——
// 数据库打不开（磁盘只读之类）时 TUI 照常能用，只是不记状态。
type Store struct {
	db *sql.DB

	mu        sync.Mutex
	queueSig  string    // 上次写入的队列指纹，没变就不重写队列表
	stateSig  string    // 上次写入的整体指纹（不含位置）
	lastSaved time.Time // 上次落盘时间，位置按这个节流
}

// storeSaveInterval 是只有播放位置在变时的落盘间隔。
const storeSaveInterval = 5 * time.Second

func storePath() string {
	if p := os.Getenv("QQMUSIC_TUI_DB"); p != "" {
		return p
	}
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "qqmusic-tui", "state.db")
}

// OpenStore 打开（必要时创建）数据库。
func OpenStore(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("找不到数据目录")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	// busy_timeout：同时开两个 TUI 时，写锁冲突等一会儿而不是直接报错。
	// WAL：读写互不阻塞，异常退出也不会把库写坏。
	dsn := "file:" + path + "?_pragma=busy_timeout(3000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // 单连接：本来就只有我们一个写者，省得 SQLite 自己和自己抢锁
	if _, err := db.Exec(storeSchema); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() {
	if s == nil || s.db == nil {
		return
	}
	s.db.Close()
}

// Load 读出上次的状态。ok=false 表示库里还没有东西（第一次运行）。
func (s *Store) Load() (st savedState, ok bool, err error) {
	if s == nil {
		return st, false, nil
	}
	settings := map[string]string{}
	rows, err := s.db.Query(`SELECT key, value FROM settings`)
	if err != nil {
		return st, false, err
	}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			rows.Close()
			return st, false, err
		}
		settings[k] = v
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return st, false, err
	}

	qrows, err := s.db.Query(`SELECT track FROM queue ORDER BY pos`)
	if err != nil {
		return st, false, err
	}
	for qrows.Next() {
		var raw string
		if err := qrows.Scan(&raw); err != nil {
			qrows.Close()
			return st, false, err
		}
		var t Track
		if json.Unmarshal([]byte(raw), &t) == nil && t.SongMid != "" {
			st.Queue = append(st.Queue, t)
		}
	}
	qrows.Close()
	if err := qrows.Err(); err != nil {
		return st, false, err
	}

	if len(settings) == 0 && len(st.Queue) == 0 {
		return st, false, nil
	}
	st.CurIndex = atoiDefault(settings["cur_index"], -1)
	st.Position, _ = strconv.ParseFloat(settings["position"], 64)
	st.Volume = atoiDefault(settings["volume"], 70)
	st.Muted = settings["muted"] == "1"
	st.Quality = settings["quality"]
	// 存的下标对不上队列（队列被截断之类）就当没在放，别越界。
	if st.CurIndex >= len(st.Queue) {
		st.CurIndex = -1
	}
	return st, true, nil
}

// Save 写一份快照。force=false 时节流：只有位置在变就每 storeSaveInterval 才写一次，
// 别的东西（切歌、改队列、调音量、换音质、暂停）一变立刻写。
func (s *Store) Save(st savedState, force bool) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	qsig := queueSignature(st.Queue)
	ssig := fmt.Sprintf("%s|%d|%d|%v|%s", qsig, st.CurIndex, st.Volume, st.Muted, st.Quality)
	if !force && ssig == s.stateSig && time.Since(s.lastSaved) < storeSaveInterval {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // Commit 之后再 Rollback 是空操作

	set := func(k, v string) error {
		_, err := tx.Exec(`INSERT INTO settings(key, value) VALUES(?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value`, k, v)
		return err
	}
	for _, kv := range [][2]string{
		{"cur_index", strconv.Itoa(st.CurIndex)},
		{"position", strconv.FormatFloat(st.Position, 'f', 3, 64)},
		{"volume", strconv.Itoa(st.Volume)},
		{"muted", boolStr(st.Muted)},
		{"quality", st.Quality},
		{"saved_at", strconv.FormatInt(time.Now().Unix(), 10)},
	} {
		if err := set(kv[0], kv[1]); err != nil {
			return err
		}
	}

	if qsig != s.queueSig {
		if _, err := tx.Exec(`DELETE FROM queue`); err != nil {
			return err
		}
		stmt, err := tx.Prepare(`INSERT INTO queue(pos, song_mid, track) VALUES(?, ?, ?)`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for i, t := range st.Queue {
			raw, err := json.Marshal(t) // Lyrics / DirectURL 带 json:"-"，不进库
			if err != nil {
				return err
			}
			if _, err := stmt.Exec(i, t.SongMid, string(raw)); err != nil {
				return err
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	s.queueSig, s.stateSig, s.lastSaved = qsig, ssig, time.Now()
	return nil
}

// LoadQuality 单独读音质设置（还没有任何播放状态时也要用得上）。
func (s *Store) LoadQuality() string {
	if s == nil {
		return ""
	}
	var q string
	_ = s.db.QueryRow(`SELECT value FROM settings WHERE key = 'quality'`).Scan(&q)
	return q
}

// queueSignature 是队列内容的指纹：曲目 mid 依次拼起来。够用来判断「队列变没变」。
func queueSignature(q []Track) string {
	var b strings.Builder
	for _, t := range q {
		b.WriteString(t.SongMid)
		b.WriteByte(',')
	}
	return b.String()
}

func atoiDefault(s string, def int) int {
	if v, err := strconv.Atoi(s); err == nil {
		return v
	}
	return def
}

func boolStr(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
