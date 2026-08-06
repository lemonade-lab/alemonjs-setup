package robot

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestMCPProjectFilesStayWithinSafeProjectWorkspace(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "node_modules", "example"), 0755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"package.json":                  "{}",
		"src/index.ts":                  "export const ready = true\n",
		".env":                          "TOKEN=private",
		"node_modules/example/index.js": "module.exports = 1",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	manager := Manager{}
	files, err := manager.ListProjectFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"package.json", "src/index.ts"}; !reflect.DeepEqual(files, want) {
		t.Fatalf("ListProjectFiles() = %#v, want %#v", files, want)
	}

	result, err := manager.ReadProjectFile(root, "src/index.ts")
	if err != nil || result.Output != "export const ready = true\n" {
		t.Fatalf("ReadProjectFile() = %#v, %v", result, err)
	}
	if _, err := manager.ReadProjectFile(root, ".env"); err == nil {
		t.Fatal("ReadProjectFile(.env) should be rejected")
	}
	if _, err := manager.WriteProjectFile(root, "src/index.ts", "export const ready = false\n"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "src/index.ts"))
	if err != nil || !strings.Contains(string(data), "false") {
		t.Fatalf("written content = %q, %v", data, err)
	}
}

func TestReadMissingEditableConfigurationAsEmptyDocument(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	manager := Manager{}
	for _, name := range []string{"alemon.config.yaml", ".npmrc"} {
		result, err := manager.Read(root, name)
		if err != nil {
			t.Fatalf("Read(%q) returned error: %v", name, err)
		}
		if result.Output != "" || result.Path != filepath.Join(root, name) {
			t.Fatalf("Read(%q) = %#v, want empty document at configuration path", name, result)
		}
	}
	if _, err := manager.Read(root, "README.md"); err == nil {
		t.Fatal("Read missing README.md should still report a missing file")
	}
}

func TestRepairPM2CreatesRunnableProductionEntryAndConfig(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"name":"example"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := (Manager{}).RepairRuntime(root, "pm2"); err != nil {
		t.Fatal(err)
	}
	entry, err := os.ReadFile(filepath.Join(root, "index.js"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(entry), "import { start } from 'alemonjs';\n\nstart();\n"; got != want {
		t.Fatalf("index.js = %q, want %q", got, want)
	}
	config, err := os.ReadFile(filepath.Join(root, "pm2.config.cjs"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"module.exports = pm2 ||", "name: 'alemonb'", "script: 'node index.js'", "NODE_ENV: 'production'"} {
		if !strings.Contains(string(config), expected) {
			t.Errorf("pm2 config does not contain %q:\n%s", expected, config)
		}
	}
}

func TestPackageManagerCommandFallsBackToNPXWithoutGlobalYarn(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "yarn.lock"), []byte("# lock\n"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "")
	command, notice := PackageManagerCommand(root, "install")
	if command.Args[0] != "npx" || !strings.Contains(strings.Join(command.Args, " "), "yarn@1.22.22 install") {
		t.Fatalf("fallback command = %#v", command.Args)
	}
	if !strings.Contains(notice, "不会修改电脑的全局安装") {
		t.Fatalf("fallback notice = %q", notice)
	}
}

func TestRunReportsMissingNodeEnvironmentWithoutRawExecError(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PATH", "")
	_, err := run(root, "npx", "--version")
	if err == nil || !strings.Contains(err.Error(), "Node.js") {
		t.Fatalf("npx error = %v, want Node.js guidance", err)
	}
	if strings.Contains(err.Error(), "executable file not found") {
		t.Fatalf("npx error leaked raw exec error: %q", err)
	}
}

func TestPermissionAdviceStaysInWebOperationFlow(t *testing.T) {
	message := permissionAdvice("保存 alemon.config.yaml").Error()
	for _, expected := range []string{"没有权限", "系统设置", "albs"} {
		if !strings.Contains(message, expected) {
			t.Fatalf("permission advice = %q, missing %q", message, expected)
		}
	}
}

func TestPM2LogPaginationStartsWithNewestPage(t *testing.T) {
	lines := make([]string, 241)
	for index := range lines {
		lines[index] = fmt.Sprintf("line-%d", index+1)
	}
	latest := paginatePM2LogLines(lines, 1)
	if !strings.HasSuffix(latest.Output, "line-241") || strings.HasPrefix(latest.Output, "line-1\n") || !latest.HasOlder {
		t.Fatalf("latest page = %#v", latest)
	}
	older := paginatePM2LogLines(lines, 2)
	if !strings.HasSuffix(older.Output, "line-121") || strings.HasSuffix(older.Output, "line-241") || !older.HasOlder {
		t.Fatalf("older page = %#v", older)
	}
	oldest := paginatePM2LogLines(lines, 3)
	if oldest.Output != "line-1" || oldest.HasOlder {
		t.Fatalf("oldest page = %#v", oldest)
	}
}

func TestRuntimeDependenciesDetectsMissingDirectPackage(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"dependencies":{"present":"1","missing":"1"},"devDependencies":{"@scope/tool":"1"}}`), 0644); err != nil {
		t.Fatal(err)
	}
	for _, packageFile := range []string{"node_modules/present/package.json", "node_modules/@scope/tool/package.json"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, packageFile)), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, packageFile), []byte(`{}`), 0644); err != nil {
			t.Fatal(err)
		}
	}
	missing, err := (Manager{}).RuntimeDependencies(root)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(missing, []string{"missing 未安装"}) {
		t.Fatalf("missing dependencies = %#v", missing)
	}
}
