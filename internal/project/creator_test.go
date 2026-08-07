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

// TestDevelopmentTemplateKeepsOnlyChosenLanguageVariant verifies that after
// patching, a generated project holds exactly one variant per source file:
// .js for JS projects and .ts/.tsx for TS projects, with no leftover files
// that would mix extensions or carry TypeScript syntax into JavaScript.
func TestDevelopmentTemplateKeepsOnlyChosenLanguageVariant(t *testing.T) {
	pairs := [][2]string{
		{"app", "app"},
		{"lvy.config", "lvy.config"},
		{"src/index", "src/index"},
		{"src/expose", "src/expose"},
		{"src/store", "src/store"},
		{"src/response/hello", "src/response/hello"},
		{"src/response/help", "src/response/help"},
	}
	run := func(t *testing.T, language string, wantExt string, dropExts []string) {
		t.Helper()
		root := t.TempDir()
		if err := copyTemplate(os.DirFS("../../templates"), "dev", root); err != nil {
			t.Fatal(err)
		}
		config := Config{Template: "dev", Language: language, ImageMode: "react", StyleMode: "css"}
		if err := patchPackage(root, config); err != nil {
			t.Fatal(err)
		}
		if err := patchDevelopmentSource(root, config); err != nil {
			t.Fatal(err)
		}
		for _, pair := range pairs {
			path := filepath.Join(root, pair[0]+"."+wantExt)
			if _, err := os.Stat(path); err != nil {
				t.Errorf("%s variant missing for %s project: %v", pair[0], language, err)
			}
		}
		for _, dropExt := range dropExts {
			for _, pair := range pairs {
				path := filepath.Join(root, pair[0]+"."+dropExt)
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Errorf("%s.%s should be removed in %s project", pair[0], dropExt, language)
				}
			}
		}
	}
	run(t, "js", "js", []string{"ts"})
	run(t, "ts", "ts", []string{"js"})
}

// TestDevelopmentTemplateJSFilesCarryNoTypeScriptSyntax ensures the hand-kept
// JS variants really are plain JavaScript, so a JS project never parses TS.
func TestDevelopmentTemplateJSFilesCarryNoTypeScriptSyntax(t *testing.T) {
	for _, file := range []string{"src/store.js", "src/expose.js"} {
		data, err := os.ReadFile(filepath.Join("../../templates/dev", file))
		if err != nil {
			t.Fatal(err)
		}
		content := string(data)
		if strings.Contains(content, "Map<string") || strings.Contains(content, ": string") {
			t.Errorf("%s still contains TypeScript syntax:\n%s", file, content)
		}
	}
}

func TestWriteAgentsFileReflectsSelections(t *testing.T) {
	root := t.TempDir()
	if err := writeAgentsFile(root, Config{Language: "ts", ESLint: true}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	for _, want := range []string{"AlemonJS", "npx tsc --noEmit", "npx eslint src --ext .ts", "src/"} {
		if !strings.Contains(content, want) {
			t.Errorf("AGENTS.md 缺少 %q：\n%s", want, content)
		}
	}
	if strings.Contains(content, "main.ts") {
		t.Errorf("JS 项目不应含 .ts 验证命令")
	}
}

func TestWriteAgentsFileJSWithoutESLint(t *testing.T) {
	root := t.TempDir()
	if err := writeAgentsFile(root, Config{Language: "js", ESLint: false}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if strings.Contains(string(data), "eslint") {
		t.Error("未启用 ESLint 时不应写入 eslint 验证命令")
	}
	if strings.Contains(string(data), "--ext .js") {
		t.Error("未启用 ESLint 时不应有 .js 扩展验证")
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
