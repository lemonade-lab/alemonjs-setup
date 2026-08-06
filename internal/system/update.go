package system

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"debug/elf"
	"debug/macho"
	"debug/pe"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const maxUpdateSize int64 = 200 << 20

type PendingUpdate struct {
	AssetName string `json:"assetName"`
	SHA256    string `json:"sha256"`
	Version   string `json:"version"`
}

// ReplaceExecutable downloads a release asset and atomically replaces the
// running program on systems that allow it. It only accepts a concrete asset
// URL chosen by the version and platform matcher.
func ReplaceExecutable(downloadURL, assetName, checksum, version string) (string, error) {
	downloaded, err := DownloadUpdate(downloadURL, assetName, checksum, version)
	if err != nil {
		return "", err
	}
	return ReplaceExecutableFile(downloaded)
}

// DownloadUpdate verifies a Release archive's SHA-256 before retaining it in
// the user's cache, so it can be applied later without another network call.
func DownloadUpdate(downloadURL, assetName, checksum, version string) (string, error) {
	if downloadURL == "" || len(checksum) != 64 {
		return "", errors.New("没有可用的匹配安装包")
	}
	path, exists, err := CachedUpdate(assetName, checksum)
	if err != nil {
		return "", err
	}
	if exists {
		if err := savePendingUpdate(PendingUpdate{AssetName: assetName, SHA256: checksum, Version: version}); err != nil {
			return "", err
		}
		return path, nil
	}
	response, err := (&http.Client{Timeout: 90 * time.Second}).Get(downloadURL)
	if err != nil {
		return "", fmt.Errorf("下载更新失败：%w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("下载更新失败：服务器返回 %s", response.Status)
	}
	partial := path + ".part"
	file, err := os.OpenFile(partial, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return "", err
	}
	_, copyErr := io.Copy(file, io.LimitReader(response.Body, maxUpdateSize+1))
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(partial)
		if copyErr != nil {
			return "", copyErr
		}
		return "", closeErr
	}
	if err := os.Rename(partial, path); err != nil {
		return "", err
	}
	if _, ready, err := CachedUpdate(assetName, checksum); err != nil || !ready {
		_ = os.Remove(path)
		if err != nil {
			return "", err
		}
		return "", errors.New("更新包校验失败")
	}
	if err := savePendingUpdate(PendingUpdate{AssetName: assetName, SHA256: checksum, Version: version}); err != nil {
		return "", err
	}
	return path, nil
}

// CachedUpdate returns the persistent location for one release asset and
// whether a complete file is already available there.
func CachedUpdate(assetName, checksum string) (string, bool, error) {
	assetName = filepath.Base(assetName)
	if assetName == "." || assetName == "" || assetName == string(filepath.Separator) {
		return "", false, errors.New("更新包名称无效")
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", false, fmt.Errorf("无法定位应用存储目录：%w", err)
	}
	directory := filepath.Join(base, "alemonjs", "alx", "updates")
	if err := os.MkdirAll(directory, 0700); err != nil {
		return "", false, fmt.Errorf("无法创建更新存储目录：%w", err)
	}
	path := filepath.Join(directory, assetName)
	info, err := os.Stat(path)
	if err == nil && info.Mode().IsRegular() && info.Size() > 0 && info.Size() <= maxUpdateSize && checksumFile(path, checksum) {
		return path, true, nil
	}
	if err != nil && !os.IsNotExist(err) {
		return "", false, err
	}
	return path, false, nil
}

func checksumFile(path, expected string) bool {
	input, err := os.Open(path)
	if err != nil {
		return false
	}
	defer input.Close()
	digest := sha256.New()
	_, err = io.Copy(digest, io.LimitReader(input, maxUpdateSize+1))
	return err == nil && fmt.Sprintf("%x", digest.Sum(nil)) == strings.ToLower(expected)
}

func pendingUpdatePath() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("无法定位应用存储目录：%w", err)
	}
	directory := filepath.Join(base, "alemonjs", "alx", "updates")
	if err := os.MkdirAll(directory, 0700); err != nil {
		return "", err
	}
	return filepath.Join(directory, "pending.json"), nil
}

