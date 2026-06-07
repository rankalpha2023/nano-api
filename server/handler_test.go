package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"nano-api/engine"
	"nano-api/types"
)

// makeTestAM 创建带 httptest 上游的 AccountManager
func makeTestAM(t *testing.T, upstreamURL string) *engine.AccountManager {
	t.Helper()
	return engine.NewAccountManager(&types.Config{
		RateLimit: types.RateLimitConfig{
			MaxRequestsPerMinute: 600,
			MinIntervalMs:        1,
			RetryIntervalMs:      5000,
		},
		Providers: []types.ProviderConfig{
			{
				Name:          "test",
				BaseURL:       upstreamURL,
				APIKeyEntries: []string{"test-key"},
				Models:        map[string]string{"m1": "m1"},
				Timeout:       10,
			},
		},
	})
}

// ============================================================
// /v1/models
// ============================================================

func TestHandleListModels_GET(t *testing.T) {
	am := engine.NewAccountManager(&types.Config{
		RateLimit: types.RateLimitConfig{
			MaxRequestsPerMinute: 40,
			MinIntervalMs:        100,
			RetryIntervalMs:      5000,
		},
		Providers: []types.ProviderConfig{
			{Name: "p1", BaseURL: "http://x", APIKeyEntries: []string{"k"}, Models: map[string]string{"gpt-4": "gpt-4-real"}},
			{Name: "p2", BaseURL: "http://x", APIKeyEntries: []string{"k"}, Models: map[string]string{"gpt-4": "gpt-4-v2"}},
		},
	})
	s := NewServer(am)

	req := httptest.NewRequest("GET", "/v1/models", nil)
	w := httptest.NewRecorder()
	s.handleListModels(w, req)

	resp := w.Result()
	if resp.StatusCode != 200 {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}

	var body types.ModelListResponse
	json.NewDecoder(resp.Body).Decode(&body)
	if body.Object != "list" {
		t.Errorf("Expected 'list', got '%s'", body.Object)
	}
	if len(body.Data) != 1 {
		t.Errorf("Expected 1 model (dedup), got %d", len(body.Data))
	}
}

func TestHandleListModels_OPTIONS(t *testing.T) {
	am := engine.NewAccountManager(&types.Config{
		Providers: []types.ProviderConfig{},
	})
	s := NewServer(am)

	req := httptest.NewRequest("OPTIONS", "/v1/models", nil)
	w := httptest.NewRecorder()
	s.handleListModels(w, req)

	if w.Result().StatusCode != 200 {
		t.Errorf("Expected 200, got %d", w.Result().StatusCode)
	}
}

func TestHandleListModels_MethodNotAllowed(t *testing.T) {
	am := engine.NewAccountManager(&types.Config{Providers: []types.ProviderConfig{}})
	s := NewServer(am)

	req := httptest.NewRequest("POST", "/v1/models", nil)
	w := httptest.NewRecorder()
	s.handleListModels(w, req)

	if w.Result().StatusCode != 405 {
		t.Errorf("Expected 405, got %d", w.Result().StatusCode)
	}
}

// ============================================================
// /v1/chat/completions — 错误处理
// ============================================================

func TestHandleChatCompletions_OPTIONS(t *testing.T) {
	am := engine.NewAccountManager(&types.Config{})
	s := NewServer(am)

	req := httptest.NewRequest("OPTIONS", "/v1/chat/completions", nil)
	w := httptest.NewRecorder()
	s.handleChatCompletions(w, req)

	if w.Result().StatusCode != 200 {
		t.Errorf("Expected 200, got %d", w.Result().StatusCode)
	}
}

func TestHandleChatCompletions_MethodNotAllowed(t *testing.T) {
	am := engine.NewAccountManager(&types.Config{})
	s := NewServer(am)

	req := httptest.NewRequest("GET", "/v1/chat/completions", nil)
	w := httptest.NewRecorder()
	s.handleChatCompletions(w, req)

	if w.Result().StatusCode != 405 {
		t.Errorf("Expected 405, got %d", w.Result().StatusCode)
	}
}

func TestHandleChatCompletions_MissingContentType(t *testing.T) {
	am := engine.NewAccountManager(&types.Config{})
	s := NewServer(am)

	body := bytes.NewReader([]byte(`{"model":"m1"}`))
	req := httptest.NewRequest("POST", "/v1/chat/completions", body)
	w := httptest.NewRecorder()
	s.handleChatCompletions(w, req)

	if w.Result().StatusCode != 400 {
		t.Errorf("Expected 400, got %d", w.Result().StatusCode)
	}
}

