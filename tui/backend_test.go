package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeTestFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func TestLocateScriptLayoutsAndPriority(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "portable space", "bin", "qqmusic-tui")
	writeTestFile(t, executable, "", 0o755)
	share := filepath.Join(root, "portable space", "share", "qqmusic-tui", "backend", "qqmusic_api.py")
	dev := filepath.Join(filepath.Dir(executable), "backend", "qqmusic_api.py")
	local := filepath.Join(root, "usr", "local", "share", "qqmusic-tui", "backend", "qqmusic_api.py")
	system := filepath.Join(root, "usr", "share", "qqmusic-tui", "backend", "qqmusic_api.py")
	override := filepath.Join(root, "explicit space", "api.py")
	for _, path := range []string{share, dev, local, system, override} {
		writeTestFile(t, path, "# test", 0o644)
	}
	dirs := []string{filepath.Dir(local), filepath.Dir(system)}
	got, err := locateScript(override, executable, dirs)
	if err != nil || got != override {
		t.Fatalf("显式覆盖: %q, %v", got, err)
	}
	for _, want := range []string{share, dev, local, system} {
		got, err := locateScript("", executable, dirs)
		if err != nil || got != want {
			t.Fatalf("应定位 %q，实际 %q, %v", want, got, err)
		}
		if err := os.Remove(want); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := locateScript("", executable, dirs); err == nil || !strings.Contains(err.Error(), "QQMUSIC_API") {
		t.Fatalf("全部缺失应提示设置环境变量: %v", err)
	}
}

func TestLocateScriptInvalidOverrideDoesNotFallback(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "qqmusic-tui")
	writeTestFile(t, filepath.Join(root, "backend", "qqmusic_api.py"), "", 0o644)
	for _, path := range []string{filepath.Join(root, "missing.py"), root} {
		got, err := locateScript(path, executable, nil)
		if got != path || err == nil || !strings.Contains(err.Error(), path) {
			t.Fatalf("错误覆盖不能回退: %q, %v", got, err)
		}
	}
}

func TestLocateScriptSymlink(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "real", "bin", "qqmusic-tui")
	script := filepath.Join(root, "real", "share", "qqmusic-tui", "backend", "qqmusic_api.py")
	writeTestFile(t, executable, "", 0o755)
	writeTestFile(t, script, "", 0o644)
	link := filepath.Join(root, "launcher")
	if err := os.Symlink(executable, link); err != nil {
		t.Fatal(err)
	}
	got, err := locateScript("", link, nil)
	if err != nil || got != script {
		t.Fatalf("符号链接应从真实安装目录定位: %q, %v", got, err)
	}
}

func TestLocateScriptIgnoresWorkingDirectory(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)
	for _, path := range []string{"qqmusic_api.py", "backend/qqmusic_api.py", "share/qqmusic-tui/backend/qqmusic_api.py"} {
		writeTestFile(t, filepath.Join(cwd, path), "raise RuntimeError('cwd trap')", 0o644)
	}
	t.Setenv("XDG_CONFIG_HOME", cwd)
	writeTestFile(t, filepath.Join(cwd, "quickshell/statindet/scripts/qqmusic/qqmusic_api.py"), "", 0o644)
	for _, executable := range []string{filepath.Join(t.TempDir(), "bin", "qqmusic-tui"), "", "qqmusic-tui"} {
		if got, err := locateScript("", executable, nil); err == nil {
			t.Fatalf("不应使用 cwd/配置目录脚本: %q", got)
		}
	}
	got, err := locateScript("backend/qqmusic_api.py", "", nil)
	if err != nil || got != filepath.Join(cwd, "backend/qqmusic_api.py") {
		t.Fatalf("仅显式覆盖允许相对路径: %q, %v", got, err)
	}
}

func TestClientPythonOverrideAndPathsWithSpaces(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "paths with spaces")
	script := filepath.Join(root, "api test.py")
	writeTestFile(t, script, `print('{"ok":true,"loggedIn":false}')`, 0o644)
	interpreter := filepath.Join(root, "python test")
	if err := os.Symlink(python, interpreter); err != nil {
		t.Fatal(err)
	}
	t.Setenv("QQMUSIC_API", script)
	t.Setenv("QQMUSIC_PYTHON", interpreter)
	client := NewClient()
	if client.python != interpreter || client.ScriptPath() != script || client.setupErr != nil {
		t.Fatalf("未保留解释器/脚本覆盖: %+v", client)
	}
	if _, err := client.SessionStatus(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Setenv("QQMUSIC_PYTHON", filepath.Join(root, "missing-python"))
	_, err = NewClient().SessionStatus(context.Background())
	if KindOf(err) != KindScriptMissing || !strings.Contains(err.Error(), "QQMUSIC_PYTHON") {
		t.Fatalf("缺解释器错误: %v", err)
	}
	t.Setenv("QQMUSIC_PYTHON", "")
	t.Setenv("PATH", t.TempDir())
	_, err = NewClient().SessionStatus(context.Background())
	if KindOf(err) != KindScriptMissing || !strings.Contains(err.Error(), "python3") {
		t.Fatalf("缺默认 Python 错误: %v", err)
	}
}

func TestBundledBackendOffline(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal("真实后端离线测试需要 Python 3: ", err)
	}
	script, err := filepath.Abs("backend/test_backend.py")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, "-I", "-B", script, "-v")
	cmd.Dir = t.TempDir()
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("真实后端离线测试失败: %v\n%s", err, output)
	}
}
