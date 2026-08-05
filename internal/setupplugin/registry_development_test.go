package setupplugin

import "testing"

func TestDevelopmentRunnerIsUsedWhenReleaseRunnerIsMissing(t *testing.T) {
	plugin := Plugin{
		Source:  t.TempDir(),
		Runtime: "binary",
		Entry:   map[string]string{"missing-platform": "dist/runner"},
		Development: &RuntimeSpec{
			Runtime: "go",
			Entry:   map[string]string{"go": "runner/main.go"},
		},
	}
	entry, err := plugin.entryPath()
	if err != nil {
		t.Fatal(err)
	}
	if entry.name != "go" || len(entry.args) != 2 || entry.args[0] != "run" {
		t.Fatalf("unexpected development entry: %#v", entry)
	}
}
