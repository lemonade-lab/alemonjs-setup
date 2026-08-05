package robot

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBackpackPackageCanBeConfiguredAndRemovedByManifestName(t *testing.T) {
	root := t.TempDir()
	writeWebViewFixture(t, filepath.Join(root, "package.json"), `{"name":"robot"}`)
	packagePath := filepath.Join(root, "packages", "checkout-folder", "package.json")
	writeWebViewFixture(t, packagePath, `{
  "name":"local-plugin",
  "alemonjs":{"config":[{"name":"token","type":"text","required":true,"description":"令牌"}]}
}`)

	config, err := (Manager{}).PackageConfig(root, "local-plugin")
	if err != nil {
		t.Fatalf("PackageConfig: %v", err)
	}
	if config.Namespace != "local-plugin" || len(config.Fields) != 1 {
		t.Fatalf("unexpected config: %#v", config)
	}
	result, err := removeLocalPackageByName(root, "local-plugin")
	if err != nil {
		t.Fatalf("removeLocalPackageByName: %v", err)
	}
	if result.Path != filepath.Join(root, "packages", "checkout-folder") {
		t.Fatalf("removed path = %q", result.Path)
	}
	if _, err := os.Stat(filepath.Dir(packagePath)); !os.IsNotExist(err) {
		t.Fatalf("package directory should be removed, stat error = %v", err)
	}
}
