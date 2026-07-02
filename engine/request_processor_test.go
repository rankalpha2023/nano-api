package engine

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"nano-api/types"
)

// ============================================================
// ProcessChatCompletion 回归测试套件
//
// 这些测试直接调用 engine.AccountManager.ProcessChatCompletion，
// 验证 UI 层重构后业务逻辑下沉的正确性。
//
// 覆盖的关键场景：
//  1. 正常 200 响应 → 返回 result，账户未被标记失败
//  2. 401/403 响应 → AuthFailed=true，账户被标记失败
//  3. 5xx 响应 → 账户仍可用
//  4. SendRequest 失败 → err != nil，账户被标记失败
//  5. 无可用账户 → result == nil, err == nil
//  6. 模型参数默认值填充
//  7. 模型名映射（realModel）
//  8. 流式响应
//  9. Anthropic 风格请求（ChatRequest 由 core.AnthropicToOpenAI 转换）
// ============================================================

// makeProcessorTestAM 创建用于 ProcessChatCompletion 测试的 AccountManager
func makeProcessorTestAM(t *testing.T, upstreamURL string) *AccountManager {
	t.Helper()
	return NewAccountManager(&types.Config{
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

// TestProcessChatCompletion_Success 200 响应：返回 result 且账户未被标记失败
func TestProcessChatCompletion_Success(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop","index":0}]}`))
	}))
	defer upstream.Close()

	am := makeProcessorTestAM(t, upstream.URL)
	ps := am.GetProviderState("test")
	if ps.AvailableCountForTest() != 1 {
		t.Fatalf("Expected 1 available initially, got %d", ps.AvailableCountForTest())
	}

	req := &types.ChatRequest{
		Model:    "m1",
		Messages: []types.Message{{Role: "user", Content: "hi"}},
	}
	result, err := am.ProcessChatCompletion(req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("Expected non-nil result")
	}
	defer result.Response.Body.Close()

	if result.Response.StatusCode != 200 {
		t.Errorf("Expected 200, got %d", result.Response.StatusCode)
	}
	if result.AuthFailed {
		t.Error("Expected AuthFailed=false on 200")
	}
	if result.Account == nil {
		t.Error("Expected non-nil Account")
	}
	if result.Metrics == nil {
		t.Error("Expected non-nil Metrics")
	}
	// ProcessChatCompletion 不调用 MarkAccountAvailable，账户仍应可用
	if ps.AvailableCountForTest() != 1 {
		t.Errorf("Expected 1 available (MarkAccountAvailable not called), got %d", ps.AvailableCountForTest())
	}
	if ps.FailedCountForTest() != 0 {
		t.Errorf("Expected 0 failed on 200, got %d", ps.FailedCountForTest())
	}
}

// TestProcessChatCompletion_AuthFailed_401 401 响应：AuthFailed=true，账户被标记失败
func TestProcessChatCompletion_AuthFailed_401(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer upstream.Close()

	am := makeProcessorTestAM(t, upstream.URL)
	ps := am.GetProviderState("test")

	req := &types.ChatRequest{Model: "m1", Messages: []types.Message{{Role: "user", Content: "hi"}}}
	result, err := am.ProcessChatCompletion(req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("Expected non-nil result (401 still returns response for UI to forward)")
	}
	defer result.Response.Body.Close()

	if !result.AuthFailed {
		t.Error("Expected AuthFailed=true on 401")
	}
	if result.Response.StatusCode != 401 {
		t.Errorf("Expected 401 forwarded, got %d", result.Response.StatusCode)
	}
	if ps.AvailableCountForTest() != 0 {
		t.Errorf("Expected 0 available after 401, got %d", ps.AvailableCountForTest())
	}
	if ps.FailedCountForTest() != 1 {
		t.Errorf("Expected 1 failed after 401, got %d", ps.FailedCountForTest())
	}
}

// TestProcessChatCompletion_AuthFailed_403 403 响应：AuthFailed=true，账户被标记失败
func TestProcessChatCompletion_AuthFailed_403(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
	}))
	defer upstream.Close()

	am := makeProcessorTestAM(t, upstream.URL)
	ps := am.GetProviderState("test")

	req := &types.ChatRequest{Model: "m1", Messages: []types.Message{{Role: "user", Content: "hi"}}}
	result, err := am.ProcessChatCompletion(req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("Expected non-nil result")
	}
	defer result.Response.Body.Close()

	if !result.AuthFailed {
		t.Error("Expected AuthFailed=true on 403")
	}
	if ps.FailedCountForTest() != 1 {
		t.Errorf("Expected 1 failed after 403, got %d", ps.FailedCountForTest())
	}
}

