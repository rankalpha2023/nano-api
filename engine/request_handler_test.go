package engine_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"nano-api/engine"
	"nano-api/types"
)

// ============================================================
// SendRequest 测试
// ============================================================

func TestSendRequest_Basic(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("Expected POST, got %s", r.Method)
		}
		if ctype := r.Header.Get("Content-Type"); ctype != "application/json" {
			t.Errorf("Expected Content-Type application/json, got %s", ctype)
		}
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-key" {
			t.Errorf("Expected Authorization Bearer test-key, got %s", auth)
		}
		w.WriteHeader(200)
		w.Write([]byte(`{"choices":[]}`))
	}))
	defer mock.Close()

	handler := engine.NewRequestHandler()
	req := &types.ChatRequest{
		Model:    "test",
		Messages: []types.Message{{Role: "user", Content: "hi"}},
	}

	resp, _, err := handler.SendRequest("test-key", mock.URL, req, 10, "", nil, nil)
	if err != nil {
		t.Fatalf("SendRequest failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}
}

func TestSendRequest_CustomHeaders(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Custom") != "custom-value" {
			t.Errorf("Expected X-Custom header, got '%s'", r.Header.Get("X-Custom"))
		}
		// Authorization override
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("Expected auth test-key, got '%s'", r.Header.Get("Authorization"))
		}
		w.WriteHeader(200)
	}))
	defer mock.Close()

	handler := engine.NewRequestHandler()
	req := &types.ChatRequest{Model: "test"}
	customHeaders := map[string]string{"X-Custom": "custom-value"}

	_, _, err := handler.SendRequest("test-key", mock.URL, req, 10, "", customHeaders, nil)
	if err != nil {
		t.Fatalf("SendRequest failed: %v", err)
	}
}

func TestSendRequest_Proxy(t *testing.T) {
	// 验证传入有效 proxy 不崩溃
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer mock.Close()

	handler := engine.NewRequestHandler()
	req := &types.ChatRequest{Model: "test"}

	// 传入空字符串 proxy — 不使用代理
	_, _, err := handler.SendRequest("key", mock.URL, req, 10, "", nil, nil)
	if err != nil {
		t.Errorf("SendRequest with empty proxy failed: %v", err)
	}
}

func TestSendRequest_InvalidURL(t *testing.T) {
	handler := engine.NewRequestHandler()
	req := &types.ChatRequest{Model: "test"}

	// 无效 URL: 包含空格
	_, _, err := handler.SendRequest("key", "http://\x7f.invalid", req, 10, "", nil, nil)
	if err == nil {
		t.Error("Expected error for invalid URL")
	}
}

func TestSendRequest_Timeout(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(200)
	}))
	defer mock.Close()

	handler := engine.NewRequestHandler()
	req := &types.ChatRequest{Model: "test"}

	// timeout=1 秒，mock 睡眠 2 秒 → 超时
	start := time.Now()
	_, _, err := handler.SendRequest("key", mock.URL, req, 1, "", nil, nil)
	elapsed := time.Since(start)

	if err == nil {
		t.Error("Expected timeout error")
	}
	if elapsed > 5*time.Second {
		t.Errorf("Expected timeout < 5s, took %v", elapsed)
	}
}

