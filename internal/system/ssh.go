package system

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type SSHPublicKey struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func SSHKeys() ([]SSHPublicKey, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("无法定位用户目录：%w", err)
	}
	directory := filepath.Join(home, ".ssh")
	entries, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		return []SSHPublicKey{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("无法读取 SSH 目录：%w", err)
	}
	keys := []SSHPublicKey{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".pub") {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || info.Size() > 16<<10 {
			continue
		}
		data, err := os.ReadFile(filepath.Join(directory, entry.Name()))
		value := strings.TrimSpace(string(data))
		if err == nil && (strings.HasPrefix(value, "ssh-") || strings.HasPrefix(value, "ecdsa-")) {
			keys = append(keys, SSHPublicKey{Name: entry.Name(), Value: value})
		}
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].Name < keys[j].Name })
	return keys, nil
}

// GenerateSSHKey only creates the standard Ed25519 identity when no public
// key exists. Private key material is never read or returned by Setup.
func GenerateSSHKey() (SSHPublicKey, error) {
	keys, err := SSHKeys()
	if err != nil {
		return SSHPublicKey{}, err
	}
	if len(keys) > 0 {
		return SSHPublicKey{}, errors.New("已存在 SSH 公钥，无需重复生成")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return SSHPublicKey{}, err
	}
	directory := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(directory, 0700); err != nil {
		return SSHPublicKey{}, fmt.Errorf("无法创建 SSH 目录：%w", err)
	}
	target := filepath.Join(directory, "id_ed25519")
	if _, err := os.Stat(target); err == nil {
		return SSHPublicKey{}, errors.New("已存在 SSH 私钥，但未找到对应公钥；不会覆盖")
	}
	output, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-f", target, "-N", "", "-C", "alx").CombinedOutput()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return SSHPublicKey{}, errors.New("未检测到 ssh-keygen。请先在左上角“环境”中安装 Git/SSH 工具后重新生成密钥")
		}
		if os.IsPermission(err) || strings.Contains(strings.ToLower(string(output)), "permission denied") {
			return SSHPublicKey{}, errors.New("没有权限创建 SSH 密钥。请在系统设置中为 alx 授予用户目录访问权限后重试")
		}
		return SSHPublicKey{}, fmt.Errorf("生成 SSH 密钥失败：%s", strings.TrimSpace(string(output)))
	}
	keys, err = SSHKeys()
	if err != nil || len(keys) == 0 {
		return SSHPublicKey{}, errors.New("SSH 密钥已生成，但无法读取公钥")
	}
	return keys[0], nil
}
