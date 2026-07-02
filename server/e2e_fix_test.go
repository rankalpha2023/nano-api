package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"nano-api/engine"
	"nano-api/types"
)

// ============================================================
// F4: Trace ID 端到端测试
// ============================================================

// TestF4_TraceIDInResponseHeader 验证非流式响应头包含 X-Trace-ID
func TestF4_TraceIDInResponseHeader(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	resp := w.Result()
	traceID := resp.Header.Get("X-Trace-ID")
	if traceID == "" {
		t.Error("Expected non-empty X-Trace-ID in response header")
	}
	if !strings.HasPrefix(traceID, "req-") {
		t.Errorf("Expected traceID to start with 'req-', got '%s'", traceID)
	}
}

// TestF4_TraceIDInStreamResponseHeader 验证流式响应头也包含 X-Trace-ID
func TestF4_TraceIDInStreamResponseHeader(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"hi"}}]}`)
		flusher.Flush()
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
	traceID := resp.Header.Get("X-Trace-ID")
	if traceID == "" {
		t.Error("Expected non-empty X-Trace-ID in stream response header")
	}
}

// TestF4_TraceIDUniquePerRequest 验证每个请求的 traceID 唯一
func TestF4_TraceIDUniquePerRequest(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"choices":[]}`))
	}))
	defer upstream.Close()

	am := makeTestAM(t, upstream.URL)
	s := NewServer(am)

	seen := make(map[string]bool)
	for i := 0; i < 20; i++ {
		body, _ := json.Marshal(types.ChatRequest{
			Model:    "m1",
			Messages: []types.Message{{Role: "user", Content: "hi"}},
		})
		req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.handleChatCompletions(w, req)

		traceID := w.Result().Header.Get("X-Trace-ID")
		if traceID == "" {
			t.Fatal("Expected non-empty traceID")
		}
		if seen[traceID] {
			t.Errorf("Duplicate traceID: %s", traceID)
		}
		seen[traceID] = true
	}
}

// TestF4_TraceIDFormat 验证 traceID 格式为 req-MMDD-HHMMSS-XXXXXXXX
func TestF4_TraceIDFormat(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"choices":[]}`))
	}))
	defer upstream.Close()

	am := makeTestAM(t, upstream.URL)
	s := NewServer(am)

	body, _ := json.Marshal(types.ChatRequest{Model: "m1", Messages: []types.Message{{Role: "user", Content: "hi"}}})
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleChatCompletions(w, req)

	traceID := w.Result().Header.Get("X-Trace-ID")
	// 格式: req-MMDD-HHMMSS-XXXXXXXX（XXXXXXXX 为 8 位 hex）
	// 长度: "req-" (4) + "MMDD-" (5) + "HHMMSS-" (7) + "XXXXXXXX" (8) = 24
	if len(traceID) != 24 {
		t.Errorf("Expected traceID length 24, got %d ('%s')", len(traceID), traceID)
	}
}

// ============================================================
// F3: MarkAccountAvailable 时序测试
// ============================================================

// TestF3_AccountMarkedAvailableOn200 验证 200 响应后账户被标记可用
func TestF3_AccountMarkedAvailableOn200(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[]}`))
	}))
	defer upstream.Close()

	am := makeTestAM(t, upstream.URL)
	s := NewServer(am)

	ps := am.GetProviderState("test")
	if ps == nil {
		t.Fatal("Expected provider state")
	}
	if ps.AvailableCountForTest() != 1 {
		t.Fatalf("Expected 1 available account initially, got %d", ps.AvailableCountForTest())
	}

	body, _ := json.Marshal(types.ChatRequest{Model: "m1", Messages: []types.Message{{Role: "user", Content: "hi"}}})
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleChatCompletions(w, req)

	if w.Result().StatusCode != 200 {
		t.Errorf("Expected 200, got %d", w.Result().StatusCode)
	}

	// 200 后账户应仍可用
	if ps.AvailableCountForTest() != 1 {
		t.Errorf("Expected 1 available account after 200, got %d", ps.AvailableCountForTest())
	}
	if ps.FailedCountForTest() != 0 {
		t.Errorf("Expected 0 failed accounts after 200, got %d", ps.FailedCountForTest())
	}
}