// TestProcessChatCompletion_5xx_NoFail 5xx 响应：账户仍可用（上游错误非账户问题）
func TestProcessChatCompletion_5xx_NoFail(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte(`{"error":"internal"}`))
	}))
	defer upstream.Close()

	am := makeProcessorTestAM(t, upstream.URL)
	ps := am.GetProviderState("test")

	req := &types.ChatRequest{Model: "m1", Messages: []types.Message{{Role: "user", Content: "hi"}}}
	result, err := am.ProcessChatCompletion(req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("Expected non-nil result")
	}
	defer result.Response.Body.Close()

	if result.AuthFailed {
		t.Error("Expected AuthFailed=false on 500")
	}
	if ps.FailedCountForTest() != 0 {
		t.Errorf("Expected 0 failed after 500, got %d", ps.FailedCountForTest())
	}
	if ps.AvailableCountForTest() != 1 {
		t.Errorf("Expected 1 available after 500, got %d", ps.AvailableCountForTest())
	}
}

// TestProcessChatCompletion_SendError SendRequest 失败：err != nil，账户被标记失败
func TestProcessChatCompletion_SendError(t *testing.T) {
	// 连接到未监听端口
	am := NewAccountManager(&types.Config{
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
	ps := am.GetProviderState("test")

	req := &types.ChatRequest{Model: "m1", Messages: []types.Message{{Role: "user", Content: "hi"}}}
	result, err := am.ProcessChatCompletion(req)
	if err == nil {
		t.Fatal("Expected error on send failure")
	}
	if result != nil {
		t.Errorf("Expected nil result on send error, got non-nil")
	}
	if ps.AvailableCountForTest() != 0 {
		t.Errorf("Expected 0 available after send error, got %d", ps.AvailableCountForTest())
	}
	if ps.FailedCountForTest() != 1 {
		t.Errorf("Expected 1 failed after send error, got %d", ps.FailedCountForTest())
	}
}

// TestProcessChatCompletion_NoAvailableAccount 无可用账户：result == nil, err == nil
func TestProcessChatCompletion_NoAvailableAccount(t *testing.T) {
	am := NewAccountManager(&types.Config{
		Providers: []types.ProviderConfig{},
	})

	req := &types.ChatRequest{Model: "nonexistent", Messages: []types.Message{{Role: "user", Content: "hi"}}}
	result, err := am.ProcessChatCompletion(req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("Expected nil result when no provider available, got non-nil")
	}
}

// TestProcessChatCompletion_ModelDefaults 模型参数默认值填充
func TestProcessChatCompletion_ModelDefaults(t *testing.T) {
	var capturedBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		capturedBody = make([]byte, r.ContentLength)
		r.Body.Read(capturedBody)
		w.Write([]byte(`{"choices":[]}`))
	}))
	defer upstream.Close()

	am := NewAccountManager(&types.Config{
		RateLimit: types.RateLimitConfig{MaxRequestsPerMinute: 600, MinIntervalMs: 1, RetryIntervalMs: 5000},
		GlobalModelConfig: types.ModelConfig{
			DefaultTemperature: 0.7,
			DefaultTopP:        0.9,
			DefaultMaxTokens:   2048,
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

	req := &types.ChatRequest{Model: "m1", Messages: []types.Message{{Role: "user", Content: "hi"}}}
	result, err := am.ProcessChatCompletion(req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("Expected non-nil result")
	}
	defer result.Response.Body.Close()

	// 验证默认值被填充到请求中
	var sentBody map[string]interface{}
	if err := json.Unmarshal(capturedBody, &sentBody); err != nil {
		t.Fatalf("Failed to unmarshal sent body: %v", err)
	}
	if sentBody["temperature"].(float64) != 0.7 {
		t.Errorf("Expected temperature=0.7, got %v", sentBody["temperature"])
	}
	if sentBody["top_p"].(float64) != 0.9 {
		t.Errorf("Expected top_p=0.9, got %v", sentBody["top_p"])
	}
	if int(sentBody["max_tokens"].(float64)) != 2048 {
		t.Errorf("Expected max_tokens=2048, got %v", sentBody["max_tokens"])
	}
}

// TestProcessChatCompletion_ModelMapping 模型名映射（realModel）
func TestProcessChatCompletion_ModelMapping(t *testing.T) {
	var capturedModel string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		capturedModel = body["model"].(string)
		w.Write([]byte(`{"choices":[]}`))
	}))
	defer upstream.Close()

	am := NewAccountManager(&types.Config{
		RateLimit: types.RateLimitConfig{MaxRequestsPerMinute: 600, MinIntervalMs: 1, RetryIntervalMs: 5000},
		Providers: []types.ProviderConfig{
			{
				Name:          "test",
				BaseURL:       upstream.URL,
				APIKeyEntries: []string{"test-key"},
				// alias "alias-1" → real "real-m1"
				Models:  map[string]string{"alias-1": "real-m1"},
				Timeout: 10,
			},
		},
	})

	req := &types.ChatRequest{Model: "alias-1", Messages: []types.Message{{Role: "user", Content: "hi"}}}
	result, err := am.ProcessChatCompletion(req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("Expected non-nil result")
	}
	defer result.Response.Body.Close()

	// 上游应收到 real-m1（被映射后的真实模型名）
	if capturedModel != "real-m1" {
		t.Errorf("Expected upstream to receive 'real-m1', got '%s'", capturedModel)
	}
	// 请求的 Model 也应被更新
	if req.Model != "real-m1" {
		t.Errorf("Expected req.Model to be updated to 'real-m1', got '%s'", req.Model)
	}
}

