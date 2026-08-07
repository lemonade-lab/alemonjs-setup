package robot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRepairRuntimeUsesIndexJSEntry verifies that repairing a project produces
// scripts and PM2 config that launch the robot's index.js, never the package
// main field (which points at the lib/ build artifact).
func TestRepairRuntimeUsesIndexJSEntry(t *testing.T) {
	root := t.TempDir()
	// main points at the build output; repair must still target index.js.
	writeWebViewFixture(t, filepath.Join(root, "package.json"), `{"name":"bot","main":"lib/index.js","scripts":{}}`)
	if _, err := (Manager{}).RepairRuntime(root, "dev"); err != nil {
		t.Fatalf("RepairRuntime(dev): %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"app": "node index.js"`, `"dev": "node index.js"`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("script missing %q:\n%s", want, data)
		}
	}
}

// TestRepairRuntimePM2UsesIndexJSEntry ensures the PM2 repair config points at
// index.js and never rewrites the build artifact under lib/.
func TestRepairRuntimePM2UsesIndexJSEntry(t *testing.T) {
	root := t.TempDir()
	writeWebViewFixture(t, filepath.Join(root, "package.json"), `{"name":"bot","main":"lib/index.js","scripts":{}}`)
	if _, err := (Manager{}).RepairRuntime(root, "pm2"); err != nil {
		t.Fatalf("RepairRuntime(pm2): %v", err)
	}
	config, err := os.ReadFile(filepath.Join(root, "pm2.config.cjs"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(config), `script: './index.js'`) {
		t.Fatalf("PM2 config should run index.js:\n%s", config)
	}
	if strings.Contains(string(config), "lib/index.js") {
		t.Fatalf("PM2 config must not reference the build artifact:\n%s", config)
	}
	// The robot's startup script is created at the top level, not lib/.
	if _, err := os.Stat(filepath.Join(root, "index.js")); err != nil {
		t.Fatalf("index.js should exist after repair: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "lib", "index.js")); !os.IsNotExist(err) {
		t.Fatalf("lib/index.js should not be created by repair")
	}
}

// TestParsePM2ProcessesMapsJListFields verifies the pm2 jlist payload maps to
// the table fields the UI renders.
func TestParsePM2ProcessesMapsJListFields(t *testing.T) {
	output := `[
  {
    "pid": 9896,
    "name": "alemonb",
    "pm_id": 0,
    "status": "online",
    "restart_time": 2,
    "pm2_env": {"script": "./index.js", "namespace": "default", "pm_uptime": 1751900000000},
    "monit": {"memory": 123456789, "cpu": 0.5}
  }
]`
	items, err := parsePM2Processes(output)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d processes, want 1", len(items))
	}
	p := items[0]
	if p.Name != "alemonb" || p.ID != 0 || p.Status != "online" || p.PID != 9896 {
		t.Fatalf("process = %#v", p)
	}
	if p.Memory != 123456789 || p.Restarts != 2 || p.Script != "./index.js" || p.Namespace != "default" {
		t.Fatalf("process fields = %#v", p)
	}
	if p.CPU != 0.5 || p.Uptime != 1751900000000 {
		t.Fatalf("process monit fields = %#v", p)
	}
}

// TestStripPM2BannerAndParse covers a PM2 daemon version mismatch, where PM2
// writes a ">>>> In-memory PM2 is out-of-date" banner to stdout ahead of the
// JSON array. The banner must be stripped before parsing.
func TestStripPM2BannerAndParse(t *testing.T) {
	payload := ">>>> In-memory PM2 is out-of-date, do:\n>>>> $ pm2 update\nIn memory PM2 version: 7.0.3\nLocal PM2 version: 5.4.3\n\n[{\"pm_id\":0,\"name\":\"alemonb\",\"status\":\"online\",\"pid\":9896,\"pm2_env\":{\"script\":\"./index.js\",\"namespace\":\"default\",\"pm_uptime\":1751900000000},\"monit\":{\"memory\":123,\"cpu\":0.1}}]"
	stripped := stripPM2Banner(payload)
	items, err := parsePM2Processes(stripped)
	if err != nil {
		t.Fatalf("parse with banner: %v\nstripped=%q", err, stripped)
	}
	if len(items) != 1 || items[0].Name != "alemonb" {
		t.Fatalf("parsed = %#v, want one alemonb process", items)
	}
}
