package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
	// 模型列表完全由配置决定，不注入任何硬编码模型。
	// 配置了两个 provider 都有 "gpt-4" 别名，dedup 后应为 1 个。
	if len(body.Data) != 1 {
		t.Errorf("Expected 1 model (gpt-4 deduped), got %d", len(body.Data))
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
// Start — E2E
// ============================================================

func TestStart_ListenAndServe(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop","index":0}]}`))
	}))
	defer upstream.Close()

	am := engine.NewAccountManager(&types.Config{
		RateLimit: types.RateLimitConfig{
			MaxRequestsPerMinute: 600,
			MinIntervalMs:        1,
			RetryIntervalMs:      5000,
		},
		Providers: []types.ProviderConfig{
			{
				Name:          "test",
				BaseURL:       upstream.URL,
				APIKeyEntries: []string{"test-key"},
				Models:        map[string]string{"m1": "m1"},
				Timeout:       10,
			},
		},
	})
	s := NewServer(am)

	// Start in goroutine
	go s.Start(0) // port 0 = random
	time.Sleep(500 * time.Millisecond)

	// The handler functions are registered on default mux (via http.HandleFunc),
	// so we just verify the server doesn't crash on start
	// We can't easily determine the random port, so we verify via handler tests instead
}

// ============================================================
// /v1/chat/completions — E2E through Start()
// ============================================================

func TestServerE2E_NonStream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"r1","object":"chat.completion","model":"m1","choices":[{"index":0,"message":{"role":"assistant","content":"Hello"},"finish_reason":"stop"}]}`))
	}))
	defer upstream.Close()

	am := engine.NewAccountManager(&types.Config{
		RateLimit: types.RateLimitConfig{
			MaxRequestsPerMinute: 600,
			MinIntervalMs:        1,
			RetryIntervalMs:      5000,
		},
		Providers: []types.ProviderConfig{
			{
				Name:          "test",
				BaseURL:       upstream.URL,
				APIKeyEntries: []string{"test-key"},
				Models:        map[string]string{"m1": "m1"},
				Timeout:       10,
			},
		},
	})
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

	if w.Result().StatusCode != 200 {
		t.Errorf("Expected 200, got %d: %s", w.Result().StatusCode, w.Body.String())
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

	if w.Result().StatusCode != 401 {
		t.Errorf("Expected 401 (forwarded from upstream), got %d", w.Result().StatusCode)
	}
}

// ============================================================
// 边缘覆盖：确保 model 路由 + realModel 替换路径全覆盖
// ============================================================

func TestHandleChatCompletions_ModelMapped(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 验证收到的 body 中包含 real model name
		buf := new(bytes.Buffer)
		buf.ReadFrom(r.Body)
		if !strings.Contains(buf.String(), `"model":"real-m1"`) {
			t.Errorf("Expected body to contain real-m1, got %s", buf.String())
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[]}`))
	}))
	defer upstream.Close()

	am := engine.NewAccountManager(&types.Config{
		RateLimit: types.RateLimitConfig{
			MaxRequestsPerMinute: 600,
			MinIntervalMs:        1,
			RetryIntervalMs:      5000,
		},
		Providers: []types.ProviderConfig{
			{
				Name:          "test",
				BaseURL:       upstream.URL,
				APIKeyEntries: []string{"test-key"},
				Models:        map[string]string{"alias-m1": "real-m1"},
				Timeout:       10,
			},
		},
	})
	s := NewServer(am)

	body, _ := json.Marshal(types.ChatRequest{
		Model:       "alias-m1",
		Messages:    []types.Message{{Role: "user", Content: "hi"}},
		Temperature: 0.7,
		TopP:        0.9,
		MaxTokens:   2048,
	})
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleChatCompletions(w, req)

	if w.Result().StatusCode != 200 {
		t.Errorf("Expected 200, got %d", w.Result().StatusCode)
	}
}
