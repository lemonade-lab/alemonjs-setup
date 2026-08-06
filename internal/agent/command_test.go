package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePackage(t *testing.T, root, scripts string) {
	t.Helper()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"name":"test","scripts":{` + scripts + `}}`
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Rejection tests never spawn a process: validation fails before exec.Command
// is invoked, so they are safe in any environment.

func TestCommandRejectsUnknownExecutable(t *testing.T) {
	runner := commandRunner{}
	_, err := runner.Run(context.Background(), t.TempDir(), "rm", []string{"-rf", "."})
	if err == nil || !strings.Contains(err.Error(), "不在允许列表") {
		t.Fatalf("未知命令应被拒绝：%v", err)
	}
}

func TestCommandRejectsPublish(t *testing.T) {
	root := t.TempDir()
	writePackage(t, root, `"build":"true"`)
	runner := commandRunner{}
	_, err := runner.Run(context.Background(), root, "npm", []string{"publish"})
	if err == nil || !strings.Contains(err.Error(), "被禁止") {
		t.Fatalf("publish 应被拒绝：%v", err)
	}
}

func TestCommandRejectsUnknownSubcommand(t *testing.T) {
	root := t.TempDir()
	writePackage(t, root, `"build":"true"`)
	runner := commandRunner{}
	_, err := runner.Run(context.Background(), root, "npm", []string{"config", "set", "registry", "https://evil.example"})
	if err == nil || !strings.Contains(err.Error(), "不在允许范围") {
		t.Fatalf("未知子命令应被拒绝：%v", err)
	}
}

func TestCommandRejectsUndefinedScript(t *testing.T) {
	root := t.TempDir()
	writePackage(t, root, `"build":"true"`)
	runner := commandRunner{}
	_, err := runner.Run(context.Background(), root, "npm", []string{"run", "nonexistent"})
	if err == nil || !strings.Contains(err.Error(), "没有脚本") {
		t.Fatalf("未声明脚本应被拒绝：%v", err)
	}
}

func TestNodeLimitedToCheck(t *testing.T) {
	root := t.TempDir()
	writePackage(t, root, `""`)
	runner := commandRunner{}
	if _, err := runner.Run(context.Background(), root, "node", []string{"-e", "require('fs').rmSync('/tmp', {recursive:true})"}); err == nil {
		t.Fatal("node -e 执行任意代码应被拒绝")
	}
	if _, err := runner.Run(context.Background(), root, "node", []string{"script.js"}); err == nil {
		t.Fatal("node <script> 执行任意脚本应被拒绝")
	}
}

// Allow-path tests exercise the pure validation logic without spawning a
// process, since a sandboxed test environment may not have npm/node on PATH.

func TestValidatePackageCommandAllowsDeclaredScript(t *testing.T) {
	root := t.TempDir()
	writePackage(t, root, `"build":"tsc --noEmit"`)
	if err := validatePackageCommand(root, "npm", []string{"run", "build"}); err != nil {
		t.Fatalf("已声明脚本应允许：%v", err)
	}
}

func TestValidatePackageCommandAllowsLifecycle(t *testing.T) {
	root := t.TempDir()
	writePackage(t, root, `"build":"true"`)
	for _, sub := range []string{"install", "build", "test", "dev"} {
		if err := validatePackageCommand(root, "npm", []string{sub}); err != nil {
			t.Fatalf("生命周期子命令 %s 应允许：%v", sub, err)
		}
	}
}

func TestTruncateOutput(t *testing.T) {
	long := strings.Repeat("x", maxCommandOutput+100)
	out := truncateOutput(long)
	if !strings.HasPrefix(out, strings.Repeat("x", maxCommandOutput)) || !strings.Contains(out, "已截断") {
		t.Errorf("截断应保留前 %d 字节并附标记", maxCommandOutput)
	}
}
