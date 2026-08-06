package robot

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanPublishFilesExcludesDependenciesAndHiddenFiles(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"dist/index.js", "dist/assets/app.js", "node_modules/pkg/index.js", ".cache/item", ".git/config", "package.json", "README.md"} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	items := scanPublishFiles(root, "")
	has := func(value string) bool {
		for _, item := range items {
			if item == value {
				return true
			}
		}
		return false
	}
	for _, value := range []string{"dist", "dist/index.js", "dist/assets", "dist/assets/app.js", "README.md"} {
		if !has(value) {
			t.Fatalf("expected scanned artifact %q, got %#v", value, items)
		}
	}
	for _, value := range []string{"node_modules", ".cache", ".git", "package.json"} {
		if has(value) {
			t.Fatalf("unexpected scanned artifact %q", value)
		}
	}
}