func savePendingUpdate(update PendingUpdate) error {
	path, err := pendingUpdatePath()
	if err != nil {
		return err
	}
	body, err := json.Marshal(update)
	if err != nil {
		return err
	}
	return os.WriteFile(path, body, 0600)
}

// ReadyPendingUpdate verifies and returns the package selected when the user
// downloaded it. Applying it therefore does not need another GitHub request.
func ReadyPendingUpdate() (PendingUpdate, string, bool, error) {
	path, err := pendingUpdatePath()
	if err != nil {
		return PendingUpdate{}, "", false, err
	}
	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return PendingUpdate{}, "", false, nil
	}
	if err != nil {
		return PendingUpdate{}, "", false, err
	}
	var update PendingUpdate
	if err := json.Unmarshal(body, &update); err != nil || update.AssetName == "" || len(update.SHA256) != 64 {
		return PendingUpdate{}, "", false, errors.New("缓存的更新元数据无效")
	}
	archive, ready, err := CachedUpdate(update.AssetName, update.SHA256)
	return update, archive, ready, err
}

func ClearPendingUpdate() error {
	path, err := pendingUpdatePath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ReplaceExecutableFile applies a user-selected local release archive. The
// caller must obtain explicit confirmation before passing local files here.
func ReplaceExecutableFile(source string) (string, error) {
	temporary, err := os.MkdirTemp("", "alx-update-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(temporary)
	binary, err := releaseBinary(source, temporary)
	if err != nil {
		return "", err
	}
	if err := verifyBinaryPlatform(binary); err != nil {
		return "", err
	}
	return replaceExecutable(binary, source, temporary)
}

func replaceExecutable(binary, archive, temporary string) (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("无法定位当前 alx：%w", err)
	}
	if resolved, err := filepath.EvalSymlinks(executable); err == nil {
		executable = resolved
	}
	pluginUpdates, pluginErr := updateBundledPluginExecutors(archive, filepath.Dir(executable))
	if runtime.GOOS == "windows" {
		next := executable + ".new.exe"
		if err := copyExecutable(binary, next); err != nil {
			return "", err
		}
		message := "新版已准备就绪；应用退出后会自动替换并重启。"
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
		return "", fmt.Errorf("无法替换当前 alx：%w", err)
	}
	message := "已更新 alx：" + executable + "。旧版本备份为 " + backup + "；请重新执行命令，后台服务会在下次重启后使用新版本。"
	return updateMessage(message, pluginUpdates, pluginErr), nil
}

// ScheduleRestart starts a short-lived helper which relaunches alx after the
// HTTP response has been written and the current process exits. On Windows it
// also promotes the .new.exe file created while the old executable was locked.
func ScheduleRestart() error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("无法定位当前 alx：%w", err)
	}
	if resolved, err := filepath.EvalSymlinks(executable); err == nil {
		executable = resolved
	}
	args := os.Args[1:]
	if runtime.GOOS == "windows" {
		replacement := executable + ".new.exe"
		if _, err := os.Stat(replacement); err == nil {
			backup := executable + ".previous-" + time.Now().Format("20060102150405") + ".exe"
			return exec.Command("powershell.exe", "-NoProfile", "-WindowStyle", "Hidden", "-Command", windowsRestartScript(executable, replacement, backup, args)).Start()
		}
	}
	command := append([]string{"-c", "sleep 0.8; exec \"$@\"", "alx-restart", executable}, args...)
	return exec.Command("/bin/sh", command...).Start()
}

