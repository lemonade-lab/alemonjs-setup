package catalog

import (
	"net/url"
	"strings"
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

func TestRepositoryInstallURLKeepsTreeRef(t *testing.T) {
	if got, want := repositoryInstallURL("https://github.com/example/project/tree/v1.2.3/packages/demo"), "https://github.com/example/project.git#v1.2.3"; got != want {
		t.Fatalf("install URL = %q, want %q", got, want)
	}
	if got, want := repositoryInstallURL("https://gitee.com/example/project"), "https://gitee.com/example/project.git"; got != want {
		t.Fatalf("default install URL = %q, want %q", got, want)
	}
}

func TestGitTagHigherPrefersNewestSemanticTag(t *testing.T) {
	if !gitTagHigher("v2.1.0", "v1.99.0") {
		t.Fatal("newer semantic tag should sort first")
	}
	if !gitTagHigher("v1.0.0", "main") {
		t.Fatal("release tag should sort ahead of arbitrary tag")
	}
	if gitTagHigher("v1.0.0", "v1.2.0") {
		t.Fatal("older semantic tag must not sort first")
	}
}

func TestCatalogTableHeadersAreNotCatalogItems(t *testing.T) {
	for _, name := range []string{"项目", "项目名", "Project", "package"} {
		if !isCatalogTableHeader(name) {
			t.Errorf("%q should be recognised as a table header", name)
		}
	}
	if isCatalogTableHeader("@alemonjs/qq-bot") {
		t.Fatal("a real package must not be recognised as a table header")
	}
}

func TestCatalogUsesDescriptionHeaderInsteadOfSecondColumn(t *testing.T) {
	groups, _, err := parseCatalog(strings.NewReader(`### 官方
| 项目 | 版本 | 说明 |
| --- | --- | --- |
| [@alemonjs/discord] | [![discord-s]][discord-p] | Discord |
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || len(groups[0].Items) != 1 {
		t.Fatalf("unexpected catalog: %#v", groups)
	}
	if got := groups[0].Items[0].Description; got != "Discord" {
		t.Fatalf("description = %q, want Discord", got)
	}
	if got := groups[0].Items[0].Name; got != "@alemonjs/discord" {
		t.Fatalf("name = %q, want unbracketed package name", got)
	}
}

func TestCatalogResolvesReferencesAfterWhitespaceTrim(t *testing.T) {
	groups, references, err := parseCatalog(strings.NewReader(`### 官方
| 项目 | 说明 |
| --- | --- |
| [@alemonjs/discord] | Discord |

[@alemonjs/discord]: https://github.com/lemonade-lab/alemonjs/tree/main/packages/discord
`))
	if err != nil {
		t.Fatal(err)
	}
	item := &groups[0].Items[0]
	item.URL = references[item.Name]
	if item.URL == "" {
		t.Fatal("reference URL was not resolved for trimmed package name")
	}
}

func TestCatalogFallsBackToSecondColumnWithoutDescriptionHeader(t *testing.T) {
	groups, _, err := parseCatalog(strings.NewReader(`### 旧目录
| 项目 | 标签 |
| --- | --- |
| [example] | 旧目录说明 |
`))
	if err != nil {
		t.Fatal(err)
	}
	if got := groups[0].Items[0].Description; got != "旧目录说明" {
		t.Fatalf("description = %q, want second column", got)
	}
}

func TestCatalogRecognisesDocsHeader(t *testing.T) {
	groups, _, err := parseCatalog(strings.NewReader(`### 文档目录
| Package | Version | docs |
| --- | --- | --- |
| [example] | 1.0.0 | 文档说明 |
`))
	if err != nil {
		t.Fatal(err)
	}
	if got := groups[0].Items[0].Description; got != "文档说明" {
		t.Fatalf("description = %q, want docs column", got)
	}
}

func TestUniqueStringsKeepsNewestVersionFirst(t *testing.T) {
	got := uniqueStrings([]string{"1.2.0", "1.2.0", "1.1.0", ""})
	if len(got) != 2 || got[0] != "1.2.0" || got[1] != "1.1.0" {
		t.Fatalf("unique strings = %#v", got)
	}
}
