package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"nano-api/engine"
	"nano-api/types"
)

// ============================================================
// /v1/messages (Anthropic Messages API) 测试套件
//
// handleAnthropicMessages 是 UI 层处理 Anthropic Messages API 的入口。
// UI 层职责（规约：禁止超过 3 行业务逻辑计算）：
//   - HTTP 方法/CORS 检查、请求体读取与 JSON 解析（HTTP I/O）
//   - 调用 core.AnthropicToOpenAI 转换请求格式（1 行调用纯函数）
//   - 调用 Engine 层 ProcessChatCompletion 编排业务流程（1 行）
//   - 调用 core.OpenAIToAnthropicResponse 转换响应格式（1 行调用纯函数）
//   - 成功后调用 MarkAccountAvailable（1 行副作用调度）
// ============================================================

// makeAnthropicTestAM 创建带 httptest 上游的 AccountManager（用于 Anthropic 测试）
func makeAnthropicTestAM(t *testing.T, upstreamURL string) *engine.AccountManager {
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
				Models:        map[string]string{"claude-sonnet-4-6": "claude-sonnet-4-6"},
				Timeout:       10,
			},
		},
	})
}

// makeAnthropicRequest 构造一个合法的 Anthropic Messages 请求体
func makeAnthropicRequest(model string, stream bool) []byte {
	body, _ := json.Marshal(types.AnthropicMessagesRequest{
		Model:     model,
		MaxTokens: 100,
		Messages: []types.AnthropicMessage{
			{Role: "user", Content: "hello"},
		},
		Stream: stream,
	})
	return body
}

// ============================================================
// CORS / HTTP 方法分支
// ============================================================

// TestHandleAnthropicMessages_OPTIONS OPTIONS 预检请求
func TestHandleAnthropicMessages_OPTIONS(t *testing.T) {
	am := engine.NewAccountManager(&types.Config{})
	s := NewServer(am)

	req := httptest.NewRequest("OPTIONS", "/v1/messages", nil)
	w := httptest.NewRecorder()
	s.handleAnthropicMessages(w, req)

	resp := w.Result()
	if resp.StatusCode != 200 {
		t.Errorf("Expected 200 for OPTIONS, got %d", resp.StatusCode)
	}
	// 验证 CORS 头
	if resp.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("Expected CORS origin '*', got '%s'", resp.Header.Get("Access-Control-Allow-Origin"))
	}
	// Anthropic 端点应额外允许 x-api-key 和 anthropic-version
	allowHeaders := resp.Header.Get("Access-Control-Allow-Headers")
	if !strings.Contains(allowHeaders, "x-api-key") {
		t.Errorf("Expected 'x-api-key' in Allow-Headers, got '%s'", allowHeaders)
	}
	if !strings.Contains(allowHeaders, "anthropic-version") {
		t.Errorf("Expected 'anthropic-version' in Allow-Headers, got '%s'", allowHeaders)
	}
}

// TestHandleAnthropicMessages_MethodNotAllowed 非 POST 方法返回 405
func TestHandleAnthropicMessages_MethodNotAllowed(t *testing.T) {
	am := engine.NewAccountManager(&types.Config{})
	s := NewServer(am)

	for _, method := range []string{"GET", "PUT", "DELETE", "PATCH"} {
		req := httptest.NewRequest(method, "/v1/messages", nil)
		w := httptest.NewRecorder()
		s.handleAnthropicMessages(w, req)

		if w.Result().StatusCode != 405 {
			t.Errorf("Method %s: expected 405, got %d", method, w.Result().StatusCode)
		}
	}
}

// ============================================================
// 请求体解析错误分支
// ============================================================

// TestHandleAnthropicMessages_InvalidJSON 无效 JSON 返回 400
func TestHandleAnthropicMessages_InvalidJSON(t *testing.T) {
	am := engine.NewAccountManager(&types.Config{})
	s := NewServer(am)

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader([]byte(`not json`)))
	w := httptest.NewRecorder()
	s.handleAnthropicMessages(w, req)

	if w.Result().StatusCode != 400 {
		t.Errorf("Expected 400 for invalid JSON, got %d", w.Result().StatusCode)
	}
}

