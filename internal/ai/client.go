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
	BaseURL string   `json:"baseUrl"`
	Model   string   `json:"model"`
	APIKey  string   `json:"apiKey"`
	Models  []string `json:"models,omitempty"`
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
	"deepseek": {ID: "deepseek", Name: "DeepSeek", BaseURL: "https://api.deepseek.com/v1", Model: "deepseek-v4-flash"},
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

// Save stores the provider credentials, fetches the provider's model list
// with the new key, and remembers the first model as the default. When the
// model list cannot be fetched (offline, wrong key) it falls back to the
// caller-provided model so saving never blocks on the network.
func (m *Manager) Save(id, baseURL, model, key string) error {
	if _, ok := defaults[id]; !ok {
		return errors.New("不支持该 AI 服务")
	}
	if strings.TrimSpace(key) == "" {
		return errors.New("请填写 API Key")
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	key = strings.TrimSpace(key)

	// Attempt to fetch models with the new key before persisting. The first
	// model becomes the stored default so a mis-saved name can never stick.
	fetchedModels, fetchErr := fetchModels(id, baseURL, key)
	stored := storedProvider{BaseURL: baseURL, Model: strings.TrimSpace(model), APIKey: key}
	if fetchErr == nil && len(fetchedModels) > 0 {
		stored.Model = fetchedModels[0]
		stored.Models = fetchedModels
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	value, err := m.load()
	if err != nil {
		return err
	}
	// Preserve the previously remembered model list when this save could not
	// fetch a fresh one, so the UI still has valid choices after a transient
	// network failure.
	if len(stored.Models) == 0 {
		stored.Models = value.Providers[id].Models
	}
	value.Providers[id] = stored
	raw, _ := json.MarshalIndent(value, "", "  ")
	if err := os.MkdirAll(filepath.Dir(m.path), 0700); err != nil {
		return err
	}
	return os.WriteFile(m.path, append(raw, '\n'), 0600)
}

// fetchModels queries an OpenAI-compatible /models endpoint. Anthropic exposes
// no list endpoint, so its stable choices are returned locally.
func fetchModels(id, baseURL, key string) ([]string, error) {
	if id == "claude" || strings.Contains(baseURL, "anthropic.com") {
		return []string{"claude-sonnet-4-5", "claude-opus-4-5", "claude-haiku-4-5"}, nil
	}
	req, err := http.NewRequest(http.MethodGet, baseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
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

// Resolved is the effective provider endpoint for one request. APIKey must
// never be logged or echoed to clients; consumers pass it straight to the
// HTTP call.
type Resolved struct {
	BaseURL   string
	Model     string
	APIKey    string
	Anthropic bool
}

// Resolve returns the effective base URL, model and key for a provider after
// applying the user's per-provider overrides on top of built-in defaults.
func (m *Manager) Resolve(id, model string) (Resolved, error) {
	m.mu.Lock()
	value, err := m.load()
	m.mu.Unlock()
	if err != nil {
		return Resolved{}, err
	}
	base, ok := defaults[id]
	if !ok {
		return Resolved{}, errors.New("不支持该 AI 服务")
	}
	saved := value.Providers[id]
	if saved.APIKey == "" {
		return Resolved{}, errors.New("请先配置 API Key")
	}
	if saved.BaseURL != "" {
		base.BaseURL = saved.BaseURL
	}
	if saved.Model != "" && !foreignModel(id, saved.Model) {
		base.Model = saved.Model
	}
	if strings.TrimSpace(model) != "" && !foreignModel(id, model) {
		base.Model = strings.TrimSpace(model)
	}
	anthropicProtocol := id == "claude" || strings.Contains(base.BaseURL, "anthropic.com")
	return Resolved{BaseURL: base.BaseURL, Model: base.Model, APIKey: saved.APIKey, Anthropic: anthropicProtocol}, nil
}

// foreignModel reports whether a model string clearly belongs to another
// provider's naming family. It guards against a mis-saved model (for example
// "gpt-5.4" stored under the deepseek provider) being sent to the wrong API.
func foreignModel(id, model string) bool {
	switch id {
	case "deepseek":
		return strings.HasPrefix(model, "gpt-") || strings.HasPrefix(model, "claude-")
	case "openai":
		return strings.HasPrefix(model, "claude-") || strings.HasPrefix(model, "deepseek-")
	case "claude":
		return strings.HasPrefix(model, "gpt-") || strings.HasPrefix(model, "deepseek-")
	}
	return false
}

func (m *Manager) Chat(id, model string, messages []map[string]string) (string, error) {
	cfg, err := m.Resolve(id, model)
	if err != nil {
		return "", err
	}
	if cfg.Anthropic {
		return anthropic(cfg, messages)
	}
	return compatible(cfg, messages)
}

// ChatResolved performs one request with an already-resolved provider. It is
// used by read-only internal reviewers and never persists or logs credentials.
func ChatResolved(cfg Resolved, messages []map[string]string) (string, error) {
	if cfg.Anthropic {
		return anthropic(cfg, messages)
	}
	return compatible(cfg, messages)
}

// Models reads the model list from an OpenAI-compatible endpoint, falling back
// to the list remembered at save time when the endpoint is unreachable.
// Anthropic exposes no list endpoint, so its stable choices are returned
// locally.
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
	models, fetchErr := fetchModels(id, strings.TrimRight(base.BaseURL, "/"), saved.APIKey)
	if fetchErr == nil {
		return models, nil
	}
	// Network is down or key rotated; serve what we remembered at save time.
	if len(saved.Models) > 0 {
		return saved.Models, nil
	}
	return nil, fetchErr
}
func compatible(r Resolved, messages []map[string]string) (string, error) {
	body, _ := json.Marshal(map[string]any{"model": r.Model, "messages": messages, "stream": false})
	req, err := http.NewRequest(http.MethodPost, r.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+r.APIKey)
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
func anthropic(r Resolved, messages []map[string]string) (string, error) {
	body, _ := json.Marshal(map[string]any{"model": r.Model, "max_tokens": 2048, "messages": messages})
	req, err := http.NewRequest(http.MethodPost, r.BaseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("x-api-key", r.APIKey)
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

// httpClient is package-level so tests can inject a fake transport without
// binding a network port.
var httpClient = &http.Client{Timeout: 90 * time.Second}

func send(req *http.Request, target any) error {
	response, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return providerError(raw, response.Status)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("AI 响应无法解析：%w", err)
	}
	return nil
}

// providerError prefers the provider's JSON error message, falling back to the
// HTTP status so a plain-text error page is not misreported as a parse failure.
func providerError(raw []byte, status string) error {
	var payload struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &payload) == nil && payload.Error.Message != "" {
		return fmt.Errorf("AI 请求失败：%s", payload.Error.Message)
	}
	message := strings.TrimSpace(string(raw))
	if len(message) > 500 {
		message = message[:500] + "…"
	}
	if message != "" {
		return fmt.Errorf("AI 请求失败：%s", message)
	}
	return fmt.Errorf("AI 请求失败：%s", status)
}