func TestHandleChatCompletions_EmptyBody(t *testing.T) {
	am := engine.NewAccountManager(&types.Config{})
	s := NewServer(am)

	body := bytes.NewReader([]byte{})
	req := httptest.NewRequest("POST", "/v1/chat/completions", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleChatCompletions(w, req)

	if w.Result().StatusCode != 400 {
		t.Errorf("Expected 400, got %d", w.Result().StatusCode)
	}
}

func TestHandleChatCompletions_InvalidJSON(t *testing.T) {
	am := engine.NewAccountManager(&types.Config{})
	s := NewServer(am)

	body := bytes.NewReader([]byte(`not json`))
	req := httptest.NewRequest("POST", "/v1/chat/completions", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleChatCompletions(w, req)

	if w.Result().StatusCode != 400 {
		t.Errorf("Expected 400, got %d", w.Result().StatusCode)
	}
}

func TestHandleChatCompletions_NoAvailableAccount(t *testing.T) {
	// 未知 model → 无 provider → 503
	am := engine.NewAccountManager(&types.Config{
		RateLimit: types.RateLimitConfig{
			MaxRequestsPerMinute: 40, MinIntervalMs: 100, RetryIntervalMs: 5000,
		},
		Providers: []types.ProviderConfig{
			{Name: "p1", BaseURL: "http://x", APIKeyEntries: []string{"k"}, Models: map[string]string{"m1": "m1"}},
		},
	})
	s := NewServer(am)

	body, _ := json.Marshal(types.ChatRequest{Model: "nonexistent", Messages: []types.Message{{Role: "user", Content: "hi"}}})
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleChatCompletions(w, req)

	if w.Result().StatusCode != 503 {
		t.Errorf("Expected 503, got %d", w.Result().StatusCode)
	}
}

// ============================================================
// /v1/chat/completions — 正常非流式响应
// ============================================================

func TestHandleChatCompletions_NonStream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := types.ChatResponse{
			ID:      "resp-1",
			Object:  "chat.completion",
			Model:   "m1",
			Choices: []types.Choice{{Index: 0, Message: types.Message{Role: "assistant", Content: "Hello!"}, FinishReason: "stop"}},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer upstream.Close()

	am := makeTestAM(t, upstream.URL)
	s := NewServer(am)

	body, _ := json.Marshal(types.ChatRequest{
		Model:       "m1",
		Messages:    []types.Message{{Role: "user", Content: "hi"}},
		Temperature: 0.5,
		MaxTokens:   100,
	})
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleChatCompletions(w, req)

	resp := w.Result()
	if resp.StatusCode != 200 {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}
}

// ============================================================
// /v1/chat/completions — 流式响应
// ============================================================

func TestHandleChatCompletions_Stream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		chunks := []string{
			`data: {"choices":[{"delta":{"role":"assistant"}}]}`,
			`data: {"choices":[{"delta":{"content":"Hello"}}]}`,
			`data: {"choices":[{"delta":{"reasoning_content":"thinking..."}}]}`,
			`data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`,
		}
		for _, c := range chunks {
			fmt.Fprintln(w, c)
			flusher.Flush()
		}
	}))
	defer upstream.Close()

	am := makeTestAM(t, upstream.URL)
	s := NewServer(am)

	body, _ := json.Marshal(types.ChatRequest{
		Model:    "m1",
		Messages: []types.Message{{Role: "user", Content: "hi"}},
		Stream:   true,
	})
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleChatCompletions(w, req)

	resp := w.Result()
	if resp.StatusCode != 200 {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}

	ctype := resp.Header.Get("Content-Type")
	if !strings.Contains(ctype, "text/event-stream") {
		t.Errorf("Expected text/event-stream, got '%s'", ctype)
	}

	// 验证 body 中包含所有 chunk
	raw := w.Body.String()
	for _, keyword := range []string{"assistant", "Hello", "thinking...", "stop"} {
		if !strings.Contains(raw, keyword) {
			t.Errorf("Expected '%s' in stream body", keyword)
		}
	}
}

// ============================================================
// Header 转发
// ============================================================

func TestHandleChatCompletions_HeaderForwarding(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "42")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[]}`))
	}))
	defer upstream.Close()

	am := makeTestAM(t, upstream.URL)
	s := NewServer(am)

	body, _ := json.Marshal(types.ChatRequest{
		Model:    "m1",
		Messages: []types.Message{{Role: "user", Content: "hi"}},
	})
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleChatCompletions(w, req)

	if w.Result().Header.Get("X-RateLimit-Remaining") != "42" {
		t.Errorf("Expected X-RateLimit-Remaining=42, got '%s'", w.Result().Header.Get("X-RateLimit-Remaining"))
	}
}

// ============================================================
// 上游错误转发
// ============================================================

func TestHandleChatCompletions_UpstreamError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
	}))
	defer upstream.Close()

	am := makeTestAM(t, upstream.URL)
	s := NewServer(am)

	body, _ := json.Marshal(types.ChatRequest{
		Model:    "m1",
		Messages: []types.Message{{Role: "user", Content: "hi"}},
	})
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleChatCompletions(w, req)

	// 上游 401 → 正常转发（handler 不拦截上游错误码）
	if w.Result().StatusCode != 401 {
		t.Errorf("Expected 401 (forwarded from upstream), got %d", w.Result().StatusCode)
	}
}