// TestF3_AccountMarkedFailedOn401 验证 401 响应后账户被标记失败
func TestF3_AccountMarkedFailedOn401(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer upstream.Close()

	am := makeTestAM(t, upstream.URL)
	s := NewServer(am)

	ps := am.GetProviderState("test")
	if ps.AvailableCountForTest() != 1 {
		t.Fatalf("Expected 1 available initially, got %d", ps.AvailableCountForTest())
	}

	body, _ := json.Marshal(types.ChatRequest{Model: "m1", Messages: []types.Message{{Role: "user", Content: "hi"}}})
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleChatCompletions(w, req)

	// 401 应被转发
	if w.Result().StatusCode != 401 {
		t.Errorf("Expected 401 forwarded, got %d", w.Result().StatusCode)
	}

	// 账户应被标记失败
	if ps.AvailableCountForTest() != 0 {
		t.Errorf("Expected 0 available after 401, got %d", ps.AvailableCountForTest())
	}
	if ps.FailedCountForTest() != 1 {
		t.Errorf("Expected 1 failed after 401, got %d", ps.FailedCountForTest())
	}
}

// TestF3_AccountMarkedFailedOn403 验证 403 响应后账户被标记失败
func TestF3_AccountMarkedFailedOn403(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
	}))
	defer upstream.Close()

	am := makeTestAM(t, upstream.URL)
	s := NewServer(am)

	ps := am.GetProviderState("test")

	body, _ := json.Marshal(types.ChatRequest{Model: "m1", Messages: []types.Message{{Role: "user", Content: "hi"}}})
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleChatCompletions(w, req)

	if w.Result().StatusCode != 403 {
		t.Errorf("Expected 403 forwarded, got %d", w.Result().StatusCode)
	}
	if ps.FailedCountForTest() != 1 {
		t.Errorf("Expected 1 failed after 403, got %d", ps.FailedCountForTest())
	}
	if ps.AvailableCountForTest() != 0 {
		t.Errorf("Expected 0 available after 403, got %d", ps.AvailableCountForTest())
	}
}

// TestF3_AccountAvailableOn500 验证 5xx 响应后账户仍可用（上游错误非账户问题）
func TestF3_AccountAvailableOn500(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte(`{"error":"internal"}`))
	}))
	defer upstream.Close()

	am := makeTestAM(t, upstream.URL)
	s := NewServer(am)

	ps := am.GetProviderState("test")

	body, _ := json.Marshal(types.ChatRequest{Model: "m1", Messages: []types.Message{{Role: "user", Content: "hi"}}})
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleChatCompletions(w, req)

	if w.Result().StatusCode != 500 {
		t.Errorf("Expected 500 forwarded, got %d", w.Result().StatusCode)
	}
	// 5xx 不标记失败
	if ps.FailedCountForTest() != 0 {
		t.Errorf("Expected 0 failed after 500, got %d", ps.FailedCountForTest())
	}
	if ps.AvailableCountForTest() != 1 {
		t.Errorf("Expected 1 available after 500, got %d", ps.AvailableCountForTest())
	}
}

// TestF3_AccountMarkedFailedOnSendError 验证 SendRequest 失败时账户被标记失败
func TestF3_AccountMarkedFailedOnSendError(t *testing.T) {
	// 连接到未监听的端口，SendRequest 会失败
	am := engine.NewAccountManager(&types.Config{
		RateLimit: types.RateLimitConfig{
			MaxRequestsPerMinute: 600,
			MinIntervalMs:        1,
			RetryIntervalMs:      5000,
		},
		Providers: []types.ProviderConfig{
			{
				Name:          "test",
				BaseURL:       "http://127.0.0.1:1", // 未监听端口
				APIKeyEntries: []string{"test-key"},
				Models:        map[string]string{"m1": "m1"},
				Timeout:       2,
			},
		},
	})
	s := NewServer(am)

	ps := am.GetProviderState("test")
	if ps.AvailableCountForTest() != 1 {
		t.Fatalf("Expected 1 available initially, got %d", ps.AvailableCountForTest())
	}

	body, _ := json.Marshal(types.ChatRequest{Model: "m1", Messages: []types.Message{{Role: "user", Content: "hi"}}})
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleChatCompletions(w, req)

	// SendRequest 失败 → 500
	if w.Result().StatusCode != 500 {
		t.Errorf("Expected 500 on send error, got %d", w.Result().StatusCode)
	}
	// 账户应被标记失败
	if ps.AvailableCountForTest() != 0 {
		t.Errorf("Expected 0 available after send error, got %d", ps.AvailableCountForTest())
	}
	if ps.FailedCountForTest() != 1 {
		t.Errorf("Expected 1 failed after send error, got %d", ps.FailedCountForTest())
	}
}

