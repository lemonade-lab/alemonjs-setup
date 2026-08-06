// Package ai stores local provider settings and sends one-turn chat requests.
package ai

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Provider struct {
	ID, Name, BaseURL, Model string
	HasKey                   bool
}
type storedProvider struct {
	BaseURL string `json:"baseUrl"`
	Model   string `json:"model"`
	APIKey  string `json:"apiKey"`
}
type store struct {
	Providers map[string]storedProvider `json:"providers"`
}
type Manager struct {
	path string
	mu   sync.Mutex
}

var defaults = map[string]Provider{
	"openai":   {ID: "openai", Name: "Codex / OpenAI", BaseURL: "https://api.openai.com/v1", Model: "gpt-5.4"},
	"deepseek": {ID: "deepseek", Name: "DeepSeek", BaseURL: "https://api.deepseek.com/v1", Model: "deepseek-chat"},
	"claude":   {ID: "claude", Name: "Claude", BaseURL: "https://api.anthropic.com", Model: "claude-sonnet-4-5"},
}

func New() (*Manager, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	return &Manager{path: filepath.Join(dir, "alemonjs", "alx-ai.json")}, nil
}
func (m *Manager) load() (store, error) {
	var value store
	value.Providers = map[string]storedProvider{}
	raw, err := os.ReadFile(m.path)
	if errors.Is(err, os.ErrNotExist) {
		return value, nil
	}
	if err != nil {
		return value, err
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return value, fmt.Errorf("AI 配置无效：%w", err)
	}
	if value.Providers == nil {
		value.Providers = map[string]storedProvider{}
	}
	return value, nil
}
func (m *Manager) List() ([]Provider, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	value, err := m.load()
	if err != nil {
		return nil, err
	}
	out := make([]Provider, 0, len(defaults))
	for _, id := range []string{"openai", "deepseek", "claude"} {
		item := defaults[id]
		saved := value.Providers[id]
		if saved.BaseURL != "" {
			item.BaseURL = saved.BaseURL
		}
		if saved.Model != "" {
			item.Model = saved.Model
		}
		item.HasKey = saved.APIKey != ""
		out = append(out, item)
	}
	return out, nil
}
func (m *Manager) Save(id, baseURL, model, key string) error {
	if _, ok := defaults[id]; !ok {
		return errors.New("不支持该 AI 服务")
	}
	if strings.TrimSpace(key) == "" {
		return errors.New("请填写 API Key")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	value, err := m.load()
	if err != nil {
		return err
	}
	value.Providers[id] = storedProvider{BaseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"), Model: strings.TrimSpace(model), APIKey: strings.TrimSpace(key)}
	raw, _ := json.MarshalIndent(value, "", "  ")
	if err := os.MkdirAll(filepath.Dir(m.path), 0700); err != nil {
		return err
	}
	return os.WriteFile(m.path, append(raw, '\n'), 0600)
}
func (m *Manager) Chat(id, model string, messages []map[string]string) (string, error) {
	m.mu.Lock()
	value, err := m.load()
	m.mu.Unlock()
	if err != nil {
		return "", err
	}
	base, ok := defaults[id]
	if !ok {
		return "", errors.New("不支持该 AI 服务")
	}
	saved := value.Providers[id]
	if saved.APIKey == "" {
		return "", errors.New("请先配置 API Key")
	}
	if saved.BaseURL != "" {
		base.BaseURL = saved.BaseURL
	}
	if saved.Model != "" {
		base.Model = saved.Model
	}
	if strings.TrimSpace(model) != "" {
		base.Model = strings.TrimSpace(model)
	}
	if id == "claude" {
		return anthropic(base, saved.APIKey, messages)
	}
	return compatible(base, saved.APIKey, messages)
}

// Models reads the model list from an OpenAI-compatible endpoint. Anthropic
// does not expose a public list endpoint, so its current stable choices are
// returned locally.
func (m *Manager) Models(id string) ([]string, error) {
	m.mu.Lock()
	value, err := m.load()
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}
	base, ok := defaults[id]
	if !ok {
		return nil, errors.New("不支持该 AI 服务")
	}
	saved := value.Providers[id]
	if saved.APIKey == "" {
		return nil, errors.New("请先配置 API Key")
	}
	if saved.BaseURL != "" {
		base.BaseURL = saved.BaseURL
	}
	if id == "claude" || strings.Contains(base.BaseURL, "anthropic.com") {
		return []string{"claude-sonnet-4-5", "claude-opus-4-5", "claude-haiku-4-5"}, nil
	}
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(base.BaseURL, "/")+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+saved.APIKey)
	var data struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := send(req, &data); err != nil {
		return nil, err
	}
	models := make([]string, 0, len(data.Data))
	for _, item := range data.Data {
		if item.ID != "" {
			models = append(models, item.ID)
		}
	}
	if len(models) == 0 {
		return nil, errors.New("该接口没有返回可用模型")
	}
	return models, nil
}
func compatible(p Provider, key string, messages []map[string]string) (string, error) {
	body, _ := json.Marshal(map[string]any{"model": p.Model, "messages": messages, "stream": false})
	req, err := http.NewRequest(http.MethodPost, p.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	var data struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := send(req, &data); err != nil {
		return "", err
	}
	if len(data.Choices) == 0 {
		return "", errors.New(data.Error.Message)
	}
	return data.Choices[0].Message.Content, nil
}
func anthropic(p Provider, key string, messages []map[string]string) (string, error) {
	body, _ := json.Marshal(map[string]any{"model": p.Model, "max_tokens": 2048, "messages": messages})
	req, err := http.NewRequest(http.MethodPost, p.BaseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("x-api-key", key)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Content-Type", "application/json")
	var data struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := send(req, &data); err != nil {
		return "", err
	}
	if len(data.Content) == 0 {
		return "", errors.New(data.Error.Message)
	}
	return data.Content[0].Text, nil
}
func send(req *http.Request, target any) error {
	client := http.Client{Timeout: 90 * time.Second}
	response, err := client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("AI 请求失败：%s", response.Status)
	}
	return nil
}
