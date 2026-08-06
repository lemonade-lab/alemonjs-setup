package robot

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func installLocalPackage(root, source string) (Result, error) {
	if err := ensurePackagesWorkspace(root); err != nil {
		return Result{}, err
	}
	directory := filepath.Join(root, "packages")
	if err := os.MkdirAll(directory, 0755); err != nil {
		if permissionError(err) {
			return Result{}, permissionAdvice("创建 packages 目录")
		}
		return Result{}, fmt.Errorf("无法创建 packages 目录：%w", err)
	}
	name := localPackageName(source)
	target := filepath.Join(directory, name)
	if _, err := os.Stat(target); err == nil {
		return Result{}, fmt.Errorf("本地插件包 %s 已存在，工具不会覆盖它", name)
	}
	if strings.HasPrefix(source, "git+") {
		repository, ref, err := splitGitPackageSource(source)
		if err != nil {
			return Result{}, err
		}
		args := []string{"clone", "--depth", "1"}
		if ref != "" {
			args = append(args, "--branch", ref)
		}
		args = append(args, repository, target)
		output, err := run(root, "git", args...)
		if err != nil {
			return Result{Path: target, Output: output}, fmt.Errorf("下载本地插件包失败：%w", err)
		}
		version := ""
		if ref != "" {
			version = "（" + ref + "）"
		}
		return Result{Path: target, Output: "已安装到 packages/" + name + version + "。\n" + output}, nil
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

// Connection packages are normal project dependencies. They must not be
// unpacked into packages/: Yarn owns node_modules and package.json so the
// selected adapter can participate in the robot runtime.
func installConnectionPackage(root, source string) (Result, error) {
	output, err := runNamedPackageManager(root, "yarn", "add", source)
	if err != nil {
		return Result{Path: root, Output: output}, fmt.Errorf("安装连接包失败：%w", err)
	}
	return Result{Path: root, Output: "已添加连接依赖 " + source + "。\n" + output}, nil
}

func removeConnectionPackage(root, source string) (Result, error) {
	output, err := runNamedPackageManager(root, "yarn", "remove", source)
	if err != nil {
		return Result{Path: root, Output: output}, fmt.Errorf("卸载连接包失败：%w", err)
	}
	return Result{Path: root, Output: "已移除连接依赖 " + source + "。\n" + output}, nil
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
		if permissionError(err) {
			return permissionAdvice("保存 packages 工作区配置")
		}
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

// removeLocalPackageByName removes a package selected from the backpack UI.
// The selected package name is resolved by its package.json rather than being
// used as a filesystem path, so a request cannot escape packages/.
func removeLocalPackageByName(root, packageName string) (Result, error) {
	if !packageNamePattern.MatchString(packageName) {
		return Result{}, errors.New("本地插件包名无效")
	}
	directory := filepath.Join(root, "packages")
	entries, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		return Result{}, fmt.Errorf("背包中没有 %s", packageName)
	}
	if err != nil {
		return Result{}, fmt.Errorf("无法读取背包：%w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		target := filepath.Join(directory, entry.Name())
		data, readErr := os.ReadFile(filepath.Join(target, "package.json"))
		if readErr != nil {
			continue
		}
		var manifest struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(data, &manifest) != nil || manifest.Name != packageName {
			continue
		}
		if err := os.RemoveAll(target); err != nil {
			return Result{}, fmt.Errorf("移除本地插件包失败：%w", err)
		}
		return Result{Path: target, Output: "已从背包移除 " + packageName + "。"}, nil
	}
	return Result{}, fmt.Errorf("背包中没有 %s", packageName)
}

// replaceLocalPackage downloads the selected published npm version before the
// previous directory is discarded. The old directory is kept as a temporary
// sibling and restored if the download or unpacking fails.
func replaceLocalPackage(root, packageName, version string) (Result, error) {
	if !packageNamePattern.MatchString(packageName) {
		return Result{}, errors.New("本地插件包名无效")
	}
	if !regexp.MustCompile(`^v?\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$`).MatchString(version) {
		return Result{}, errors.New("请选择有效的插件版本")
	}
	items, err := (Manager{}).LocalPackages(root)
	if err != nil {
		return Result{}, err
	}
	var previous LocalPackage
	for _, item := range items {
		if item.Name == packageName && item.Valid {
			previous = item
			break
		}
	}
	if previous.Path == "" {
		return Result{}, errors.New("背包中没有可更新的本地插件包")
	}
	backup := previous.Path + ".albs-backup"
	if _, err := os.Stat(backup); err == nil {
		return Result{}, errors.New("检测到未完成的插件更新，请先刷新背包后重试")
	}
	if err := os.Rename(previous.Path, backup); err != nil {
		return Result{}, fmt.Errorf("无法备份当前插件版本：%w", err)
	}
	result, installErr := installLocalPackage(root, packageName+"@"+strings.TrimPrefix(version, "v"))
	if installErr == nil {
		_ = os.RemoveAll(backup)
		return Result{Path: result.Path, Output: "已切换 " + packageName + " 到 v" + strings.TrimPrefix(version, "v") + "。\n" + result.Output}, nil
	}
	_ = os.RemoveAll(result.Path)
	if restoreErr := os.Rename(backup, previous.Path); restoreErr != nil {
		return Result{}, fmt.Errorf("更新失败且无法恢复原插件：%w", restoreErr)
	}
	return result, fmt.Errorf("切换版本失败，已恢复原插件：%w", installErr)
}

func switchLocalPackageVersion(root, packageName, version string) (Result, error) {
	items, err := (Manager{}).LocalPackages(root)
	if err != nil {
		return Result{}, err
	}
	for _, item := range items {
		if item.Name != packageName || !item.Valid {
			continue
		}
		if output, gitErr := gitRun(item.Path, "rev-parse", "--is-inside-work-tree"); gitErr == nil && strings.TrimSpace(output) == "true" {
			if !gitVersionPattern.MatchString(version) {
				return Result{}, errors.New("请选择有效的 Git 版本标签")
			}
			status, statusErr := gitRun(item.Path, "status", "--porcelain")
			if statusErr != nil {
				return Result{}, errors.New("无法确认 Git 工作区状态")
			}
			if strings.TrimSpace(status) != "" {
				return Result{}, errors.New("插件 Git 工作区有未提交修改，请先提交或还原后再切换版本")
			}
			if _, tagErr := gitRun(item.Path, "rev-parse", "-q", "--verify", "refs/tags/"+version); tagErr != nil {
				if _, fetchErr := gitRun(item.Path, "fetch", "origin", "tag", version); fetchErr != nil {
					return Result{}, errors.New("无法从 Git 仓库获取该版本标签")
				}
				if _, tagErr = gitRun(item.Path, "rev-parse", "-q", "--verify", "refs/tags/"+version); tagErr != nil {
					return Result{}, errors.New("Git 仓库中没有该版本标签")
				}
			}
			output, checkoutErr := gitRun(item.Path, "checkout", "--detach", "tags/"+version)
			return Result{Path: item.Path, Output: output}, checkoutErr
		}
		return replaceLocalPackage(root, packageName, version)
	}
	return Result{}, errors.New("背包中没有可切换版本的本地插件包")
}

func localPackageName(source string) string {
	value := strings.TrimPrefix(source, "git+")
	value = strings.SplitN(value, "#", 2)[0]
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

var gitPackageRefPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)

// splitGitPackageSource accepts the npm-style git+URL#tag form. The ref is
// passed as a separate git --branch argument, never interpolated into a shell.
func splitGitPackageSource(source string) (repository, ref string, err error) {
	value := strings.TrimPrefix(strings.TrimSpace(source), "git+")
	parts := strings.SplitN(value, "#", 2)
	repository = strings.TrimSpace(parts[0])
	if repository == "" || !(strings.HasPrefix(repository, "https://") || strings.HasPrefix(repository, "ssh://") || strings.HasPrefix(repository, "git@")) {
		return "", "", errors.New("Git 插件地址无效")
	}
	if len(parts) == 2 {
		ref = strings.TrimSpace(parts[1])
	}
	if ref != "" && (!gitPackageRefPattern.MatchString(ref) || strings.Contains(ref, "..") || strings.HasPrefix(ref, "-")) {
		return "", "", errors.New("插件版本 tag 无效")
	}
	return repository, ref, nil
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