// TestF3_AccountAvailableOnStreamSuccess 验证流式响应成功后账户被标记可用
func TestF3_AccountAvailableOnStreamSuccess(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"hi"}}]}`)
		fmt.Fprintln(w, `data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`)
		flusher.Flush()
	}))
	defer upstream.Close()

	am := makeTestAM(t, upstream.URL)
	s := NewServer(am)

	ps := am.GetProviderState("test")

	body, _ := json.Marshal(types.ChatRequest{
		Model:    "m1",
		Messages: []types.Message{{Role: "user", Content: "hi"}},
		Stream:   true,
	})
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleChatCompletions(w, req)

	if w.Result().StatusCode != 200 {
		t.Errorf("Expected 200, got %d", w.Result().StatusCode)
	}
	// 流式成功后账户应可用
	if ps.AvailableCountForTest() != 1 {
		t.Errorf("Expected 1 available after stream success, got %d", ps.AvailableCountForTest())
	}
	if ps.FailedCountForTest() != 0 {
		t.Errorf("Expected 0 failed after stream success, got %d", ps.FailedCountForTest())
	}
}

// TestF3_AccountMarkedFailedOnStream401 验证流式 401 响应后账户被标记失败
func TestF3_AccountMarkedFailedOnStream401(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
	}))
	defer upstream.Close()

	am := makeTestAM(t, upstream.URL)
	s := NewServer(am)

	ps := am.GetProviderState("test")

	body, _ := json.Marshal(types.ChatRequest{
		Model:    "m1",
		Messages: []types.Message{{Role: "user", Content: "hi"}},
		Stream:   true,
	})
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleChatCompletions(w, req)

	if w.Result().StatusCode != 401 {
		t.Errorf("Expected 401, got %d", w.Result().StatusCode)
	}
	if ps.FailedCountForTest() != 1 {
		t.Errorf("Expected 1 failed after stream 401, got %d", ps.FailedCountForTest())
	}
}

// ============================================================
// F1+F2: 端到端集成测试（通过 AccountManager + Server）
// ============================================================

// TestE2E_RetryRecoversFromEOF 验证端到端场景下重试从连接关闭恢复
func TestE2E_RetryRecoversFromEOF(t *testing.T) {
	var callCount int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&callCount, 1) == 1 {
			// 首次关闭连接
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Fatal("server does not support hijacking")
			}
			conn, _, _ := hj.Hijack()
			conn.Close()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"recovered"},"finish_reason":"stop","index":0}]}`))
	}))
	defer upstream.Close()

	// 使用 RequestRetry=3 的配置
	am := engine.NewAccountManager(&types.Config{
		RequestRetry: 3,
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

	body, _ := json.Marshal(types.ChatRequest{Model: "m1", Messages: []types.Message{{Role: "user", Content: "hi"}}})
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleChatCompletions(w, req)

	resp := w.Result()
	if resp.StatusCode != 200 {
		t.Errorf("Expected 200 after retry recovery, got %d: %s", resp.StatusCode, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "recovered") {
		t.Errorf("Expected 'recovered' in response body, got %s", w.Body.String())
	}
}

// TestE2E_NoRetryWhenRetryIsZero 验证 RequestRetry=0 时端到端不重试
func TestE2E_NoRetryWhenRetryIsZero(t *testing.T) {
	var callCount int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("server does not support hijacking")
		}
		conn, _, _ := hj.Hijack()
		conn.Close()
	}))
	defer upstream.Close()

	am := engine.NewAccountManager(&types.Config{
		RequestRetry: 0, // 不重试
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

	body, _ := json.Marshal(types.ChatRequest{Model: "m1", Messages: []types.Message{{Role: "user", Content: "hi"}}})
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleChatCompletions(w, req)

	// 不重试 → 500
	if w.Result().StatusCode != 500 {
		t.Errorf("Expected 500 without retry, got %d", w.Result().StatusCode)
	}
	if callCount != 1 {
		t.Errorf("Expected upstream called once (no retry), got %d", callCount)
	}
}

// TestE2E_TransportReuseAcrossMultipleRequests 验证多个请求复用 Transport（连接池共享）
func TestE2E_TransportReuseAcrossMultipleRequests(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[]}`))
	}))
	defer upstream.Close()

	am := makeTestAM(t, upstream.URL)
	s := NewServer(am)

	// 发送 5 个请求
	for i := 0; i < 5; i++ {
		body, _ := json.Marshal(types.ChatRequest{Model: "m1", Messages: []types.Message{{Role: "user", Content: "hi"}}})
		req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.handleChatCompletions(w, req)

		if w.Result().StatusCode != 200 {
			t.Errorf("Request %d: expected 200, got %d", i+1, w.Result().StatusCode)
		}
	}

	// 所有请求应复用同一 Transport（通过 RequestHandler 的 TransportCount 验证）
	// AccountManager 内部的 requestHandler 是 engine.RequestHandler
	// 我们无法直接从 server 访问它，但可以通过 engine 测试验证（见 request_f1f2_test.go）
}

// TestE2E_TraceIDAndAccountStatusCombined 验证 traceID 和账户状态同时正确
func TestE2E_TraceIDAndAccountStatusCombined(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[]}`))
	}))
	defer upstream.Close()

	am := makeTestAM(t, upstream.URL)
	s := NewServer(am)

	ps := am.GetProviderState("test")

	// 发送请求
	body, _ := json.Marshal(types.ChatRequest{Model: "m1", Messages: []types.Message{{Role: "user", Content: "hi"}}})
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleChatCompletions(w, req)

	resp := w.Result()

	// F4: traceID 存在
	traceID := resp.Header.Get("X-Trace-ID")
	if traceID == "" {
		t.Error("F4: Expected non-empty X-Trace-ID")
	}

	// F3: 账户可用
	if ps.AvailableCountForTest() != 1 {
		t.Errorf("F3: Expected 1 available, got %d", ps.AvailableCountForTest())
	}
	if ps.FailedCountForTest() != 0 {
		t.Errorf("F3: Expected 0 failed, got %d", ps.FailedCountForTest())
	}
}