// TestProcessChatCompletion_Streaming 流式响应也能正常处理
func TestProcessChatCompletion_Streaming(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"hi"}}]}`)
		fmt.Fprintln(w, `data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`)
		flusher.Flush()
	}))
	defer upstream.Close()

	am := makeProcessorTestAM(t, upstream.URL)

	req := &types.ChatRequest{
		Model:    "m1",
		Messages: []types.Message{{Role: "user", Content: "hi"}},
		Stream:   true,
	}
	result, err := am.ProcessChatCompletion(req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("Expected non-nil result")
	}
	defer result.Response.Body.Close()

	if result.Response.StatusCode != 200 {
		t.Errorf("Expected 200, got %d", result.Response.StatusCode)
	}
	if !strings.Contains(result.Response.Header.Get("Content-Type"), "text/event-stream") {
		t.Errorf("Expected stream content-type, got %s", result.Response.Header.Get("Content-Type"))
	}
}

// TestProcessChatCompletion_ExtraFields ExtraFields 被正确合并到上游请求
func TestProcessChatCompletion_ExtraFields(t *testing.T) {
	var capturedBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		capturedBody = make([]byte, r.ContentLength)
		r.Body.Read(capturedBody)
		w.Write([]byte(`{"choices":[]}`))
	}))
	defer upstream.Close()

	am := NewAccountManager(&types.Config{
		RateLimit: types.RateLimitConfig{MaxRequestsPerMinute: 600, MinIntervalMs: 1, RetryIntervalMs: 5000},
		Providers: []types.ProviderConfig{
			{
				Name:          "test",
				BaseURL:       upstream.URL,
				APIKeyEntries: []string{"test-key"},
				Models:        map[string]string{"m1": "m1"},
				Timeout:       10,
				ExtraFields: map[string]interface{}{
					"thinking_enabled": true,
				},
			},
		},
	})

	req := &types.ChatRequest{Model: "m1", Messages: []types.Message{{Role: "user", Content: "hi"}}}
	result, err := am.ProcessChatCompletion(req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("Expected non-nil result")
	}
	defer result.Response.Body.Close()

	var sentBody map[string]interface{}
	if err := json.Unmarshal(capturedBody, &sentBody); err != nil {
		t.Fatalf("Failed to unmarshal sent body: %v", err)
	}
	if sentBody["thinking_enabled"] != true {
		t.Errorf("Expected thinking_enabled=true, got %v", sentBody["thinking_enabled"])
	}
}

// TestProcessChatCompletion_CustomHeaders 自定义请求头被正确发送到上游
func TestProcessChatCompletion_CustomHeaders(t *testing.T) {
	var capturedHeader string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		capturedHeader = r.Header.Get("X-Custom-Header")
		w.Write([]byte(`{"choices":[]}`))
	}))
	defer upstream.Close()

	am := NewAccountManager(&types.Config{
		RateLimit: types.RateLimitConfig{MaxRequestsPerMinute: 600, MinIntervalMs: 1, RetryIntervalMs: 5000},
		Providers: []types.ProviderConfig{
			{
				Name:          "test",
				BaseURL:       upstream.URL,
				APIKeyEntries: []string{"test-key"},
				Models:        map[string]string{"m1": "m1"},
				Timeout:       10,
				Headers:       map[string]string{"X-Custom-Header": "custom-value"},
			},
		},
	})

	req := &types.ChatRequest{Model: "m1", Messages: []types.Message{{Role: "user", Content: "hi"}}}
	result, err := am.ProcessChatCompletion(req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("Expected non-nil result")
	}
	defer result.Response.Body.Close()

	if capturedHeader != "custom-value" {
		t.Errorf("Expected X-Custom-Header=custom-value, got '%s'", capturedHeader)
	}
}