// TestHandleAnthropicMessages_EmptyBody 空请求体返回 400（JSON 解析失败）
func TestHandleAnthropicMessages_EmptyBody(t *testing.T) {
	am := engine.NewAccountManager(&types.Config{})
	s := NewServer(am)

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader([]byte{}))
	w := httptest.NewRecorder()
	s.handleAnthropicMessages(w, req)

	if w.Result().StatusCode != 400 {
		t.Errorf("Expected 400 for empty body, got %d", w.Result().StatusCode)
	}
}

// ============================================================
// 账户/Provider 错误分支
// ============================================================

// TestHandleAnthropicMessages_NoAvailableAccount 无可用账户返回 503
func TestHandleAnthropicMessages_NoAvailableAccount(t *testing.T) {
	am := engine.NewAccountManager(&types.Config{
		Providers: []types.ProviderConfig{},
	})
	s := NewServer(am)

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(makeAnthropicRequest("nonexistent", false)))
	w := httptest.NewRecorder()
	s.handleAnthropicMessages(w, req)

	if w.Result().StatusCode != 503 {
		t.Errorf("Expected 503 for no available account, got %d", w.Result().StatusCode)
	}
}

// TestHandleAnthropicMessages_SendError 上游发送失败返回 500
func TestHandleAnthropicMessages_SendError(t *testing.T) {
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
				Models:        map[string]string{"claude-sonnet-4-6": "claude-sonnet-4-6"},
				Timeout:       2,
			},
		},
	})
	s := NewServer(am)

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(makeAnthropicRequest("claude-sonnet-4-6", false)))
	w := httptest.NewRecorder()
	s.handleAnthropicMessages(w, req)

	if w.Result().StatusCode != 500 {
		t.Errorf("Expected 500 on send error, got %d", w.Result().StatusCode)
	}
}

// ============================================================
// 正常响应分支
// ============================================================

// TestHandleAnthropicMessages_Success 正常响应：OpenAI → Anthropic 格式转换
func TestHandleAnthropicMessages_Success(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := types.ChatResponse{
			ID:      "resp-1",
			Object:  "chat.completion",
			Model:   "claude-sonnet-4-6",
			Choices: []types.Choice{{Index: 0, Message: types.Message{Role: "assistant", Content: "Hello!"}, FinishReason: "stop"}},
			Usage:   &types.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer upstream.Close()

	am := makeAnthropicTestAM(t, upstream.URL)
	s := NewServer(am)

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(makeAnthropicRequest("claude-sonnet-4-6", false)))
	w := httptest.NewRecorder()
	s.handleAnthropicMessages(w, req)

	resp := w.Result()
	if resp.StatusCode != 200 {
		t.Errorf("Expected 200, got %d: %s", resp.StatusCode, w.Body.String())
	}

	// 验证返回的是 Anthropic 格式
	var anthropicResp types.AnthropicMessagesResponse
	if err := json.NewDecoder(resp.Body).Decode(&anthropicResp); err != nil {
		t.Fatalf("Failed to decode Anthropic response: %v", err)
	}
	if anthropicResp.Type != "message" {
		t.Errorf("Expected type='message', got '%s'", anthropicResp.Type)
	}
	if anthropicResp.Role != "assistant" {
		t.Errorf("Expected role='assistant', got '%s'", anthropicResp.Role)
	}
	// model 应为原始请求中的 model（originalModel），而非上游返回的
	if anthropicResp.Model != "claude-sonnet-4-6" {
		t.Errorf("Expected model='claude-sonnet-4-6', got '%s'", anthropicResp.Model)
	}
	if anthropicResp.StopReason != "end_turn" {
		t.Errorf("Expected stop_reason='end_turn', got '%s'", anthropicResp.StopReason)
	}
	if len(anthropicResp.Content) != 1 {
		t.Fatalf("Expected 1 content block, got %d", len(anthropicResp.Content))
	}
	if anthropicResp.Content[0].Type != "text" || anthropicResp.Content[0].Text != "Hello!" {
		t.Errorf("Unexpected content block: %+v", anthropicResp.Content[0])
	}
	if anthropicResp.Usage.InputTokens != 10 || anthropicResp.Usage.OutputTokens != 5 {
		t.Errorf("Unexpected usage: %+v", anthropicResp.Usage)
	}
}