// windowsRestartScript is deliberately self-contained: PowerShell's $args is
// unreliable when -Command is followed by an empty application argument list.
// The helper retries until the exiting process releases the executable, keeps a
// rollback copy, and records failures next to the staged binary for diagnosis.
func windowsRestartScript(executable, replacement, backup string, args []string) string {
	quotedArgs := make([]string, len(args))
	for index, argument := range args {
		quotedArgs[index] = powershellQuote(argument)
	}
	arguments := "@(" + strings.Join(quotedArgs, ",") + ")"
	return strings.Join([]string{
		"$target=" + powershellQuote(executable),
		"$replacement=" + powershellQuote(replacement),
		"$backup=" + powershellQuote(backup),
		"$failure=" + powershellQuote(replacement+".failure.txt"),
		"$arguments=" + arguments,
		"Start-Sleep -Milliseconds 500",
		"$restarted=$false",
		"for($attempt=0; $attempt -lt 150; $attempt++){ try { if(Test-Path -LiteralPath $target){ Move-Item -LiteralPath $target -Destination $backup -Force -ErrorAction Stop }; Move-Item -LiteralPath $replacement -Destination $target -Force -ErrorAction Stop; Start-Process -FilePath $target -ArgumentList $arguments; $restarted=$true; break } catch { if(!(Test-Path -LiteralPath $target) -and (Test-Path -LiteralPath $backup)){ Move-Item -LiteralPath $backup -Destination $target -Force -ErrorAction SilentlyContinue }; Start-Sleep -Milliseconds 200 } }",
		"if(!$restarted){ [System.IO.File]::WriteAllText($failure, 'Automatic update could not replace or restart alx. The previous executable was restored when possible.') }",
	}, "; ")
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
	return name == "alx" || name == "alx.exe" || strings.HasPrefix(name, "alx-") || strings.HasPrefix(name, "alemonx")
}

func verifyBinaryPlatform(path string) error {
	switch runtime.GOOS {
	case "windows":
		file, err := pe.Open(path)
		if err != nil {
			return errors.New("更新包中的程序不是 Windows 可执行文件")
		}
		defer file.Close()
		if runtime.GOARCH == "amd64" && file.FileHeader.Machine != pe.IMAGE_FILE_MACHINE_AMD64 {
			return errors.New("更新包与当前 Windows 架构不匹配")
		}
	case "darwin":
		file, err := macho.Open(path)
		if err != nil {
			return errors.New("更新包中的程序不是 macOS 可执行文件")
		}
		defer file.Close()
		if runtime.GOARCH == "arm64" && file.Cpu != macho.CpuArm64 || runtime.GOARCH == "amd64" && file.Cpu != macho.CpuAmd64 {
			return errors.New("更新包与当前 macOS 架构不匹配")
		}
	case "linux":
		file, err := elf.Open(path)
		if err != nil {
			return errors.New("更新包中的程序不是 Linux 可执行文件")
		}
		defer file.Close()
		if runtime.GOARCH == "arm64" && file.Machine != elf.EM_AARCH64 || runtime.GOARCH == "amd64" && file.Machine != elf.EM_X86_64 {
			return errors.New("更新包与当前 Linux 架构不匹配")
		}
	}
	return nil
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
			if entry.UncompressedSize64 > uint64(maxUpdateSize) {
				err = errors.New("更新包解压后超过 200 MB")
			} else {
				var copied int64
				copied, err = io.Copy(out, io.LimitReader(in, maxUpdateSize+1))
				if err == nil && copied > maxUpdateSize {
					err = errors.New("更新包解压后超过 200 MB")
				}
			}
			_ = out.Close()
		}
		_ = in.Close()
		if err != nil {
			return "", err
		}
		return target, nil
	}
	return "", errors.New("安装包中未找到 alx 可执行文件")
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
		if header.Size > maxUpdateSize {
			_ = out.Close()
			return "", errors.New("更新包解压后超过 200 MB")
		}
		copied, copyErr := io.Copy(out, io.LimitReader(reader, maxUpdateSize+1))
		if copyErr == nil && copied > maxUpdateSize {
			copyErr = errors.New("更新包解压后超过 200 MB")
		}
		closeErr := out.Close()
		if copyErr != nil {
			return "", copyErr
		}
		if closeErr != nil {
			return "", closeErr
		}
		return target, nil
	}
	return "", errors.New("安装包中未找到 alx 可执行文件")
}

// updateBundledPluginExecutors only updates an already installed plugin. An
// explicit uninstall therefore remains respected: updating alx never silently
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
		manifest, manifestErr := os.Lstat(filepath.Join(pluginDirectory, "alx.setup.json"))
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
