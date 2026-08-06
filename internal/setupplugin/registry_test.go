package setupplugin

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRegistryListsValidPluginWithNavigation(t *testing.T) {
	root := t.TempDir()
	docker := filepath.Join(root, "docker")
	if err := os.Mkdir(docker, 0755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"id":"docker","name":"Docker","version":"1.0.0","navigation":{"label":"Docker","icon":"◇","order":10},"pages":[{"id":"overview","label":"概览"},{"id":"compose","label":"Compose"}]}`
	if err := os.WriteFile(filepath.Join(docker, manifestName), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}
	plugin, err := NewRegistry(root).Find("docker")
	if err != nil {
		t.Fatal(err)
	}
	if plugin.Navigation.Label != "Docker" || len(plugin.Pages) != 2 || plugin.Pages[1].ID != "compose" {
		t.Fatalf("unexpected plugin: %#v", plugin)
	}
}

func TestRegistryRunsDeclaredBinaryAction(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-only")
	}
	root := t.TempDir()
	directory := filepath.Join(root, "fixture")
	if err := os.Mkdir(directory, 0755); err != nil {
		t.Fatal(err)
	}
	key := runtime.GOOS + "-" + runtime.GOARCH
	manifest := `{"id":"fixture","name":"Fixture","version":"1.0.0","entry":{"` + key + `":"runner"},"actions":[{"id":"check","label":"检查"}]}`
	if err := os.WriteFile(filepath.Join(directory, manifestName), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "runner"), []byte(`#!/bin/sh
cat >/dev/null
printf '{"output":"已检查"}'
`), 0755); err != nil {
		t.Fatal(err)
	}
	output, err := NewRegistry(root).Run("fixture", "check", nil, false)
	if err != nil || output != "已检查" {
		t.Fatalf("run = %q, %v", output, err)
	}
}

func TestRegistrySkipsMalformedPlugin(t *testing.T) {
	root := t.TempDir()
	broken := filepath.Join(root, "broken")
	if err := os.Mkdir(broken, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(broken, manifestName), []byte(`{"id":"bad"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewRegistry(root).Find("bad"); err == nil {
		t.Fatal("malformed plugin must not be discovered")
	}
}

func TestRegistryCanDisableAndReenablePlugin(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "fixture")
	if err := os.Mkdir(directory, 0755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"id":"fixture","name":"Fixture","version":"1.0.0"}`
	if err := os.WriteFile(filepath.Join(directory, manifestName), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}
	registry := Registry{roots: []string{root}, statePath: filepath.Join(t.TempDir(), "plugins.json")}
	if err := registry.SetEnabled("fixture", false); err != nil {
		t.Fatal(err)
	}
	if len(registry.List()) != 0 {
		t.Fatal("disabled plugin must not appear in active list")
	}
	if all := registry.All(); len(all) != 1 || all[0].Enabled {
		t.Fatalf("disabled plugin should remain manageable: %#v", all)
	}
	if err := registry.SetEnabled("fixture", true); err != nil {
		t.Fatal(err)
	}
	if active := registry.List(); len(active) != 1 || !active[0].Enabled {
		t.Fatalf("plugin should be active again: %#v", active)
	}
}

func TestRegistryRendersOnlinePluginFromAppsXIndex(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/apps-x.md":
			_, _ = w.Write([]byte("[network]: https://github.com/lemonade-lab/alemonx-network\n"))
		case "/alx.json":
			_, _ = w.Write([]byte(`{"id":"alemonx-network","name":"网络","version":"1.0.0","pages":[{"id":"overview","label":"概览"}],"actions":[{"id":"check","label":"检查","page":"overview"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	registry := Registry{
		onlineIndexURL: server.URL + "/apps-x.md",
		httpClient:     server.Client(),
		onlineManifestURL: func(string) string {
			return server.URL + "/alx.json"
		},
	}
	plugins := registry.List()
	if len(plugins) != 1 || !plugins[0].Online || plugins[0].Runnable || plugins[0].Name != "网络" {
		t.Fatalf("online plugin = %#v", plugins)
	}
	if _, err := registry.Run("alemonx-network", "check", nil, false); err == nil {
		t.Fatal("online plugin must not execute before installation")
	}
}
