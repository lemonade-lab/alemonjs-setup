package ai

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func jsonResponse(req *http.Request, payload string) (*http.Response, error) {
	return &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(payload)),
		Request:    req,
	}, nil
}

// TestSaveFetchesAndRemembersModels verifies saving a key pulls the provider's
// model list, picks the first as the default, and persists it for offline use.
func TestSaveFetchesAndRemembersModels(t *testing.T) {
	original := httpClient.Transport
	httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/v1/models" {
			t.Fatalf("应请求 /v1/models，实际 %s", req.URL.Path)
		}
		if got := req.Header.Get("Authorization"); got != "Bearer secret" {
			t.Fatalf("Authorization 错误：%q", got)
		}
		return jsonResponse(req, `{"data":[{"id":"deepseek-v4-flash"},{"id":"deepseek-v4-pro"}]}`)
	})
	defer func() { httpClient.Transport = original }()

	m := &Manager{path: t.TempDir() + "/alx-ai.json"}
	if err := m.Save("deepseek", "https://api.deepseek.com/v1", "gpt-5.4", "secret"); err != nil {
		t.Fatal(err)
	}
	// The stored model must be the first fetched model, not the caller's value.
	cfg, err := m.Resolve("deepseek", "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model != "deepseek-v4-flash" {
		t.Errorf("保存后应使用拉取到的第一个模型，实际 %q", cfg.Model)
	}
	// The remembered list is served when the network is down.
	httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, io.ErrUnexpectedEOF
	})
	models, err := m.Models("deepseek")
	if err != nil {
		t.Fatalf("断网时应回退到记忆的模型列表：%v", err)
	}
	if len(models) != 2 || models[0] != "deepseek-v4-flash" || models[1] != "deepseek-v4-pro" {
		t.Errorf("记忆的模型列表错误：%v", models)
	}
}

// TestSaveFallsBackOnFetchFailure verifies a failed model fetch does not block
// saving; the caller's model is kept.
func TestSaveFallsBackOnFetchFailure(t *testing.T) {
	original := httpClient.Transport
	httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, io.ErrUnexpectedEOF
	})
	defer func() { httpClient.Transport = original }()

	m := &Manager{path: t.TempDir() + "/alx-ai.json"}
	if err := m.Save("deepseek", "https://api.deepseek.com/v1", "deepseek-chat", "secret"); err != nil {
		t.Fatal(err)
	}
	cfg, err := m.Resolve("deepseek", "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model != "deepseek-chat" {
		t.Errorf("拉取失败时应用调用方传入的模型，实际 %q", cfg.Model)
	}
}

func TestForeignModel(t *testing.T) {
	cases := []struct {
		id, model string
		want      bool
	}{
		{"deepseek", "gpt-5.4", true},       // 错存了 OpenAI 模型
		{"deepseek", "claude-sonnet-4-5", true},
		{"deepseek", "deepseek-v4-flash", false},
		{"deepseek", "deepseek-chat", false},
		{"openai", "gpt-5.4", false},
		{"openai", "claude-opus-4-5", true},
		{"claude", "claude-sonnet-4-5", false},
		{"claude", "deepseek-chat", true},
	}
	for _, c := range cases {
		if got := foreignModel(c.id, c.model); got != c.want {
			t.Errorf("foreignModel(%q, %q) = %v, 期望 %v", c.id, c.model, got, c.want)
		}
	}
}

func TestResolveFallsBackOnForeignModel(t *testing.T) {
	m := &Manager{path: t.TempDir() + "/alx-ai.json"}
	if err := m.Save("deepseek", "https://api.deepseek.com/v1", "gpt-5.4", "secret"); err != nil {
		t.Fatal(err)
	}
	cfg, err := m.Resolve("deepseek", "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model == "gpt-5.4" {
		t.Errorf("错存的 OpenAI 模型不应被发送给 DeepSeek，实际 %q", cfg.Model)
	}
	if cfg.Model != "deepseek-v4-flash" {
		t.Errorf("应回退到 DeepSeek 默认模型，实际 %q", cfg.Model)
	}
	if cfg.Anthropic {
		t.Error("DeepSeek 不应被识别为 Anthropic 协议")
	}
}
