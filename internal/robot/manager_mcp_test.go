package robot

import (
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