// TestHandleAnthropicMessages_SuccessWithModelAlias 模型别名映射
func TestHandleAnthropicMessages_SuccessWithModelAlias(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// 上游应收到真实模型名 real-claude
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		if body["model"] != "real-claude" {
			t.Errorf("Expected upstream model='real-claude', got '%v'", body["model"])
		}
		resp := types.ChatResponse{
			ID:      "resp-1",
			Model:   "real-claude",
			Choices: []types.Choice{{Index: 0, Message: types.Message{Role: "assistant", Content: "ok"}, FinishReason: "stop"}},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer upstream.Close()

	am := engine.NewAccountManager(&types.Config{
		RateLimit: types.RateLimitConfig{MaxRequestsPerMinute: 600, MinIntervalMs: 1, RetryIntervalMs: 5000},
		Providers: []types.ProviderConfig{
			{
				Name:          "test",
				BaseURL:       upstream.URL,
				APIKeyEntries: []string{"test-key"},
				Models:        map[string]string{"claude-sonnet-4-6": "real-claude"},
				Timeout:       10,
			},
		},
	})
	s := NewServer(am)

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(makeAnthropicRequest("claude-sonnet-4-6", false)))
	w := httptest.NewRecorder()
	s.handleAnthropicMessages(w, req)

	resp := w.Result()
	if resp.StatusCode != 200 {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}

	var anthropicResp types.AnthropicMessagesResponse
	json.NewDecoder(resp.Body).Decode(&anthropicResp)
	// 响应的 model 应为原始请求的别名（originalModel），而非上游的 real-claude
	if anthropicResp.Model != "claude-sonnet-4-6" {
		t.Errorf("Expected response model='claude-sonnet-4-6' (original), got '%s'", anthropicResp.Model)
	}
}

// TestHandleAnthropicMessages_ToolCalls 工具调用响应转换
func TestHandleAnthropicMessages_ToolCalls(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := types.ChatResponse{
			ID:    "resp-1",
			Model: "claude-sonnet-4-6",
			Choices: []types.Choice{{
				Index: 0,
				Message: types.Message{
					Role: "assistant",
					ToolCalls: []types.ToolCall{{
						ID:   "call_1",
						Type: "function",
						Function: types.FunctionCall{
							Name:      "get_weather",
							Arguments: `{"city":"SF"}`,
						},
					}},
				},
				FinishReason: "tool_calls",
			}},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer upstream.Close()

	am := makeAnthropicTestAM(t, upstream.URL)
	s := NewServer(am)

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(makeAnthropicRequest("claude-sonnet-4-6", false)))
	w := httptest.NewRecorder()
	s.handleAnthropicMessages(w, req)

	resp := w.Result()
	if resp.StatusCode != 200 {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}

	var anthropicResp types.AnthropicMessagesResponse
	json.NewDecoder(resp.Body).Decode(&anthropicResp)
	if anthropicResp.StopReason != "tool_use" {
		t.Errorf("Expected stop_reason='tool_use', got '%s'", anthropicResp.StopReason)
	}
	found := false
	for _, block := range anthropicResp.Content {
		if block.Type == "tool_use" {
			found = true
			if block.ID != "call_1" {
				t.Errorf("Expected id='call_1', got '%s'", block.ID)
			}
			if block.Name != "get_weather" {
				t.Errorf("Expected name='get_weather', got '%s'", block.Name)
			}
		}
	}
	if !found {
		t.Error("Expected to find a tool_use content block")
	}
}

