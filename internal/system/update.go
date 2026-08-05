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
	if runtime.GOOS == "windows" {
		next := executable + ".new.exe"
		if err := copyExecutable(binary, next); err != nil {
			return "", err
		}
		return "新版已下载到 " + next + "。请退出 albs 后用它替换当前文件。", nil
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
	return "已更新 albs：" + executable + "。旧版本备份为 " + backup + "；请重新执行命令，后台服务会在下次重启后使用新版本。", nil
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
