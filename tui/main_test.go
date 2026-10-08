package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStartupArgs(t *testing.T) {
	for _, tc := range []struct {
		args    []string
		handled bool
		code    int
		want    string
	}{
		{nil, false, 0, ""},
		{[]string{"--version"}, true, 0, "qqmusic-tui " + version + " (commit " + commit + ")"},
		{[]string{"--help"}, true, 0, "QQMUSIC_API"},
		{[]string{"-h"}, true, 0, "QQMUSIC_PYTHON"},
		{[]string{"--unknown"}, true, 2, "参数无效"},
		{[]string{"--help", "extra"}, true, 2, "参数无效"},
	} {
		var out, stderr bytes.Buffer
		handled, code := startupArgs(tc.args, &out, &stderr)
		if handled != tc.handled || code != tc.code || !strings.Contains(out.String()+stderr.String(), tc.want) {
			t.Errorf("参数 %v: handled=%v code=%d stdout=%q stderr=%q", tc.args, handled, code, out.String(), stderr.String())
		}
		if tc.code == 0 && stderr.Len() != 0 || tc.code != 0 && out.Len() != 0 {
			t.Errorf("参数 %v 使用了错误输出流", tc.args)
		}
	}
}

func TestCLIFlagsBeforeInitialization(t *testing.T) {
	// 真正编译发行二进制，同时验证 main.version/main.commit ldflags 名称。
	binary := filepath.Join(t.TempDir(), "qqmusic-tui")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	build := exec.CommandContext(ctx, "go", "build", "-buildvcs=false", "-ldflags", "-X main.version=1.2.3-test -X main.commit=offline123", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("构建命令行测试二进制: %v\n%s", err, output)
	}
	for _, arg := range []string{"--version", "--help", "-h", "--invalid"} {
		t.Run(arg, func(t *testing.T) {
			root := t.TempDir()
			cmd := exec.CommandContext(ctx, binary, arg)
			cmd.Dir = root
			// 不继承宿主依赖或状态路径；不存在的 HOME、XDG 目录不应被创建。
			cmd.Env = []string{
				"HOME=" + filepath.Join(root, "home"),
				"PATH=" + filepath.Join(root, "no-tools"),
				"XDG_CACHE_HOME=" + filepath.Join(root, "cache"),
				"XDG_CONFIG_HOME=" + filepath.Join(root, "config"),
				"XDG_DATA_HOME=" + filepath.Join(root, "data"),
				"XDG_STATE_HOME=" + filepath.Join(root, "state"),
				"XDG_RUNTIME_DIR=" + filepath.Join(root, "runtime"),
				"QQMUSIC_API=" + filepath.Join(root, "missing-api.py"),
				"QQMUSIC_PYTHON=" + filepath.Join(root, "missing-python"),
				"QQMUSIC_TUI_DB=" + filepath.Join(root, "state.db"),
				"QQMUSIC_TUI_STATE_FILE=" + filepath.Join(root, "now.json"),
				"QQMUSIC_TUI_CONFIG=" + filepath.Join(root, "settings.json"),
			}
			var out, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &out, &stderr
			err := cmd.Run()
			if arg == "--invalid" {
				if cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != 2 || !strings.Contains(stderr.String(), "参数无效") {
					t.Fatalf("非法参数退出不符: %v, %q", err, stderr.String())
				}
			} else {
				if err != nil || stderr.Len() != 0 {
					t.Fatalf("无依赖启动参数失败: %v, %q", err, stderr.String())
				}
				if arg == "--version" {
					if out.String() != "qqmusic-tui 1.2.3-test (commit offline123)\n" {
						t.Fatalf("ldflags 未生效: %q", out.String())
					}
				} else if !strings.Contains(out.String(), "QQMUSIC_API") {
					t.Fatalf("帮助输出不符: %q", out.String())
				}
			}
			entries, err := os.ReadDir(root)
			if err != nil || len(entries) != 0 {
				t.Fatalf("参数处理产生了状态文件: %v, %v", entries, err)
			}
		})
	}
}