// TestHandleAnthropicMessages_WithTools 请求中带 tools 定义
func TestHandleAnthropicMessages_WithTools(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// 验证上游收到了转换后的 tools
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		tools, ok := body["tools"].([]interface{})
		if !ok || len(tools) != 1 {
			t.Errorf("Expected 1 tool in upstream request, got %v", body["tools"])
		}
		resp := types.ChatResponse{
			ID:      "resp-1",
			Model:   "claude-sonnet-4-6",
			Choices: []types.Choice{{Index: 0, Message: types.Message{Role: "assistant", Content: "ok"}, FinishReason: "stop"}},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer upstream.Close()

	am := makeAnthropicTestAM(t, upstream.URL)
	s := NewServer(am)

	ar := types.AnthropicMessagesRequest{
		Model:     "claude-sonnet-4-6",
		MaxTokens: 100,
		Tools: []types.AnthropicTool{
			{Name: "get_weather", Description: "Get weather", InputSchema: map[string]interface{}{"type": "object"}},
		},
		ToolChoice: map[string]interface{}{"type": "auto"},
		Messages: []types.AnthropicMessage{
			{Role: "user", Content: "What's the weather?"},
		},
	}
	body, _ := json.Marshal(ar)
	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleAnthropicMessages(w, req)

	if w.Result().StatusCode != 200 {
		t.Errorf("Expected 200, got %d", w.Result().StatusCode)
	}
}

// TestHandleAnthropicMessages_WithSystemPrompt 带 system prompt
func TestHandleAnthropicMessages_WithSystemPrompt(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		messages, _ := body["messages"].([]interface{})
		// 应有 2 条消息：system + user
		if len(messages) != 2 {
			t.Errorf("Expected 2 messages (system+user), got %d", len(messages))
		}
		resp := types.ChatResponse{
			ID:      "resp-1",
			Model:   "claude-sonnet-4-6",
			Choices: []types.Choice{{Index: 0, Message: types.Message{Role: "assistant", Content: "ok"}, FinishReason: "stop"}},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer upstream.Close()

	am := makeAnthropicTestAM(t, upstream.URL)
	s := NewServer(am)

	ar := types.AnthropicMessagesRequest{
		Model:     "claude-sonnet-4-6",
		MaxTokens: 100,
		System:    "You are helpful",
		Messages: []types.AnthropicMessage{
			{Role: "user", Content: "hi"},
		},
	}
	body, _ := json.Marshal(ar)
	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleAnthropicMessages(w, req)

	if w.Result().StatusCode != 200 {
		t.Errorf("Expected 200, got %d", w.Result().StatusCode)
	}
}

// ============================================================
// 错误响应转发分支
// ============================================================

// TestHandleAnthropicMessages_ConversionError OpenAI→Anthropic 转换失败时原始返回 OpenAI 格式
func TestHandleAnthropicMessages_ConversionError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// 返回无效的 OpenAI 响应（无 choices），会触发 OpenAIToAnthropicResponse 转换失败
		w.Write([]byte(`{"id":"r","model":"m","choices":[]}`))
	}))
	defer upstream.Close()

	am := makeAnthropicTestAM(t, upstream.URL)
	s := NewServer(am)

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(makeAnthropicRequest("claude-sonnet-4-6", false)))
	w := httptest.NewRecorder()
	s.handleAnthropicMessages(w, req)

	resp := w.Result()
	// 转换失败时返回上游原始状态码（200）和原始 body
	if resp.StatusCode != 200 {
		t.Errorf("Expected 200 (upstream status forwarded), got %d", resp.StatusCode)
	}
	// body 应为原始 OpenAI 格式（不是 Anthropic 格式）
	body := w.Body.String()
	if !strings.Contains(body, `"choices":[]`) {
		t.Errorf("Expected raw OpenAI body forwarded, got '%s'", body)
	}
}

