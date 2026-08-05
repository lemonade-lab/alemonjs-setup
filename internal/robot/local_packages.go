package robot

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func installLocalPackage(root, source string) (Result, error) {
	if err := ensurePackagesWorkspace(root); err != nil {
		return Result{}, err
	}
	directory := filepath.Join(root, "packages")
	if err := os.MkdirAll(directory, 0755); err != nil {
		return Result{}, fmt.Errorf("无法创建 packages 目录：%w", err)
	}
	name := localPackageName(source)
	target := filepath.Join(directory, name)
	if _, err := os.Stat(target); err == nil {
		return Result{}, fmt.Errorf("本地插件包 %s 已存在，工具不会覆盖它", name)
	}
	if strings.HasPrefix(source, "git+") {
		output, err := run(root, "git", "clone", "--depth", "1", strings.TrimPrefix(source, "git+"), target)
		if err != nil {
			return Result{Path: target, Output: output}, fmt.Errorf("下载本地插件包失败：%w", err)
		}
		return Result{Path: target, Output: "已安装到 packages/" + name + "。\n" + output}, nil
	}
	temporary, err := os.MkdirTemp("", "albs-package-")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(temporary)
	output, err := run(temporary, "npm", "pack", source, "--json")
	if err != nil {
		return Result{Path: target, Output: output}, fmt.Errorf("下载 npm 插件包失败：%w", err)
	}
	archive, err := packedFilename(output)
	if err != nil {
		return Result{}, err
	}
	extracted := filepath.Join(temporary, "extracted")
	if err := os.MkdirAll(extracted, 0755); err != nil {
		return Result{}, err
	}
	if output, err = run(temporary, "tar", "-xzf", filepath.Join(temporary, archive), "-C", extracted); err != nil {
		return Result{Path: target, Output: output}, fmt.Errorf("解压 npm 插件包失败：%w", err)
	}
	if err := copyPath(filepath.Join(extracted, "package"), target); err != nil {
		return Result{}, fmt.Errorf("写入本地插件包失败：%w", err)
	}
	return Result{Path: target, Output: "已安装到 packages/" + name + "。"}, nil
}

func ensurePackagesWorkspace(root string) error {
	manifest := filepath.Join(root, "package.json")
	data, err := os.ReadFile(manifest)
	if err != nil {
		return fmt.Errorf("无法读取 package.json：%w", err)
	}
	var values map[string]any
	if err := json.Unmarshal(data, &values); err != nil {
		return errors.New("package.json 格式无法识别")
	}
	if workspaces, ok := values["workspaces"].([]any); ok {
		for _, item := range workspaces {
			if item == "packages/*" {
				return nil
			}
		}
		values["workspaces"] = append(workspaces, "packages/*")
	} else if _, ok := values["workspaces"]; !ok {
		values["private"] = true
		values["workspaces"] = []string{"packages/*"}
	} else {
		return errors.New("package.json 的 workspaces 格式暂不支持自动添加 packages/*")
	}
	updated, err := json.MarshalIndent(values, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(manifest, append(updated, '\n'), 0644); err != nil {
		return fmt.Errorf("无法写入 packages 工作区配置：%w", err)
	}
	return nil
}

func removeLocalPackage(root, source string) (Result, error) {
	name := localPackageName(source)
	target := filepath.Join(root, "packages", name)
	info, err := os.Stat(target)
	if os.IsNotExist(err) {
		return Result{}, fmt.Errorf("背包中没有 %s", name)
	}
	if err != nil || !info.IsDir() {
		return Result{}, errors.New("本地插件包目录无法访问")
	}
	if err := os.RemoveAll(target); err != nil {
		return Result{}, fmt.Errorf("移除本地插件包失败：%w", err)
	}
	return Result{Path: target, Output: "已从 packages 移除 " + name + "。"}, nil
}

func localPackageName(source string) string {
	value := strings.TrimPrefix(source, "git+")
	value = strings.TrimSuffix(value, ".git")
	if index := strings.LastIndex(value, "@"); index > strings.LastIndex(value, "/") {
		value = value[:index]
	}
	value = filepath.Base(value)
	value = strings.TrimPrefix(value, "@")
	value = strings.ReplaceAll(value, "/", "-")
	value = strings.ReplaceAll(value, "@", "-")
	return value
}

func packedFilename(output string) (string, error) {
	var entries []struct {
		Filename string `json:"filename"`
	}
	if err := json.Unmarshal([]byte(output), &entries); err != nil || len(entries) == 0 || entries[0].Filename == "" {
		return "", errors.New("npm 插件包文件名无效")
	}
	return entries[0].Filename, nil
}
