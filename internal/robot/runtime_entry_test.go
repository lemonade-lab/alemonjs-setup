package robot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeEntryFileUsesMain(t *testing.T) {
	root := t.TempDir()
	cases := []struct {
		main string
		want string
	}{
		{"", "index.js"},
		{"index.js", "index.js"},
		{"./src/index.ts", "src/index.ts"},
		{"dist/index.js", "dist/index.js"},
		{"../escape", "index.js"}, // path traversal rejected
	}
	for _, c := range cases {
		if got := runtimeEntryFile(root, c.main); got != c.want {
			t.Errorf("runtimeEntryFile(%q) = %q, want %q", c.main, got, c.want)
		}
	}
}

// TestRepairRuntimeRespectsMainEntry verifies that repairing a project which
// declares a main entry produces a script that runs that entry instead of a
// hard-coded index.js.
func TestRepairRuntimeRespectsMainEntry(t *testing.T) {
	root := t.TempDir()
	writeWebViewFixture(t, filepath.Join(root, "package.json"), `{"name":"bot","main":"src/index.ts","scripts":{}}`)
	result, err := (Manager{}).RepairRuntime(root, "dev")
	if err != nil {
		t.Fatalf("RepairRuntime(dev): %v", err)
	}
	if !strings.Contains(result.Output, "补齐运行脚本") {
		t.Fatalf("unexpected output: %q", result.Output)
	}
	data, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"app": "node src/index.ts"`) {
		t.Fatalf("app script should target main entry:\n%s", data)
	}
	if !strings.Contains(string(data), `"dev": "node src/index.ts"`) {
		t.Fatalf("dev script should follow app:\n%s", data)
	}
}
