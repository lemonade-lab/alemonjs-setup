package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type RepoSymbol struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Path     string `json:"path"`
	Line     int    `json:"line"`
	Exported bool   `json:"exported"`
}
type RepoReference struct {
	Symbol string `json:"symbol"`
	Path   string `json:"path"`
	Line   int    `json:"line"`
	Kind   string `json:"kind"`
}
type RepoIndex struct {
	Version    int             `json:"version"`
	RootHash   string          `json:"rootHash"`
	Files      []string        `json:"files"`
	Symbols    []RepoSymbol    `json:"symbols"`
	References []RepoReference `json:"references"`
	Routes     []RepoReference `json:"routes"`
}

var symbolPattern = regexp.MustCompile(`(?m)^\s*(export\s+)?(?:async\s+)?(?:function|class|interface|type|const|let|var)\s+([A-Za-z_$][\w$]*)|^\s*(?:func|type)\s+([A-Za-z_][\w]*)`)
var routePattern = regexp.MustCompile(`(?m)(alemonjs\.(?:onEvent|useEvent|onMessage)|useRoute|router\.(?:get|post|put|delete)|Router\()`)

func BuildRepoIndex(root string, files FileService) (RepoIndex, error) {
	paths, err := files.ListFiles(root)
	if err != nil {
		return RepoIndex{}, err
	}
	index := RepoIndex{Version: 1, RootHash: projectHash(root), Files: paths}
	for _, path := range paths {
		if !indexablePath(path) {
			continue
		}
		content, err := files.ReadFile(root, path)
		if err != nil {
			continue
		}
		for lineNo, line := range strings.Split(content, "\n") {
			matches := symbolPattern.FindStringSubmatch(line)
			if len(matches) > 0 {
				name := matches[2]
				if name == "" {
					name = matches[3]
				}
				if name != "" {
					kind := "symbol"
					if strings.Contains(line, "function") {
						kind = "function"
					}
					if strings.Contains(line, "class") {
						kind = "class"
					}
					if strings.Contains(line, "interface") {
						kind = "interface"
					}
					if strings.Contains(line, "type ") {
						kind = "type"
					}
					index.Symbols = append(index.Symbols, RepoSymbol{Name: name, Kind: kind, Path: path, Line: lineNo + 1, Exported: matches[1] != "" || strings.HasPrefix(strings.ToUpper(name[:1]), name[:1])})
				}
			}
			if routePattern.MatchString(line) {
				index.Routes = append(index.Routes, RepoReference{Symbol: strings.TrimSpace(line), Path: path, Line: lineNo + 1, Kind: "route/event"})
			}
		}
		for _, symbol := range index.Symbols {
			if strings.Contains(content, symbol.Name) {
				index.References = append(index.References, RepoReference{Symbol: symbol.Name, Path: path, Kind: "text"})
			}
		}
	}
	sort.Slice(index.Symbols, func(i, j int) bool {
		if index.Symbols[i].Path == index.Symbols[j].Path {
			return index.Symbols[i].Line < index.Symbols[j].Line
		}
		return index.Symbols[i].Path < index.Symbols[j].Path
	})
	return index, nil
}

type RepoIndexStore struct{ Dir string }

func NewRepoIndexStore(dir string) *RepoIndexStore { return &RepoIndexStore{Dir: dir} }
func (s *RepoIndexStore) path(root string) string {
	return filepath.Join(s.Dir, projectHash(root)+".json")
}
func (s *RepoIndexStore) Save(root string, index RepoIndex) error {
	if err := os.MkdirAll(s.Dir, 0700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path(root), append(raw, '\n'), 0600)
}
func (s *RepoIndexStore) Load(root string) (RepoIndex, error) {
	var index RepoIndex
	raw, err := os.ReadFile(s.path(root))
	if err != nil {
		return index, err
	}
	err = json.Unmarshal(raw, &index)
	return index, err
}
func FindSymbols(index RepoIndex, query, kind string) []RepoSymbol {
	var out []RepoSymbol
	for _, item := range index.Symbols {
		if strings.Contains(strings.ToLower(item.Name), strings.ToLower(query)) && (kind == "" || item.Kind == kind) {
			out = append(out, item)
		}
	}
	return limitSymbols(out)
}
func FindReferences(index RepoIndex, symbol string) []RepoReference {
	var out []RepoReference
	for _, item := range index.References {
		if item.Symbol == symbol {
			out = append(out, item)
		}
	}
	return limitReferences(out)
}
func limitSymbols(items []RepoSymbol) []RepoSymbol {
	if len(items) > 100 {
		return items[:100]
	}
	return items
}
func limitReferences(items []RepoReference) []RepoReference {
	if len(items) > 100 {
		return items[:100]
	}
	return items
}
func indexablePath(path string) bool {
	ext := filepath.Ext(path)
	switch ext {
	case ".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs", ".go", ".json", ".yaml", ".yml":
		return true
	}
	return false
}
func projectHash(root string) string {
	sum := sha256.Sum256([]byte(root))
	return hex.EncodeToString(sum[:8])
}
func (i RepoIndex) Summary() string {
	raw, _ := json.Marshal(map[string]any{"version": i.Version, "files": len(i.Files), "symbols": len(i.Symbols), "references": len(i.References), "routes": len(i.Routes)})
	return fmt.Sprint(string(raw))
}
