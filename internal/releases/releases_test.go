package releases

import "testing"

func TestMatchingAssetForDoesNotTreatDarwinAsWindows(t *testing.T) {
	assets := []Asset{
		{Name: "alx-darwin-arm64.zip", URL: "mac-arm"},
		{Name: "alx-darwin-amd64.zip", URL: "mac-intel"},
		{Name: "alx-windows-amd64.zip", URL: "windows"},
	}

	if got := matchingAssetFor(assets, "windows", "amd64"); got.URL != "windows" {
		t.Fatalf("Windows asset = %#v, want windows archive", got)
	}
	if got := matchingAssetFor(assets, "darwin", "arm64"); got.URL != "mac-arm" {
		t.Fatalf("Darwin arm64 asset = %#v, want mac arm archive", got)
	}
}

func TestVersionCompareSupportsSemverPrereleases(t *testing.T) {
	tests := []struct {
		left, right string
		want        int
	}{
		{"v1.2.3", "v1.2.3-beta.1", 1},
		{"1.2.3-beta.1", "v1.2.3-beta.2", -1},
		{"v1.2.3+build.8", "v1.2.3", 0},
		{"v2.0.0", "v1.99.99", 1},
	}
	for _, test := range tests {
		if got := versionCompare(test.left, test.right); got != test.want {
			t.Errorf("versionCompare(%q, %q) = %d, want %d", test.left, test.right, got, test.want)
		}
	}
}

func TestMatchingAssetForRequiresExactPlatformAndArchitecture(t *testing.T) {
	assets := []Asset{{Name: "alx-darwin-arm64.zip", URL: "mac-arm"}}
	if got := matchingAssetFor(assets, "windows", "amd64"); got.Name != "" {
		t.Fatalf("Windows should not receive unmatched asset: %#v", got)
	}
}
