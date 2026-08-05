package robot

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWebViewsDiscoverCurrentRobotPluginsAndContainFiles(t *testing.T) {
	root := t.TempDir()
	writeWebViewFixture(t, filepath.Join(root, "package.json"), `{"name":"robot","dependencies":{"web-dep":"1.0.0"}}`)
	writeWebViewFixture(t, filepath.Join(root, "packages", "local-web", "package.json"), `{"name":"local-web","description":"local","alemonjs":{"web":{"root":"dist"},"desktop":{"sidebars":[{"name":"本地页面"}]}}}`)
	writeWebViewFixture(t, filepath.Join(root, "packages", "local-web", "dist", "index.html"), `<script src="/assets/app.js"></script>`)
	writeWebViewFixture(t, filepath.Join(root, "packages", "local-web", "dist", "assets", "app.js"), `console.log('ok')`)
	writeWebViewFixture(t, filepath.Join(root, "node_modules", "web-dep", "package.json"), `{"name":"web-dep","alemonjs":{"web":{"root":"dist"},"desktop":{"sidebars":[{"name":"依赖页面"}]}}}`)
	writeWebViewFixture(t, filepath.Join(root, "node_modules", "web-dep", "dist", "index.html"), `ok`)

	entries, err := (Manager{}).WebViews(root)
	if err != nil {
		t.Fatalf("WebViews: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	var local WebViewEntry
	for _, entry := range entries {
		if entry.Package == "local-web" {
			local = entry
		}
	}
	if local.ID == "" || local.Name != "本地页面" {
		t.Fatalf("unexpected local entry: %#v", local)
	}
	file, err := (Manager{}).WebViewFile(root, local.ID, "assets/app.js")
	if err != nil || filepath.Base(file) != "app.js" {
		t.Fatalf("asset = %q, %v", file, err)
	}
	if _, err := (Manager{}).WebViewFile(root, local.ID, "../../package.json"); err == nil {
		t.Fatal("path traversal must be rejected")
	}
}

func writeWebViewFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
