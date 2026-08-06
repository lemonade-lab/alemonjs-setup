// Package access implements the optional local account protection for alx.
package access

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const sessionDuration = 12 * time.Hour

type config struct {
	Enabled      bool   `json:"enabled"`
	Account      string `json:"account,omitempty"`
	PasswordHash string `json:"passwordHash,omitempty"`
	SessionKey   string `json:"sessionKey,omitempty"`
}

// Status deliberately contains no password material.
type Status struct {
	Enabled       bool   `json:"enabled"`
	Authenticated bool   `json:"authenticated"`
	Account       string `json:"account,omitempty"`
}

type Manager struct {
	path string
	mu   sync.RWMutex
	data config
}

func DefaultPath() (string, error) {
	directory, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("无法定位用户配置目录：%w", err)
	}
	return filepath.Join(directory, "alemonjs", "alx-auth.json"), nil
}

func New() (*Manager, error) {
	path, err := DefaultPath()
	if err != nil {
		return nil, err
	}
	return NewAt(path)
}

func NewAt(path string) (*Manager, error) {
	manager := &Manager{path: path}
	if err := manager.reload(); err != nil {
		return nil, err
	}
	return manager, nil
}

func (m *Manager) reload() error {
	data, err := os.ReadFile(m.path)
	if errors.Is(err, os.ErrNotExist) {
		m.data = config{}
		return nil
	}
	if err != nil {
		return fmt.Errorf("无法读取身份认证配置：%w", err)
	}
	var next config
	if err := json.Unmarshal(data, &next); err != nil {
		return fmt.Errorf("身份认证配置无效：%w", err)
	}
	if next.Enabled && (next.Account == "" || next.PasswordHash == "" || next.SessionKey == "") {
		return errors.New("身份认证配置不完整")
	}
	m.data = next
	return nil
}

func (m *Manager) current() (config, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.reload(); err != nil {
		return config{}, err
	}
	return m.data, nil
}

func (m *Manager) Status(token string) (Status, error) {
	data, err := m.current()
	if err != nil {
		return Status{}, err
	}
	status := Status{Enabled: data.Enabled}
	if data.Enabled && m.validToken(data, token) {
		status.Authenticated, status.Account = true, data.Account
	}
	return status, nil
}

func (m *Manager) Enable(account, password, confirmation string) (string, error) {
	account = strings.TrimSpace(account)
	if account == "" || len(account) > 64 || strings.ContainsAny(account, "\r\n") {
		return "", errors.New("账户需为 1 到 64 个非换行字符")
	}
	if password == "" {
		return "", errors.New("请填写密码")
	}
	if password != confirmation {
		return "", errors.New("两次输入的密码不一致")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.reload(); err != nil {
		return "", err
	}
	if m.data.Enabled {
		return "", errors.New("身份认证已开启；请先使用现有账户登录")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("无法保护密码：%w", err)
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return "", fmt.Errorf("无法生成登录密钥：%w", err)
	}
	m.data = config{Enabled: true, Account: account, PasswordHash: string(hash), SessionKey: base64.RawURLEncoding.EncodeToString(key)}
	if err := m.persist(); err != nil {
		return "", err
	}
	return m.issueToken(m.data)
}

func (m *Manager) Disable() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data = config{}
	return m.persist()
}

func (m *Manager) Login(account, password string) (string, error) {
	data, err := m.current()
	if err != nil {
		return "", err
	}
	if !data.Enabled {
		return "", errors.New("身份认证尚未开启")
	}
	if account != data.Account || bcrypt.CompareHashAndPassword([]byte(data.PasswordHash), []byte(password)) != nil {
		return "", errors.New("账户或密码错误")
	}
	return m.issueToken(data)
}

func (m *Manager) Authenticate(token string) bool {
	data, err := m.current()
	return err == nil && (!data.Enabled || m.validToken(data, token))
}

func (m *Manager) persist() error {
	if err := os.MkdirAll(filepath.Dir(m.path), 0700); err != nil {
		return fmt.Errorf("无法创建身份认证配置目录：%w", err)
	}
	encoded, err := json.MarshalIndent(m.data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.path, append(encoded, '\n'), 0600)
}

func (m *Manager) issueToken(data config) (string, error) {
	payload, err := json.Marshal(struct {
		Account string `json:"account"`
		Expires int64  `json:"expires"`
	}{Account: data.Account, Expires: time.Now().Add(sessionDuration).Unix()})
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	key, err := base64.RawURLEncoding.DecodeString(data.SessionKey)
	if err != nil {
		return "", errors.New("身份认证会话密钥无效")
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(encoded))
	return encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (m *Manager) validToken(data config, token string) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 2 || token == "" {
		return false
	}
	key, err := base64.RawURLEncoding.DecodeString(data.SessionKey)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(parts[0]))
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(signature, mac.Sum(nil)) {
		return false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return false
	}
	var value struct {
		Account string `json:"account"`
		Expires int64  `json:"expires"`
	}
	return json.Unmarshal(payload, &value) == nil && value.Account == data.Account && value.Expires > time.Now().Unix()
}
