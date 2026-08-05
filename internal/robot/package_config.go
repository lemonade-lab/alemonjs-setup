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
	data, subject, err := installedPackageManifest(path, name)
	if err != nil {
		return PackageConfig{}, err
	}
	return packageConfigFromManifest(path, data, subject)
}

// installedPackageManifest resolves both dependency packages and packages kept
// in the robot's backpack. Local packages are workspace packages, so looking
// only in node_modules made their declared AlemonJS configuration impossible
// to manage from the backpack page.
func installedPackageManifest(project, name string) ([]byte, string, error) {
	data, err := os.ReadFile(filepath.Join(project, "node_modules", filepath.FromSlash(name), "package.json"))
	if err == nil {
		return data, "该包", nil
	}
	if !os.IsNotExist(err) {
		return nil, "", fmt.Errorf("无法读取包配置声明：%w", err)
	}
	entries, readErr := os.ReadDir(filepath.Join(project, "packages"))
	if readErr != nil && !os.IsNotExist(readErr) {
		return nil, "", fmt.Errorf("无法读取背包：%w", readErr)
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		candidate, candidateErr := os.ReadFile(filepath.Join(project, "packages", entry.Name(), "package.json"))
		if candidateErr != nil {
			continue
		}
		var manifest struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(candidate, &manifest) == nil && manifest.Name == name {
			return candidate, "背包中的本地插件包", nil
		}
	}
	return nil, "", errors.New("该包尚未安装到依赖或背包中")
}

// CurrentPackageConfig reads the robot project's own package.json. It is not a
// node_modules lookup: a project can expose its own alemonjs.config extension
// and should configure it from the main robot configuration screen.
func (m Manager) CurrentPackageConfig(root string) (PackageConfig, error) {
	path, err := projectPath(root)
	if err != nil {
		return PackageConfig{}, err
	}
	data, err := os.ReadFile(filepath.Join(path, "package.json"))
	if err != nil {
		return PackageConfig{}, fmt.Errorf("无法读取当前项目的 package.json：%w", err)
	}
	return packageConfigFromManifest(path, data, "当前项目")
}

func packageConfigFromManifest(path string, data []byte, subject string) (PackageConfig, error) {
	var manifest packageConfigManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return PackageConfig{}, errors.New(subject + "的 package.json 无法识别")
	}
	if manifest.Name == "" || len(manifest.Alemonjs.Config) == 0 {
		return PackageConfig{}, errors.New(subject + "没有声明 alemonjs.config")
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
	return m.savePackageConfigDefinition(root, definition, values)
}

func (m Manager) savePackageConfigDefinition(root string, definition PackageConfig, values map[string]string) (Result, error) {
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

func (m Manager) SaveCurrentPackageConfig(root string, values map[string]string) (Result, error) {
	definition, err := m.CurrentPackageConfig(root)
	if err != nil {
		return Result{}, err
	}
	return m.savePackageConfigDefinition(root, definition, values)
}

// SaveLogin only changes login after the selected connection's declared
// required fields already have values. This keeps a package from being made
// active in alemon.config.yaml with an unusable, half-filled configuration.
func (m Manager) SaveLogin(root, login, packageName string) (Result, error) {
	login = strings.TrimSpace(login)
	if login == "" || strings.ContainsAny(login, "\r\n") {
		return Result{}, errors.New("请填写有效的登录连接")
	}
	if packageName != "" {
		definition, err := m.PackageConfig(root, packageName)
		if err != nil {
			// A custom npm package may not be an AlemonJS connection package. It
			// has no declared fields to validate, so saving its custom login value
			// remains valid. Installation/read failures still must be surfaced.
			if !strings.Contains(err.Error(), "没有声明 alemonjs.config") {
				return Result{}, err
			}
		} else {
			missing := make([]string, 0)
			for _, field := range definition.Fields {
				if field.Required && strings.TrimSpace(definition.Values[field.Name]) == "" {
					label := field.Description
					if label == "" {
						label = field.Name
					}
					missing = append(missing, label)
				}
			}
			if len(missing) > 0 {
				return Result{}, fmt.Errorf("请先完成 %s 的必填配置：%s", definition.Package, strings.Join(missing, "、"))
			}
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
	value := "login: '" + strings.ReplaceAll(login, "'", "''") + "'"
	if regexp.MustCompile(`(?m)^login:\s*.*$`).MatchString(content) {
		content = regexp.MustCompile(`(?m)^login:\s*.*$`).ReplaceAllString(content, value)
	} else {
		content = strings.TrimRight(content, "\n")
		if content != "" {
			content += "\n"
		}
		content += value + "\n"
	}
	result, err := m.Write(root, "alemon.config.yaml", content)
	if err != nil {
		return Result{}, err
	}
	result.Output = "登录连接已保存。"
	return result, nil
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
