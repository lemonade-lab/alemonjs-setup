package project

import (
	"os"
	"path/filepath"
	"testing"
)

// TestZZGenerateCombinationsForLint generates one project per guided-flow
// configuration combination into a fixed directory (no dependency install) so
// an external eslint pass can verify every generated source is lint-clean.
func TestZZGenerateCombinationsForLint(t *testing.T) {
	out := os.Getenv("LINT_OUT")
	if out == "" {
		t.Skip("LINT_OUT not set")
	}
	combos := []struct{ lang, image, style string }{
		{"js", "none", "css"},
		{"js", "react", "css"},
		{"js", "react", "tailwind"},
		{"js", "react", "sass"},
		{"js", "react", "less"},
		{"ts", "none", "css"},
		{"ts", "react", "css"},
		{"ts", "react", "tailwind"},
		{"ts", "react", "sass"},
		{"ts", "react", "less"},
	}
	for i, c := range combos {
		dir := filepath.Join(out, "proj", c.lang+"-"+c.image+"-"+c.style)
		if err := os.RemoveAll(dir); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := copyTemplate(os.DirFS("../../templates"), "dev", dir); err != nil {
			t.Fatalf("combo %d %s copy: %v", i, c.lang, err)
		}
		config := Config{Template: "dev", Language: c.lang, ImageMode: c.image, StyleMode: c.style, ESLint: true}
		if err := patchPackage(dir, config); err != nil {
			t.Fatalf("combo %d %s package: %v", i, c.lang, err)
		}
		if err := patchDevelopmentSource(dir, config); err != nil {
			t.Fatalf("combo %d %s source: %v", i, c.lang, err)
		}
		t.Logf("generated %s", dir)
	}
}