// TestE2E_AllFixesCombined 验证所有修复协同工作：
// F1 (Transport 复用) + F2 (重试) + F3 (账户状态) + F4 (traceID)
func TestE2E_AllFixesCombined(t *testing.T) {
	var callCount int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&callCount, 1) == 1 {
			// 首次关闭连接（触发 F2 重试）
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Fatal("server does not support hijacking")
			}
			conn, _, _ := hj.Hijack()
			conn.Close()
			return
		}
		// 第二次正常返回
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop","index":0}]}`))
	}))
	defer upstream.Close()

	am := engine.NewAccountManager(&types.Config{
		RequestRetry: 2,
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
	ps := am.GetProviderState("test")

	body, _ := json.Marshal(types.ChatRequest{Model: "m1", Messages: []types.Message{{Role: "user", Content: "hi"}}})
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleChatCompletions(w, req)

	resp := w.Result()

	// F2: 重试后成功
	if resp.StatusCode != 200 {
		t.Errorf("F2: Expected 200 after retry, got %d", resp.StatusCode)
	}
	if callCount < 2 {
		t.Errorf("F2: Expected upstream called >= 2, got %d", callCount)
	}

	// F3: 账户仍可用
	if ps.AvailableCountForTest() != 1 {
		t.Errorf("F3: Expected 1 available, got %d", ps.AvailableCountForTest())
	}

	// F4: traceID 存在
	traceID := resp.Header.Get("X-Trace-ID")
	if traceID == "" {
		t.Error("F4: Expected non-empty X-Trace-ID")
	}
	if !strings.HasPrefix(traceID, "req-") {
		t.Errorf("F4: Expected traceID prefix 'req-', got '%s'", traceID)
	}
}
