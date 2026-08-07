package setupplugin

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestRegistryListsValidPluginWithNavigation(t *testing.T) {
	root := t.TempDir()
	docker := filepath.Join(root, "docker")
	if err := os.Mkdir(docker, 0755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"id":"docker","name":"Docker","version":"1.0.0","navigation":{"label":"Docker","icon":"◇","order":10},"entry":{"linux-amd64":"runner"},"web":{"root":"web"}}`
	if err := os.WriteFile(filepath.Join(docker, manifestName), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}
	plugin, err := NewRegistry(root).Find("docker")
	if err != nil {
		t.Fatal(err)
	}
	if plugin.Navigation.Label != "Docker" || plugin.Web == nil || plugin.Web.Root != "web" || !plugin.Runnable {
		t.Fatalf("unexpected plugin: %#v", plugin)
	}
}

func TestDecodeManifestAcceptsValidWebRoot(t *testing.T) {
	plugin, err := decodeManifest([]byte(`{"id":"demo","name":"Demo","version":"1.0.0","web":{"root":"web"}}`), "/plugins/demo")
	if err != nil {
		t.Fatalf("valid web root rejected: %v", err)
	}
	if plugin.Web == nil || plugin.Web.Root != "web" {
		t.Fatalf("web root not preserved: %#v", plugin.Web)
	}
}

func TestDecodeManifestRejectsUnsafeWebRoot(t *testing.T) {
	for _, root := range []string{"/etc", "../escape", "a/../b", "", "  "} {
		if _, err := decodeManifest([]byte(`{"id":"demo","name":"Demo","version":"1.0.0","web":{"root":"`+root+`"}}`), "/plugins/demo"); err == nil {
			t.Fatalf("unsafe web root %q must be rejected", root)
		}
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
	manifest := `{"id":"fixture","name":"Fixture","version":"1.0.0","entry":{"` + key + `":"runner"},"web":{"root":"web"}}`
	if err := os.WriteFile(filepath.Join(directory, manifestName), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(directory, "web"), 0755); err != nil {
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

func TestRegistrySkipsPluginWithoutWeb(t *testing.T) {
	root := t.TempDir()
	noweb := filepath.Join(root, "noweb")
	if err := os.Mkdir(noweb, 0755); err != nil {
		t.Fatal(err)
	}
	// A manifest without a web root is no longer a usable setup plugin.
	manifest := `{"id":"noweb","name":"NoWeb","version":"1.0.0"}`
	if err := os.WriteFile(filepath.Join(noweb, manifestName), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewRegistry(root).Find("noweb"); err == nil {
		t.Fatal("plugin without web root must not be discovered")
	}
}

func TestRegistryCanDisableAndReenablePlugin(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "fixture")
	if err := os.Mkdir(directory, 0755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"id":"fixture","name":"Fixture","version":"1.0.0","web":{"root":"web"}}`
	if err := os.WriteFile(filepath.Join(directory, manifestName), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry(root)
	registry.statePath = filepath.Join(t.TempDir(), "plugins.json")
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
			_, _ = w.Write([]byte(`{"id":"alemonx-network","name":"网络","version":"1.0.0","web":{"root":"web"}}`))
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

func TestRegistryHotPlugReflectsAddedPlugin(t *testing.T) {
	root := t.TempDir()
	registry := NewRegistry(root)
	registry.Rescan()
	before := registry.Revision()

	// Add a new plugin directory with a valid manifest and web root.
	directory := filepath.Join(root, "newone")
	if err := os.MkdirAll(filepath.Join(directory, "web"), 0755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"id":"newone","name":"New","version":"1.0.0","web":{"root":"web"}}`
	if err := os.WriteFile(filepath.Join(directory, manifestName), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}

	registry.Rescan()
	after := registry.Revision()
	if after <= before {
		t.Fatalf("revision must bump after adding a plugin (before=%d after=%d)", before, after)
	}
	if _, err := registry.Find("newone"); err != nil {
		t.Fatalf("new plugin must be discoverable after rescan: %v", err)
	}
}

func TestRegistrySubscribeSignalsChange(t *testing.T) {
	root := t.TempDir()
	registry := NewRegistry(root)
	registry.Rescan()
	changes := registry.Subscribe()
	defer registry.Unsubscribe(changes)

	// A rescan that changes nothing must not wake subscribers.
	registry.Rescan()
	select {
	case <-changes:
		t.Fatal("unexpected change signal when the plugin set did not change")
	default:
	}

	directory := filepath.Join(root, "newone")
	if err := os.MkdirAll(filepath.Join(directory, "web"), 0755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"id":"newone","name":"New","version":"1.0.0","web":{"root":"web"}}`
	if err := os.WriteFile(filepath.Join(directory, manifestName), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}
	registry.Rescan()
	select {
	case <-changes:
	case <-time.After(time.Second):
		t.Fatal("expected a change signal after the plugin set changed")
	}

	// Unsubscribe stops further delivery.
	registry.Unsubscribe(changes)
	if err := os.RemoveAll(directory); err != nil {
		t.Fatal(err)
	}
	registry.Rescan()
	select {
	case <-changes:
		t.Fatal("subscriber still receives signals after unsubscribe")
	default:
	}
}

func requireGit(t *testing.T) string {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is required for online plugin installation tests")
	}
	return git
}

// makeGitPluginRepo commits a plugin manifest to a throwaway local repository so
// Install can clone it without network access.
func makeGitPluginRepo(t *testing.T) string {
	t.Helper()
	git := requireGit(t)
	remote := t.TempDir()
	if err := os.MkdirAll(filepath.Join(remote, "web"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"id":"alemonx-network","name":"网络","version":"1.0.0","web":{"root":"web"}}`
	if err := os.WriteFile(filepath.Join(remote, manifestName), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		command := exec.Command(git, args...)
		command.Dir = remote
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, output)
		}
	}
	run("init", "-q", "-b", "main")
	run("add", ".")
	run("-c", "user.email=test@example.com", "-c", "user.name=test", "commit", "-q", "-m", "init")
	return remote
}

func TestRegistryInstallsOnlinePluginLocally(t *testing.T) {
	remote := makeGitPluginRepo(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/apps-x.md":
			_, _ = w.Write([]byte("[network]: https://github.com/lemonade-lab/alemonx-network\n"))
		case "/alx.json":
			_, _ = w.Write([]byte(`{"id":"alemonx-network","name":"网络","version":"1.0.0","web":{"root":"web"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	root := t.TempDir()
	registry := Registry{
		roots:             []string{root},
		onlineIndexURL:    server.URL + "/apps-x.md",
		httpClient:        server.Client(),
		onlineManifestURL: func(string) string { return server.URL + "/alx.json" },
		cloneURL:          func(string) string { return remote },
	}
	installed, err := registry.Install("alemonx-network")
	if err != nil {
		t.Fatalf("install failed: %v", err)
	}
	if installed.Online {
		t.Fatal("installed plugin must be local, not online-only")
	}
	if !installed.Enabled {
		t.Fatal("installed plugin must be enabled")
	}
	all := registry.All()
	if len(all) != 1 || all[0].ID != "alemonx-network" || all[0].Online || all[0].Source != root {
		t.Fatalf("installed plugin should be the sole local entry: %#v", all)
	}
	if _, err := registry.Install("alemonx-network"); err == nil {
		t.Fatal("duplicate install must be rejected")
	}
}

func TestRegistryInstallRejectsUnknownPlugin(t *testing.T) {
	registry := Registry{roots: []string{t.TempDir()}}
	if _, err := registry.Install("nope"); err == nil {
		t.Fatal("unknown plugin id must be rejected")
	}
}
