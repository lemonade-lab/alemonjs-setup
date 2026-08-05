package catalog

import (
	"net/url"
	"testing"
)

func TestRepositoryFileCandidatesKeepURLDirectory(t *testing.T) {
	tests := []struct {
		source   string
		filename string
		want     string
	}{
		{"https://github.com/example/project/blob/main/packages/kook/README.md", "README.md", "https://raw.githubusercontent.com/example/project/main/packages/kook/README.md"},
		{"https://github.com/example/project/blob/main/packages/kook/README.md", "package.json", "https://raw.githubusercontent.com/example/project/main/packages/kook/package.json"},
		{"https://github.com/example/project/tree/main/packages/kook", "README.md", "https://raw.githubusercontent.com/example/project/main/packages/kook/README.md"},
		{"https://github.com/example/project/tree/main/packages/kook", "package.json", "https://raw.githubusercontent.com/example/project/main/packages/kook/package.json"},
	}
	for _, test := range tests {
		parsed, err := url.Parse(test.source)
		if err != nil {
			t.Fatal(err)
		}
		items, err := repositoryFileCandidates(parsed, test.filename)
		if err != nil || len(items) == 0 || items[0] != test.want {
			t.Fatalf("%s %s: got %v, %v", test.source, test.filename, items, err)
		}
	}
}
