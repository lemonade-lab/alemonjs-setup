package releases

import "testing"

func TestMatchingAssetForDoesNotTreatDarwinAsWindows(t *testing.T) {
	assets := []Asset{
		{Name: "albs-darwin-arm64.zip", URL: "mac-arm"},
		{Name: "albs-darwin-amd64.zip", URL: "mac-intel"},
		{Name: "albs-windows-amd64.zip", URL: "windows"},
	}

	if got := matchingAssetFor(assets, "windows", "amd64"); got.URL != "windows" {
		t.Fatalf("Windows asset = %#v, want windows archive", got)
	}
	if got := matchingAssetFor(assets, "darwin", "arm64"); got.URL != "mac-arm" {
		t.Fatalf("Darwin arm64 asset = %#v, want mac arm archive", got)
	}
}

func TestMatchingAssetForRequiresExactPlatformAndArchitecture(t *testing.T) {
	assets := []Asset{{Name: "albs-darwin-arm64.zip", URL: "mac-arm"}}
	if got := matchingAssetFor(assets, "windows", "amd64"); got.Name != "" {
		t.Fatalf("Windows should not receive unmatched asset: %#v", got)
	}
}
