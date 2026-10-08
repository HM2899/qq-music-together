package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// makeTestAudio 用 ffmpeg 生成一段 10 秒的测试音频，避免测试依赖外网。
func makeTestAudio(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("没有 ffmpeg，跳过")
	}
	path := filepath.Join(t.TempDir(), "test.mp3")
	cmd := exec.Command("ffmpeg", "-y", "-f", "lavfi",
		"-i", "sine=frequency=440:duration=10", "-q:a", "9", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("生成测试音频失败: %v\n%s", err, out)
	}
	return path
}

func TestMpvPlaybackControl(t *testing.T) {
	audio := makeTestAudio(t)
	m, err := StartMpv(70, "--ao=null", "--no-config")
	if err != nil {
		t.Fatalf("启动 mpv 失败: %v", err)
	}
	defer m.Close()

	if err := m.Load(audio); err != nil {
		t.Fatalf("载入失败: %v", err)
	}

	// 等 mpv 解析出时长。
	var dur float64
	for i := 0; i < 40; i++ {
		if dur = m.Duration(); dur > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if dur < 9 || dur > 11 {
		t.Fatalf("时长应约 10 秒，实际 %v", dur)
	}

	// 播放一小段后进度应前进。
	time.Sleep(1200 * time.Millisecond)
	if pos := m.TimePos(); pos <= 0 {
		t.Fatalf("进度应大于 0，实际 %v", pos)
	}

	// 暂停应生效。
	if err := m.SetPause(true); err != nil {
		t.Fatalf("暂停失败: %v", err)
	}
	if !m.Paused() {
		t.Fatal("应处于暂停状态")
	}
	time.Sleep(600 * time.Millisecond)
	pausedPos := m.TimePos()
	time.Sleep(600 * time.Millisecond)
	if moved := m.TimePos() - pausedPos; moved > 0.3 {
		t.Fatalf("暂停后进度仍在走: %v", moved)
	}

	// 跳转应生效。
	if err := m.SeekAbs(8); err != nil {
		t.Fatalf("跳转失败: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	if pos := m.TimePos(); pos < 7.5 {
		t.Fatalf("跳转后进度应约 8，实际 %v", pos)
	}

	// 音量设置应生效。
	if err := m.SetVolume(33); err != nil {
		t.Fatalf("设置音量失败: %v", err)
	}
}

func TestMpvDetectsExternalKill(t *testing.T) {
	audio := makeTestAudio(t)
	m, err := StartMpv(70, "--ao=null", "--no-config")
	if err != nil {
		t.Fatalf("启动 mpv 失败: %v", err)
	}
	defer m.Close()

	if err := m.Load(audio); err != nil {
		t.Fatalf("载入失败: %v", err)
	}
	if m.Dead() {
		t.Fatal("刚启动不应是已退出状态")
	}

	// 模拟被外部杀掉。
	if err := m.cmd.Process.Kill(); err != nil {
		t.Fatalf("杀 mpv 失败: %v", err)
	}
	for i := 0; i < 60; i++ {
		if m.Dead() {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !m.Dead() {
		t.Fatal("应检测到 mpv 已退出")
	}
	// 死后调用不应 panic，而是安全返回。
	_ = m.TimePos()
	_ = m.SetPause(true)
}

// TestMpvUIShowsDeath 验证界面会提示播放器退出。
func TestMpvUIShowsDeath(t *testing.T) {
	audio := makeTestAudio(t)
	mpv, err := StartMpv(70, "--ao=null", "--no-config")
	if err != nil {
		t.Fatalf("启动 mpv 失败: %v", err)
	}
	defer mpv.Close()

	m := newModelWithQueue(mpv, []Track{{ID: "1", Title: "A", DirectURL: audio, Lyrics: []Lyric{{Time: 0, Text: "a"}}}})
	m.w, m.h = 100, 30

	_ = mpv.cmd.Process.Kill()
	for i := 0; i < 60; i++ {
		if mpv.Dead() {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	next, _ := m.Update(tickMsg(time.Now()))
	nm := next.(Model)
	if !strings.Contains(stripANSI(nm.View()), "mpv 已退出") {
		t.Errorf("界面应提示 mpv 已退出\n%s", stripANSI(nm.View()))
	}
}
