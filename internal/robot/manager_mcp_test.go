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