func TestSendRequest_ExtraFieldsMerged(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"thinking_enabled":true`) {
			t.Errorf("Expected thinking_enabled in body, got %s", string(body))
		}
		w.WriteHeader(200)
	}))
	defer mock.Close()

	handler := engine.NewRequestHandler()
	req := &types.ChatRequest{Model: "test"}
	extraFields := map[string]interface{}{"thinking_enabled": true}

	_, _, err := handler.SendRequest("key", mock.URL, req, 10, "", nil, extraFields)
	if err != nil {
		t.Fatalf("SendRequest failed: %v", err)
	}
}

// ============================================================
// StreamResponse 测试
// ============================================================

func TestStreamResponse_Basic(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 发送 JSON chunks (非 SSE 格式，因为 json.Decoder 直接解码)
		fmt.Fprintf(w, `{"choices":[{"delta":{"role":"assistant"},"index":0}]}`+"\n")
		fmt.Fprintf(w, `{"choices":[{"delta":{"content":"Hello"},"index":0}]}`+"\n")
		fmt.Fprintf(w, `{"choices":[{"delta":{},"finish_reason":"stop","index":0}]}`+"\n")
	}))
	defer mock.Close()

	resp, err := http.Get(mock.URL)
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}

	handler := engine.NewRequestHandler()
	var received []string
	err = handler.StreamResponse(resp, func(choice *types.Choice) error {
		if choice.Delta != nil && choice.Delta.Content != "" {
			received = append(received, choice.Delta.Content)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("StreamResponse failed: %v", err)
	}

	if len(received) != 1 || received[0] != "Hello" {
		t.Errorf("Expected ['Hello'], got %v", received)
	}
}

func TestStreamResponse_ReasoningContent(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"choices":[{"delta":{"reasoning_content":"thinking..."},"index":0}]}`)
	}))
	defer mock.Close()

	resp, _ := http.Get(mock.URL)
	handler := engine.NewRequestHandler()

	var reasoning string
	handler.StreamResponse(resp, func(choice *types.Choice) error {
		if choice.Delta != nil && choice.Delta.ReasoningContent != "" {
			reasoning = choice.Delta.ReasoningContent
		}
		return nil
	})

	if reasoning != "thinking..." {
		t.Errorf("Expected 'thinking...', got '%s'", reasoning)
	}
}

func TestStreamResponse_NonStreamingBody(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`not valid json`))
	}))
	defer mock.Close()

	resp, _ := http.Get(mock.URL)
	handler := engine.NewRequestHandler()

	err := handler.StreamResponse(resp, func(choice *types.Choice) error {
		return nil
	})
	if err == nil {
		t.Error("Expected error for non-streaming body")
	}
}

// ============================================================
// NewRequestHandler / 零值测试
// ============================================================

func TestNewRequestHandler_NotNil(t *testing.T) {
	handler := engine.NewRequestHandler()
	if handler == nil {
		t.Fatal("Expected non-nil handler")
	}
}

// ============================================================
// 补充覆盖率：SendRequest 错误路径
// ============================================================

func TestSendRequest_ConnectionRefused(t *testing.T) {
	handler := engine.NewRequestHandler()
	req := &types.ChatRequest{Model: "test"}

	// 未监听的端口
	_, _, err := handler.SendRequest("key", "http://127.0.0.1:1", req, 1, "", nil, nil)
	if err == nil {
		t.Error("Expected connection error")
	}
}

func TestSendRequest_WithValidProxyIgnored(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer mock.Close()

	handler := engine.NewRequestHandler()
	req := &types.ChatRequest{Model: "test"}

	// 传入一个无法解析的 proxy — 应该被忽略
	_, _, err := handler.SendRequest("key", mock.URL, req, 10, "://invalid", nil, nil)
	if err != nil {
		t.Fatalf("Broken proxy should not cause error: %v", err)
	}
}

func TestStreamResponse_EmptyBody(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(``))
	}))
	defer mock.Close()

	resp, _ := http.Get(mock.URL)
	handler := engine.NewRequestHandler()

	err := handler.StreamResponse(resp, func(choice *types.Choice) error {
		return nil
	})
	// EOF → 正常结束
	if err != nil {
		t.Errorf("Empty body should not error: %v", err)
	}
}

func TestStreamResponse_CallbackError(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"choices":[{"delta":{"content":"test"},"index":0}]}`+"\n")
	}))
	defer mock.Close()

	resp, _ := http.Get(mock.URL)
	handler := engine.NewRequestHandler()

	err := handler.StreamResponse(resp, func(choice *types.Choice) error {
		return fmt.Errorf("callback abort")
	})
	if err == nil {
		t.Error("Expected callback error to propagate")
	}
}

func TestSendRequest_ExtraFieldsNil(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer mock.Close()

	handler := engine.NewRequestHandler()
	req := &types.ChatRequest{Model: "test", Messages: []types.Message{{Role: "user", Content: "hi"}}}

	// extraFields=nil 走快速路径
	_, _, err := handler.SendRequest("key", mock.URL, req, 10, "", nil, nil)
	if err != nil {
		t.Fatalf("SendRequest with nil extraFields: %v", err)
	}

	// extraFields 空 map 也走快速路径
	_, _, err = handler.SendRequest("key", mock.URL, req, 10, "", nil, map[string]interface{}{})
	if err != nil {
		t.Fatalf("SendRequest with empty extraFields: %v", err)
	}
}
