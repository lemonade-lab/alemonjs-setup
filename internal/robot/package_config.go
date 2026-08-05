package robot

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type PackageConfigField struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Required    bool   `json:"required"`
	Description string `json:"description"`
}

type PackageConfig struct {
	Package   string               `json:"package"`
	Namespace string               `json:"namespace"`
	Fields    []PackageConfigField `json:"fields"`
	Values    map[string]string    `json:"values"`
}

type packageConfigManifest struct {
	Name     string `json:"name"`
	Alemonjs struct {
		Config  []PackageConfigField `json:"config"`
		Desktop struct {
			Platform []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"platform"`
		} `json:"desktop"`
	} `json:"alemonjs"`
}

var packageNamePattern = regexp.MustCompile(`^(?:@[a-zA-Z0-9][a-zA-Z0-9._-]*/)?[a-zA-Z0-9][a-zA-Z0-9._-]*$`)
var yamlNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)

func (m Manager) PackageConfig(root, name string) (PackageConfig, error) {
	path, err := projectPath(root)
	if err != nil {
		return PackageConfig{}, err
	}
	if !packageNamePattern.MatchString(name) {
		return PackageConfig{}, errors.New("包名无效")
	}
	data, err := os.ReadFile(filepath.Join(path, "node_modules", filepath.FromSlash(name), "package.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return PackageConfig{}, errors.New("该包尚未安装，安装后才能配置")
		}
		return PackageConfig{}, fmt.Errorf("无法读取包配置声明：%w", err)
	}
	var manifest packageConfigManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return PackageConfig{}, errors.New("包的 package.json 无法识别")
	}
	if manifest.Name == "" || len(manifest.Alemonjs.Config) == 0 {
		return PackageConfig{}, errors.New("该包没有声明 alemonjs.config")
	}
	namespace := manifest.Name
	for _, platform := range manifest.Alemonjs.Desktop.Platform {
		if platform.Value == manifest.Name && yamlNamePattern.MatchString(platform.Name) {
			namespace = platform.Name
			break
		}
	}
	content, err := os.ReadFile(filepath.Join(path, "alemon.config.yaml"))
	if err != nil && !os.IsNotExist(err) {
		return PackageConfig{}, fmt.Errorf("无法读取机器人运行配置：%w", err)
	}
	return PackageConfig{Package: manifest.Name, Namespace: namespace, Fields: manifest.Alemonjs.Config, Values: readConfigValues(string(content), namespace)}, nil
}

func (m Manager) SavePackageConfig(root, name string, values map[string]string) (Result, error) {
	definition, err := m.PackageConfig(root, name)
	if err != nil {
		return Result{}, err
	}
	allowed := make(map[string]PackageConfigField, len(definition.Fields))
	for _, field := range definition.Fields {
		allowed[field.Name] = field
	}
	for key := range values {
		if _, ok := allowed[key]; !ok {
			return Result{}, errors.New("配置项不属于该包")
		}
	}
	current, err := m.Read(root, "alemon.config.yaml")
	if err != nil && !strings.Contains(err.Error(), "no such file") {
		return Result{}, err
	}
	content := ""
	if err == nil {
		content = current.Output
	}
	updated := mergeConfigValues(content, definition.Namespace, definition.Fields, values)
	return m.Write(root, "alemon.config.yaml", updated)
}

func readConfigValues(content, namespace string) map[string]string {
	values := map[string]string{}
	lines := strings.Split(content, "\n")
	section := false
	for _, line := range lines {
		if strings.TrimSpace(line) == yamlKey(namespace)+":" {
			section = true
			continue
		}
		if section && len(line) > 0 && line[0] != ' ' && line[0] != '\t' {
			break
		}
		if !section {
			continue
		}
		parts := strings.SplitN(strings.TrimSpace(line), ":", 2)
		if len(parts) != 2 || !yamlNamePattern.MatchString(parts[0]) {
			continue
		}
		values[parts[0]] = strings.Trim(strings.TrimSpace(parts[1]), "\"'")
	}
	return values
}

func mergeConfigValues(content, namespace string, fields []PackageConfigField, values map[string]string) string {
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		lines = nil
	}
	start, end := -1, len(lines)
	for index, line := range lines {
		if strings.TrimSpace(line) == yamlKey(namespace)+":" {
			start = index
			continue
		}
		if start >= 0 && len(line) > 0 && line[0] != ' ' && line[0] != '\t' {
			end = index
			break
		}
	}
	section := []string{}
	if start >= 0 {
		section = append(section, lines[start+1:end]...)
	}
	for _, field := range fields {
		value, ok := values[field.Name]
		if !ok {
			continue
		}
		replacement := "  " + field.Name + ": " + yamlValue(field.Type, value)
		updated := false
		for index, line := range section {
			if strings.HasPrefix(strings.TrimSpace(line), field.Name+":") {
				section[index] = replacement
				updated = true
				break
			}
		}
		if !updated {
			section = append(section, replacement)
		}
	}
	result := make([]string, 0, len(lines)+len(section)+1)
	if start < 0 {
		result = append(result, lines...)
		if len(result) > 0 {
			result = append(result, "")
		}
		result = append(result, yamlKey(namespace)+":")
		result = append(result, section...)
	} else {
		result = append(result, lines[:start]...)
		result = append(result, yamlKey(namespace)+":")
		result = append(result, section...)
		result = append(result, lines[end:]...)
	}
	return strings.Join(result, "\n") + "\n"
}

func yamlKey(value string) string {
	if yamlNamePattern.MatchString(value) {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func yamlValue(kind, value string) string {
	switch kind {
	case "number", "integer":
		if _, err := strconv.ParseFloat(value, 64); err == nil {
			return value
		}
	case "boolean", "bool":
		if value == "true" || value == "false" {
			return value
		}
	}
	return strconv.Quote(value)
}
