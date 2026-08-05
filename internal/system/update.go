package system

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// ReplaceExecutable downloads a release asset and atomically replaces the
// running program on systems that allow it. It only accepts a concrete asset
// URL chosen by the version and platform matcher.
func ReplaceExecutable(downloadURL, assetName string) (string, error) {
	if downloadURL == "" {
		return "", errors.New("没有可用的匹配安装包")
	}
	response, err := (&http.Client{Timeout: 90 * time.Second}).Get(downloadURL)
	if err != nil {
		return "", fmt.Errorf("下载更新失败：%w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("下载更新失败：服务器返回 %s", response.Status)
	}
	temporary, err := os.MkdirTemp("", "albs-update-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(temporary)
	downloaded := filepath.Join(temporary, assetName)
	file, err := os.OpenFile(downloaded, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0700)
	if err != nil {
		return "", err
	}
	_, copyErr := io.Copy(file, io.LimitReader(response.Body, 200<<20))
	closeErr := file.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	binary, err := releaseBinary(downloaded, temporary)
	if err != nil {
		return "", err
	}
	executable, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("无法定位当前 albs：%w", err)
	}
	if resolved, err := filepath.EvalSymlinks(executable); err == nil {
		executable = resolved
	}
	pluginUpdates, pluginErr := updateBundledPluginExecutors(downloaded, filepath.Dir(executable))
	if runtime.GOOS == "windows" {
		next := executable + ".new.exe"
		if err := copyExecutable(binary, next); err != nil {
			return "", err
		}
		message := "新版已下载到 " + next + "。请退出 albs 后用它替换当前文件。"
		return updateMessage(message, pluginUpdates, pluginErr), nil
	}
	next := executable + ".new"
	if err := copyExecutable(binary, next); err != nil {
		return "", err
	}
	backup := executable + ".previous-" + time.Now().Format("20060102150405")
	if err := copyExecutable(executable, backup); err != nil {
		_ = os.Remove(next)
		return "", fmt.Errorf("无法保存旧版本备份：%w", err)
	}
	if err := os.Rename(next, executable); err != nil {
		_ = os.Remove(next)
		return "", fmt.Errorf("无法替换当前 albs：%w", err)
	}
	message := "已更新 albs：" + executable + "。旧版本备份为 " + backup + "；请重新执行命令，后台服务会在下次重启后使用新版本。"
	return updateMessage(message, pluginUpdates, pluginErr), nil
}

func updateMessage(message string, pluginUpdates int, pluginErr error) string {
	if pluginUpdates > 0 {
		message += fmt.Sprintf(" 已同步 %d 个已安装插件执行器。", pluginUpdates)
	}
	if pluginErr != nil {
		message += " 插件执行器未同步：" + pluginErr.Error()
	}
	return message
}

func copyExecutable(source, target string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}
func releaseBinary(source, directory string) (string, error) {
	lower := strings.ToLower(source)
	if strings.HasSuffix(lower, ".zip") {
		return unzipBinary(source, directory)
	}
	if strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz") {
		return untarBinary(source, directory)
	}
	return source, nil
}
func isBinaryName(name string) bool {
	name = strings.ToLower(filepath.Base(name))
	return name == "albs" || name == "albs.exe" || strings.HasPrefix(name, "albs-") || strings.HasPrefix(name, "alemonjs-setup")
}
func unzipBinary(source, directory string) (string, error) {
	archive, err := zip.OpenReader(source)
	if err != nil {
		return "", err
	}
	defer archive.Close()
	for _, entry := range archive.File {
		if entry.FileInfo().IsDir() || !isBinaryName(entry.Name) {
			continue
		}
		in, err := entry.Open()
		if err != nil {
			return "", err
		}
		target := filepath.Join(directory, "update-binary")
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
		if err == nil {
			_, err = io.Copy(out, in)
			_ = out.Close()
		}
		_ = in.Close()
		if err != nil {
			return "", err
		}
		return target, nil
	}
	return "", errors.New("安装包中未找到 albs 可执行文件")
}
func untarBinary(source, directory string) (string, error) {
	input, err := os.Open(source)
	if err != nil {
		return "", err
	}
	defer input.Close()
	gzipReader, err := gzip.NewReader(input)
	if err != nil {
		return "", err
	}
	defer gzipReader.Close()
	reader := tar.NewReader(gzipReader)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		if header.FileInfo().IsDir() || !isBinaryName(header.Name) {
			continue
		}
		target := filepath.Join(directory, "update-binary")
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
		if err != nil {
			return "", err
		}
		_, copyErr := io.Copy(out, reader)
		closeErr := out.Close()
		if copyErr != nil {
			return "", copyErr
		}
		if closeErr != nil {
			return "", closeErr
		}
		return target, nil
	}
	return "", errors.New("安装包中未找到 albs 可执行文件")
}

// updateBundledPluginExecutors only updates an already installed plugin. An
// explicit uninstall therefore remains respected: updating albs never silently
// recreates a plugin directory the user removed.
func updateBundledPluginExecutors(source, executableDirectory string) (int, error) {
	if !strings.HasSuffix(strings.ToLower(source), ".zip") {
		return 0, nil
	}
	archive, err := zip.OpenReader(source)
	if err != nil {
		return 0, err
	}
	defer archive.Close()
	updated := 0
	for _, entry := range archive.File {
		parts := strings.Split(filepath.ToSlash(entry.Name), "/")
		if len(parts) != 4 || parts[0] != "plugins" || parts[2] != "dist" || parts[1] == "" || parts[3] == "" || entry.FileInfo().IsDir() {
			continue
		}
		if strings.Contains(parts[1], ".") || strings.Contains(parts[3], "/") {
			continue
		}
		pluginDirectory := filepath.Join(executableDirectory, "plugins", parts[1])
		manifest, manifestErr := os.Lstat(filepath.Join(pluginDirectory, "albs.setup.json"))
		if manifestErr != nil || !manifest.Mode().IsRegular() || manifest.Mode()&os.ModeSymlink != 0 {
			continue
		}
		targetDirectory := filepath.Join(pluginDirectory, "dist")
		if err := os.MkdirAll(targetDirectory, 0755); err != nil {
			return updated, err
		}
		input, err := entry.Open()
		if err != nil {
			return updated, err
		}
		temporary := filepath.Join(targetDirectory, "."+parts[3]+".new")
		output, err := os.OpenFile(temporary, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
		if err == nil {
			_, err = io.Copy(output, io.LimitReader(input, 100<<20))
			closeErr := output.Close()
			if err == nil {
				err = closeErr
			}
		}
		_ = input.Close()
		if err != nil {
			_ = os.Remove(temporary)
			return updated, err
		}
		if err := os.Rename(temporary, filepath.Join(targetDirectory, parts[3])); err != nil {
			_ = os.Remove(temporary)
			return updated, err
		}
		updated++
	}
	return updated, nil
}
