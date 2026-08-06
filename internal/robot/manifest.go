package robot

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type PackageManifest struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Homepage    string `json:"homepage"`
	Repository  string `json:"repository"`
	License     string `json:"license"`
	Private     bool   `json:"private"`
	Access      string `json:"access"`
}

var npmNamePattern = regexp.MustCompile(`^(?:@[a-z0-9][a-z0-9._-]*/)?[a-z0-9][a-z0-9._-]*$`)

func (Manager) PackageManifest(root string) (PackageManifest, error) {
	path, err := projectPath(root)
	if err != nil {
		return PackageManifest{}, err
	}
	data, err := os.ReadFile(filepath.Join(path, "package.json"))
	if err != nil {
		return PackageManifest{}, fmt.Errorf("无法读取 package.json：%w", err)
	}
	var source map[string]json.RawMessage
	if err := json.Unmarshal(data, &source); err != nil {
		return PackageManifest{}, errors.New("package.json 格式无法识别")
	}
	result := PackageManifest{}
	readString := func(key string) string { var value string; _ = json.Unmarshal(source[key], &value); return value }
	result.Name, result.Version, result.Description, result.Homepage, result.License = readString("name"), readString("version"), readString("description"), readString("homepage"), readString("license")
	_ = json.Unmarshal(source["private"], &result.Private)
	if raw := source["repository"]; len(raw) > 0 {
		if json.Unmarshal(raw, &result.Repository) != nil {
			var repository struct {
				URL string `json:"url"`
			}
			if json.Unmarshal(raw, &repository) == nil {
				result.Repository = repository.URL
			}
		}
	}
	if raw := source["publishConfig"]; len(raw) > 0 {
		var config struct {
			Access string `json:"access"`
		}
		if json.Unmarshal(raw, &config) == nil {
			result.Access = config.Access
		}
	}
	return result, nil
}

func (Manager) SavePackageManifest(root string, input PackageManifest) (Result, error) {
	path, err := projectPath(root)
	if err != nil {
		return Result{}, err
	}
	if err := validateManifest(input); err != nil {
		return Result{}, err
	}
	file := filepath.Join(path, "package.json")
	data, err := os.ReadFile(file)
	if err != nil {
		return Result{}, fmt.Errorf("无法读取 package.json：%w", err)
	}
	var source map[string]any
	if err := json.Unmarshal(data, &source); err != nil {
		return Result{}, errors.New("package.json 格式无法识别")
	}
	source["name"], source["version"], source["description"], source["homepage"], source["license"], source["private"] = input.Name, input.Version, input.Description, input.Homepage, input.License, input.Private
	if input.Repository == "" {
		delete(source, "repository")
	} else {
		source["repository"] = input.Repository
	}
	if input.Access == "" {
		if config, ok := source["publishConfig"].(map[string]any); ok {
			delete(config, "access")
			if len(config) == 0 {
				delete(source, "publishConfig")
			}
		}
	} else {
		config, _ := source["publishConfig"].(map[string]any)
		if config == nil {
			config = map[string]any{}
		}
		config["access"] = input.Access
		source["publishConfig"] = config
	}
	updated, err := json.MarshalIndent(source, "", "  ")
	if err != nil {
		return Result{}, err
	}
	if err := os.WriteFile(file, append(updated, '\n'), 0644); err != nil {
		if permissionError(err) {
			return Result{}, permissionAdvice("保存 package.json")
		}
		return Result{}, fmt.Errorf("无法保存 package.json：%w", err)
	}
	return Result{Path: file, Output: "发布信息已保存。"}, nil
}

func validateManifest(input PackageManifest) error {
	if !npmNamePattern.MatchString(input.Name) {
		return errors.New("包名只能使用小写字母、数字、短横线、下划线和一个可选的 @scope/")
	}
	if !isNpmVersion(input.Version) {
		return errors.New("版本号应为 1.2.3 格式")
	}
	if len(input.Description) > 512 || strings.ContainsAny(input.Description, "\r\n") {
		return errors.New("包描述应为单行且不超过 512 个字符")
	}
	if input.License != "" && (len(input.License) > 100 || strings.ContainsAny(input.License, "\r\n")) {
		return errors.New("许可证格式无效")
	}
	for _, value := range []string{input.Homepage, input.Repository} {
		if value == "" {
			continue
		}
		parsed, err := url.Parse(value)
		if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
			return errors.New("主页和仓库地址必须是完整的 http(s) 地址")
		}
	}
	if input.Access != "" && input.Access != "public" && input.Access != "restricted" {
		return errors.New("发布权限只能选择 public 或 restricted")
	}
	return nil
}
