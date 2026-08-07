package robot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestScopedConnectionPackageUsesShortNamespace covers @alemonjs/* packages,
// whose desktop.platform entries declare only a short name (onebot) and no
// value. The config must be keyed by that short name, not the scoped package
// name, matching how the framework reads the connection section.
func TestScopedConnectionPackageUsesShortNamespace(t *testing.T) {
	root := t.TempDir()
	writeWebViewFixture(t, filepath.Join(root, "package.json"), `{"name":"robot"}`)
	writeWebViewFixture(t, filepath.Join(root, "node_modules", "@alemonjs", "onebot", "package.json"), `{
  "name":"@alemonjs/onebot",
  "alemonjs":{
    "config":[{"name":"token","type":"string","required":true,"description":"token"}],
    "desktop":{"platform":[{"name":"onebot"}]}
  }
}`)
	writeWebViewFixture(t, filepath.Join(root, "alemon.config.yaml"), "onebot:\n  token: \"abc\"\n")

	config, err := (Manager{}).PackageConfig(root, "@alemonjs/onebot")
	if err != nil {
		t.Fatalf("PackageConfig: %v", err)
	}
	if config.Namespace != "onebot" {
		t.Fatalf("namespace = %q, want onebot", config.Namespace)
	}
	if config.Values["token"] != "abc" {
		t.Fatalf("values = %#v, want token=abc", config.Values)
	}
}

// TestScopedConnectionPackageReadsLegacyKey keeps values written by older
// versions that keyed the section by the scoped package name.
func TestScopedConnectionPackageReadsLegacyKey(t *testing.T) {
	root := t.TempDir()
	writeWebViewFixture(t, filepath.Join(root, "package.json"), `{"name":"robot"}`)
	writeWebViewFixture(t, filepath.Join(root, "node_modules", "@alemonjs", "onebot", "package.json"), `{
  "name":"@alemonjs/onebot",
  "alemonjs":{
    "config":[{"name":"token","type":"string","required":true,"description":"token"}],
    "desktop":{"platform":[{"name":"onebot"}]}
  }
}`)
	writeWebViewFixture(t, filepath.Join(root, "alemon.config.yaml"), `'@alemonjs/onebot':
  token: "legacy"
`)

	config, err := (Manager{}).PackageConfig(root, "@alemonjs/onebot")
	if err != nil {
		t.Fatalf("PackageConfig: %v", err)
	}
	if config.Values["token"] != "legacy" {
		t.Fatalf("values = %#v, want legacy token preserved", config.Values)
	}
}

// TestSaveScopedConnectionMigratesLegacyKey writes to the short key and removes
// the stale scoped-package block in one pass.
func TestSaveScopedConnectionMigratesLegacyKey(t *testing.T) {
	root := t.TempDir()
	writeWebViewFixture(t, filepath.Join(root, "package.json"), `{"name":"robot"}`)
	writeWebViewFixture(t, filepath.Join(root, "node_modules", "@alemonjs", "onebot", "package.json"), `{
  "name":"@alemonjs/onebot",
  "alemonjs":{
    "config":[
      {"name":"token","type":"string","required":true,"description":"token"},
      {"name":"url","type":"string","required":true,"description":"连接地址"}
    ],
    "desktop":{"platform":[{"name":"onebot"}]}
  }
}`)
	writeWebViewFixture(t, filepath.Join(root, "alemon.config.yaml"), `'@alemonjs/onebot':
  token: "old"
  url: "ws://old"
`)

	if _, err := (Manager{}).SavePackageConfig(root, "@alemonjs/onebot", map[string]string{
		"token": "new",
		"url":   "ws://new",
	}); err != nil {
		t.Fatalf("SavePackageConfig: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "alemon.config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	output := string(data)
	if !strings.Contains(output, "onebot:") {
		t.Fatalf("saved config misses short key:\n%s", output)
	}
	if strings.Contains(output, "@alemonjs/onebot") {
		t.Fatalf("saved config kept legacy scoped key:\n%s", output)
	}
	if !strings.Contains(output, `token: "new"`) || !strings.Contains(output, `url: "ws://new"`) {
		t.Fatalf("saved config misses new values:\n%s", output)
	}
}
