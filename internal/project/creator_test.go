package project

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDevelopmentTemplatePackagesFollowSelections(t *testing.T) {
	root := t.TempDir()
	if err := copyTemplate(os.DirFS("../../templates"), "dev", root); err != nil {
		t.Fatal(err)
	}
	config := Config{
		Template:            "dev",
		Language:            "js",
		UsePM2:              false,
		ImageMode:           "none",
		StyleMode:           "css",
		DevelopmentPackages: []string{"database", "onebot"},
	}
	if err := patchPackage(root, config); err != nil {
		t.Fatal(err)
	}
	if err := patchDevelopmentSource(root, config); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	var pkg struct {
		DevDependencies map[string]string `json:"devDependencies"`
		Scripts         map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		t.Fatal(err)
	}
	for _, dependency := range []string{"@alemonjs/db", "@alemonjs/onebot", "alemonjs", "lvyjs"} {
		if pkg.DevDependencies[dependency] == "" {
			t.Errorf("expected %s to be installed", dependency)
		}
	}
	for _, dependency := range []string{"@alemonjs/bubble", "@alemonjs/discord", "@alemonjs/qq-bot", "jsxp", "pm2", "tailwindcss", "@types/node"} {
		if _, ok := pkg.DevDependencies[dependency]; ok {
			t.Errorf("did not expect %s to be installed", dependency)
		}
	}
	if pkg.Scripts["dev"] != "lvy app.js" {
		t.Errorf("dev script = %q, want JavaScript entry", pkg.Scripts["dev"])
	}
	if _, err := os.Stat(filepath.Join(root, "src", "index.js")); err != nil {
		t.Errorf("JavaScript entry missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "src", "response", "help.ts")); !os.IsNotExist(err) {
		t.Error("TypeScript help file should be renamed for JavaScript projects")
	}
	help, err := os.ReadFile(filepath.Join(root, "src", "response", "help.js"))
	if err != nil || string(help) == "" || strings.Contains(string(help), "jsxp") {
		t.Errorf("non-image help template = %q, %v", help, err)
	}
}

func TestPatchPackageDeclaresSelectedPackageManager(t *testing.T) {
	root := t.TempDir()
	if err := copyTemplate(os.DirFS("../../templates"), "bot", root); err != nil {
		t.Fatal(err)
	}
	if err := patchPackage(root, Config{Name: "example", PackageManager: "pnpm", Language: "ts", ImageMode: "none", StyleMode: "css"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	var pkg struct {
		PackageManager string `json:"packageManager"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil || pkg.PackageManager != "pnpm@9.15.0" {
		t.Fatalf("package manager = %q, %v", pkg.PackageManager, err)
	}
}

func TestCreateCommandReportsMissingEnvironmentClearly(t *testing.T) {
	t.Setenv("PATH", "")
	logs := []string{}
	err := run(t.TempDir(), &logs, "git", "--version")
	if err == nil || !strings.Contains(err.Error(), "左上角“环境”") {
		t.Fatalf("run error = %v, want Git installation guidance", err)
	}
	if strings.Contains(err.Error(), "executable file not found") {
		t.Fatalf("run error leaked raw exec error: %q", err)
	}
}