// TestHandleAnthropicMessages_AuthFailed401 401 响应：账户标记失败，但仍转换并返回
func TestHandleAnthropicMessages_AuthFailed401(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer upstream.Close()

	am := makeAnthropicTestAM(t, upstream.URL)
	s := NewServer(am)
	ps := am.GetProviderState("test")

	if ps.AvailableCountForTest() != 1 {
		t.Fatalf("Expected 1 available initially, got %d", ps.AvailableCountForTest())
	}

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(makeAnthropicRequest("claude-sonnet-4-6", false)))
	w := httptest.NewRecorder()
	s.handleAnthropicMessages(w, req)

	// 401 时上游返回的 body 不是合法 OpenAI ChatResponse（含 choices），
	// OpenAIToAnthropicResponse 会失败 → 走"原始返回"分支，状态码为 401
	resp := w.Result()
	if resp.StatusCode != 401 {
		t.Errorf("Expected 401 forwarded, got %d", resp.StatusCode)
	}
	// 账户应被标记失败（ProcessChatCompletion 已标记）
	if ps.AvailableCountForTest() != 0 {
		t.Errorf("Expected 0 available after 401, got %d", ps.AvailableCountForTest())
	}
	if ps.FailedCountForTest() != 1 {
		t.Errorf("Expected 1 failed after 401, got %d", ps.FailedCountForTest())
	}
}

// TestHandleAnthropicMessages_5xxNoFail 5xx 响应：账户仍可用
func TestHandleAnthropicMessages_5xxNoFail(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte(`{"error":"internal"}`))
	}))
	defer upstream.Close()

	am := makeAnthropicTestAM(t, upstream.URL)
	s := NewServer(am)
	ps := am.GetProviderState("test")

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(makeAnthropicRequest("claude-sonnet-4-6", false)))
	w := httptest.NewRecorder()
	s.handleAnthropicMessages(w, req)

	// 5xx 时上游 body 不是合法 OpenAI ChatResponse → 转换失败 → 原始返回 500
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

// TestHandleAnthropicMessages_401WithValidOpenAIBody 401 + 合法 OpenAI body → 转换为 Anthropic 格式返回
func TestHandleAnthropicMessages_401WithValidOpenAIBody(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		// 返回合法的 OpenAI ChatResponse（含 choices），让 OpenAIToAnthropicResponse 转换成功
		resp := types.ChatResponse{
			ID:      "resp-1",
			Model:   "claude-sonnet-4-6",
			Choices: []types.Choice{{Index: 0, Message: types.Message{Role: "assistant", Content: "auth error"}, FinishReason: "stop"}},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer upstream.Close()

	am := makeAnthropicTestAM(t, upstream.URL)
	s := NewServer(am)
	ps := am.GetProviderState("test")

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(makeAnthropicRequest("claude-sonnet-4-6", false)))
	w := httptest.NewRecorder()
	s.handleAnthropicMessages(w, req)

	// 转换成功 → 状态码被强制设为 200（handleAnthropicMessages 中 WriteHeader(http.StatusOK)）
	if w.Result().StatusCode != 200 {
		t.Errorf("Expected 200 (Anthropic format), got %d", w.Result().StatusCode)
	}
	// 账户仍应被标记失败（401 → AuthFailed=true → ProcessChatCompletion 已标记）
	if ps.FailedCountForTest() != 1 {
		t.Errorf("Expected 1 failed after 401, got %d", ps.FailedCountForTest())
	}
}

// ============================================================
// 流式请求分支
// ============================================================

// TestHandleAnthropicMessages_StreamRequest 流式请求（Stream=true）
// 注：handleAnthropicMessages 不实现真正的流式转发，它会读取完整上游响应后转换为 Anthropic 格式。
// 此测试验证 Stream=true 的请求能正常处理。
func TestHandleAnthropicMessages_StreamRequest(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := types.ChatResponse{
			ID:      "resp-1",
			Model:   "claude-sonnet-4-6",
			Choices: []types.Choice{{Index: 0, Message: types.Message{Role: "assistant", Content: "streamed"}, FinishReason: "stop"}},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer upstream.Close()

	am := makeAnthropicTestAM(t, upstream.URL)
	s := NewServer(am)

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(makeAnthropicRequest("claude-sonnet-4-6", true)))
	w := httptest.NewRecorder()
	s.handleAnthropicMessages(w, req)

	if w.Result().StatusCode != 200 {
		t.Errorf("Expected 200, got %d", w.Result().StatusCode)
	}
	var anthropicResp types.AnthropicMessagesResponse
	json.NewDecoder(w.Result().Body).Decode(&anthropicResp)
	if len(anthropicResp.Content) != 1 || anthropicResp.Content[0].Text != "streamed" {
		t.Errorf("Unexpected response: %+v", anthropicResp.Content)
	}
}

// ============================================================
// 账户状态标记分支
// ============================================================

// TestHandleAnthropicMessages_AccountMarkedAvailableOnSuccess 成功响应后账户标记可用
func TestHandleAnthropicMessages_AccountMarkedAvailableOnSuccess(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := types.ChatResponse{
			ID:      "resp-1",
			Model:   "claude-sonnet-4-6",
			Choices: []types.Choice{{Index: 0, Message: types.Message{Role: "assistant", Content: "ok"}, FinishReason: "stop"}},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer upstream.Close()

	am := makeAnthropicTestAM(t, upstream.URL)
	s := NewServer(am)
	ps := am.GetProviderState("test")

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(makeAnthropicRequest("claude-sonnet-4-6", false)))
	w := httptest.NewRecorder()
	s.handleAnthropicMessages(w, req)

	if w.Result().StatusCode != 200 {
		t.Errorf("Expected 200, got %d", w.Result().StatusCode)
	}
	// 成功后 MarkAccountAvailable 被调用（!AuthFailed）
	if ps.AvailableCountForTest() != 1 {
		t.Errorf("Expected 1 available after success, got %d", ps.AvailableCountForTest())
	}
	if ps.FailedCountForTest() != 0 {
		t.Errorf("Expected 0 failed after success, got %d", ps.FailedCountForTest())
	}
}

// TestHandleAnthropicMessages_ContentResponseHeader 响应 Content-Type 为 application/json
func TestHandleAnthropicMessages_ContentResponseHeader(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := types.ChatResponse{
			ID:      "resp-1",
			Model:   "claude-sonnet-4-6",
			Choices: []types.Choice{{Index: 0, Message: types.Message{Role: "assistant", Content: "ok"}, FinishReason: "stop"}},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer upstream.Close()

	am := makeAnthropicTestAM(t, upstream.URL)
	s := NewServer(am)

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(makeAnthropicRequest("claude-sonnet-4-6", false)))
	w := httptest.NewRecorder()
	s.handleAnthropicMessages(w, req)

	resp := w.Result()
	if resp.Header.Get("Content-Type") != "application/json" {
		t.Errorf("Expected Content-Type='application/json', got '%s'", resp.Header.Get("Content-Type"))
	}
}

// TestHandleAnthropicMessages_InvalidJSONResponseHeader 转换失败时 Content-Type 仍为 application/json
func TestHandleAnthropicMessages_InvalidJSONResponseHeader(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// 无 choices → 转换失败 → 原始返回
		w.Write([]byte(`{"id":"r","model":"m","choices":[]}`))
	}))
	defer upstream.Close()

	am := makeAnthropicTestAM(t, upstream.URL)
	s := NewServer(am)

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(makeAnthropicRequest("claude-sonnet-4-6", false)))
	w := httptest.NewRecorder()
	s.handleAnthropicMessages(w, req)

	resp := w.Result()
	if resp.Header.Get("Content-Type") != "application/json" {
		t.Errorf("Expected Content-Type='application/json' on conversion error, got '%s'", resp.Header.Get("Content-Type"))
	}
}
