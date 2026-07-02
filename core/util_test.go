package core

import (
	"errors"
	"strings"
	"testing"

	"nano-api/types"
)

// ============================================================
// MaskAPIKey 回归测试套件（纯函数）
// ============================================================

// TestMaskAPIKey_TypicalCase 典型 API Key 脱敏
func TestMaskAPIKey_TypicalCase(t *testing.T) {
	input := "sk-abcdef1234567890ghij"
	got := MaskAPIKey(input)
	// 期望: sk-ab + 多个* + ghij
	if !strings.HasPrefix(got, "sk-a") {
		t.Errorf("Expected prefix 'sk-a', got '%s'", got)
	}
	if !strings.HasSuffix(got, "ghij") {
		t.Errorf("Expected suffix 'ghij', got '%s'", got)
	}
	// 中间应全部是 *
	if strings.ContainsAny(got[len("sk-a"):len(got)-len("ghij")], "abcdef1234567890") {
		t.Errorf("Middle part should be all '*', got '%s'", got)
	}
}

// TestMaskAPIKey_ExactlyEightChars 恰好 8 字符：不脱敏
func TestMaskAPIKey_ExactlyEightChars(t *testing.T) {
	input := "12345678"
	got := MaskAPIKey(input)
	if got != input {
		t.Errorf("Expected unchanged for 8-char key, got '%s'", got)
	}
}

// TestMaskAPIKey_ShortKey 少于 8 字符：不脱敏
func TestMaskAPIKey_ShortKey(t *testing.T) {
	cases := []string{"", "a", "ab", "1234567"}
	for _, input := range cases {
		got := MaskAPIKey(input)
		if got != input {
			t.Errorf("Expected unchanged for short key '%s', got '%s'", input, got)
		}
	}
}

// TestMaskAPIKey_LongKey 长 Key 应保留首 4 + 末 4 + 中间 *
func TestMaskAPIKey_LongKey(t *testing.T) {
	input := "sk-very-long-api-key-1234567890-abcdefghij"
	got := MaskAPIKey(input)
	if len(got) != len(input) {
		t.Errorf("Expected length %d, got %d", len(input), len(got))
	}
	if got[:4] != input[:4] {
		t.Errorf("Expected prefix '%s', got '%s'", input[:4], got[:4])
	}
	if got[len(got)-4:] != input[len(input)-4:] {
		t.Errorf("Expected suffix '%s', got '%s'", input[len(input)-4:], got[len(got)-4:])
	}
	// 中间所有字符都应是 *
	for i := 4; i < len(got)-4; i++ {
		if got[i] != '*' {
			t.Errorf("Expected '*' at position %d, got '%c'", i, got[i])
		}
	}
}

// TestMaskAPIKey_PreservesLength 脱敏前后长度一致
func TestMaskAPIKey_PreservesLength(t *testing.T) {
	cases := []string{
		"sk-1234567890abcdef",
		"sk-1234567890abcdefghijklmnopqrstuvwxyz",
		"123456789",
	}
	for _, input := range cases {
		got := MaskAPIKey(input)
		if len(got) != len(input) {
			t.Errorf("Length mismatch: input=%d, output=%d (input='%s')", len(input), len(got), input)
		}
	}
}

// TestMaskAPIKey_DoesNotLeakMiddle 中间部分不应泄露原字符
func TestMaskAPIKey_DoesNotLeakMiddle(t *testing.T) {
	// 输入中间部分包含唯一标识字符
	input := "sk-XXXXXXXXXX-SECRET-MIDDLE-YYYYYYYYYY-zend"
	got := MaskAPIKey(input)
	if strings.Contains(got, "SECRET") {
		t.Errorf("Middle 'SECRET' leaked into output: '%s'", got)
	}
	if strings.Contains(got, "MIDDLE") {
		t.Errorf("Middle 'MIDDLE' leaked into output: '%s'", got)
	}
}

// ============================================================
// IsRetryableConnErr 回归测试套件（纯函数）
// ============================================================

// TestIsRetryableConnErr_EOF
func TestIsRetryableConnErr_EOF(t *testing.T) {
	err := errors.New(`Post "http://x": EOF`)
	if !IsRetryableConnErr(err) {
		t.Error("Expected EOF to be retryable")
	}
}

// TestIsRetryableConnErr_ConnectionReset
func TestIsRetryableConnErr_ConnectionReset(t *testing.T) {
	err := errors.New("read tcp: connection reset by peer")
	if !IsRetryableConnErr(err) {
		t.Error("Expected connection reset to be retryable")
	}
}

// TestIsRetryableConnErr_BrokenPipe
func TestIsRetryableConnErr_BrokenPipe(t *testing.T) {
	err := errors.New("write tcp: broken pipe")
	if !IsRetryableConnErr(err) {
		t.Error("Expected broken pipe to be retryable")
	}
}

// TestIsRetryableConnErr_TLSHandshake
func TestIsRetryableConnErr_TLSHandshake(t *testing.T) {
	err := errors.New("tls: handshake failure")
	if !IsRetryableConnErr(err) {
		t.Error("Expected tls handshake error to be retryable")
	}
}

// TestIsRetryableConnErr_DialTCP
func TestIsRetryableConnErr_DialTCP(t *testing.T) {
	err := errors.New("dial tcp: i/o timeout")
	if !IsRetryableConnErr(err) {
		t.Error("Expected dial tcp error to be retryable")
	}
}

// TestIsRetryableConnErr_ConnectionRefused
func TestIsRetryableConnErr_ConnectionRefused(t *testing.T) {
	err := errors.New("connect: connection refused")
	if !IsRetryableConnErr(err) {
		t.Error("Expected connection refused to be retryable")
	}
}

// TestIsRetryableConnErr_NilError nil 错误不可重试
func TestIsRetryableConnErr_NilError(t *testing.T) {
	if IsRetryableConnErr(nil) {
		t.Error("Expected nil error to not be retryable")
	}
}

// TestIsRetryableConnErr_NonRetryable 非连接级错误不可重试
func TestIsRetryableConnErr_NonRetryable(t *testing.T) {
	// 这些错误消息不含可重试关键字
	cases := []string{
		"context deadline exceeded",
		"internal server error",
		"permission denied",
		"invalid argument",
		"unexpected status code 500",
	}
	for _, msg := range cases {
		err := errors.New(msg)
		if IsRetryableConnErr(err) {
			t.Errorf("Expected '%s' to NOT be retryable", msg)
		}
	}
}

// TestIsRetryableConnErr_EmptyMessage 空错误消息不可重试
func TestIsRetryableConnErr_EmptyMessage(t *testing.T) {
	err := errors.New("")
	if IsRetryableConnErr(err) {
		t.Error("Expected empty error message to not be retryable")
	}
}

// ============================================================
// AnthropicToOpenAI / OpenAIToAnthropicResponse 纯函数回归测试
// ============================================================

// TestAnthropicToOpenAI_BasicRequest 基础 Anthropic 请求转换
func TestAnthropicToOpenAI_BasicRequest(t *testing.T) {
	temp := 0.7
	topP := 0.9
	ar := &types.AnthropicMessagesRequest{
		Model:       "claude-sonnet-4-6",
		MaxTokens:   1024,
		Temperature: &temp,
		TopP:        &topP,
		Stream:      false,
		Messages: []types.AnthropicMessage{
			{Role: "user", Content: "hello"},
		},
	}

	req := AnthropicToOpenAI(ar)
	if req.Model != "claude-sonnet-4-6" {
		t.Errorf("Expected model 'claude-sonnet-4-6', got '%s'", req.Model)
	}
	if req.MaxTokens != 1024 {
		t.Errorf("Expected max_tokens=1024, got %d", req.MaxTokens)
	}
	if req.Temperature != 0.7 {
		t.Errorf("Expected temperature=0.7, got %f", req.Temperature)
	}
	if req.TopP != 0.9 {
		t.Errorf("Expected top_p=0.9, got %f", req.TopP)
	}
	if len(req.Messages) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(req.Messages))
	}
	if req.Messages[0].Role != "user" || req.Messages[0].Content != "hello" {
		t.Errorf("Unexpected message: %+v", req.Messages[0])
	}
}

// TestAnthropicToOpenAI_WithSystemPrompt 带 system prompt 的请求转换
func TestAnthropicToOpenAI_WithSystemPrompt(t *testing.T) {
	ar := &types.AnthropicMessagesRequest{
		Model:     "claude-sonnet-4-6",
		MaxTokens: 100,
		System:    "You are a helpful assistant",
		Messages: []types.AnthropicMessage{
			{Role: "user", Content: "hi"},
		},
	}

	req := AnthropicToOpenAI(ar)
	// 应该有 2 条消息：system + user
	if len(req.Messages) != 2 {
		t.Fatalf("Expected 2 messages (system + user), got %d", len(req.Messages))
	}
	if req.Messages[0].Role != "system" {
		t.Errorf("Expected first message role='system', got '%s'", req.Messages[0].Role)
	}
	if req.Messages[0].Content != "You are a helpful assistant" {
		t.Errorf("Expected system content, got '%v'", req.Messages[0].Content)
	}
}

// TestAnthropicToOpenAI_SystemAsBlocks system 字段为 block 数组时的转换
func TestAnthropicToOpenAI_SystemAsBlocks(t *testing.T) {
	ar := &types.AnthropicMessagesRequest{
		Model:     "claude-sonnet-4-6",
		MaxTokens: 100,
		System: []interface{}{
			map[string]interface{}{"type": "text", "text": "Part 1"},
			map[string]interface{}{"type": "text", "text": "Part 2"},
		},
		Messages: []types.AnthropicMessage{
			{Role: "user", Content: "hi"},
		},
	}

	req := AnthropicToOpenAI(ar)
	if len(req.Messages) != 2 {
		t.Fatalf("Expected 2 messages, got %d", len(req.Messages))
	}
	if req.Messages[0].Role != "system" {
		t.Errorf("Expected system role, got '%s'", req.Messages[0].Role)
	}
	// 两个 text block 应用 \n 连接
	if req.Messages[0].Content != "Part 1\nPart 2" {
		t.Errorf("Expected combined system content, got '%v'", req.Messages[0].Content)
	}
}

// TestOpenAIToAnthropicResponse_Basic 基础响应转换
func TestOpenAIToAnthropicResponse_Basic(t *testing.T) {
	respBody := []byte(`{
		"id": "resp-1",
		"model": "m1",
		"choices": [{
			"index": 0,
			"message": {"role": "assistant", "content": "Hello!"},
			"finish_reason": "stop"
		}],
		"usage": {"prompt_tokens": 10, "completion_tokens": 5}
	}`)

	resp, err := OpenAIToAnthropicResponse(respBody, "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if resp.ID != "resp-1" {
		t.Errorf("Expected ID='resp-1', got '%s'", resp.ID)
	}
	if resp.Type != "message" {
		t.Errorf("Expected type='message', got '%s'", resp.Type)
	}
	if resp.Role != "assistant" {
		t.Errorf("Expected role='assistant', got '%s'", resp.Role)
	}
	if resp.Model != "claude-sonnet-4-6" {
		t.Errorf("Expected model='claude-sonnet-4-6' (override), got '%s'", resp.Model)
	}
	if resp.StopReason != "end_turn" {
		t.Errorf("Expected stop_reason='end_turn', got '%s'", resp.StopReason)
	}
	if len(resp.Content) != 1 {
		t.Fatalf("Expected 1 content block, got %d", len(resp.Content))
	}
	if resp.Content[0].Type != "text" || resp.Content[0].Text != "Hello!" {
		t.Errorf("Unexpected content block: %+v", resp.Content[0])
	}
	if resp.Usage.InputTokens != 10 || resp.Usage.OutputTokens != 5 {
		t.Errorf("Unexpected usage: %+v", resp.Usage)
	}
}

// TestOpenAIToAnthropicResponse_FinishReasonMapping 终止原因映射
func TestOpenAIToAnthropicResponse_FinishReasonMapping(t *testing.T) {
	cases := []struct {
		finishReason string
		expected     string
	}{
		{"stop", "end_turn"},
		{"length", "max_tokens"},
		{"tool_calls", "tool_use"},
		{"content_filter", "end_turn"},
	}
	for _, c := range cases {
		respBody := []byte(`{
			"id": "r",
			"model": "m",
			"choices": [{"index": 0, "message": {"role": "assistant", "content": "x"}, "finish_reason": "` + c.finishReason + `"}]
		}`)
		resp, err := OpenAIToAnthropicResponse(respBody, "")
		if err != nil {
			t.Fatalf("Unexpected error for finish_reason=%s: %v", c.finishReason, err)
		}
		if resp.StopReason != c.expected {
			t.Errorf("finish_reason='%s': expected stop_reason='%s', got '%s'",
				c.finishReason, c.expected, resp.StopReason)
		}
	}
}

// TestOpenAIToAnthropicResponse_ToolCalls 工具调用转换
func TestOpenAIToAnthropicResponse_ToolCalls(t *testing.T) {
	respBody := []byte(`{
		"id": "r",
		"model": "m",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"tool_calls": [{
					"id": "call_1",
					"type": "function",
					"function": {"name": "get_weather", "arguments": "{\"city\":\"SF\"}"}
				}]
			},
			"finish_reason": "tool_calls"
		}]
	}`)

	resp, err := OpenAIToAnthropicResponse(respBody, "")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if resp.StopReason != "tool_use" {
		t.Errorf("Expected stop_reason='tool_use', got '%s'", resp.StopReason)
	}
	// 应有 1 个 tool_use 内容块
	found := false
	for _, block := range resp.Content {
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

// TestOpenAIToAnthropicResponse_InvalidJSON 无效 JSON 应返回错误
func TestOpenAIToAnthropicResponse_InvalidJSON(t *testing.T) {
	_, err := OpenAIToAnthropicResponse([]byte(`not json`), "")
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}

// TestOpenAIToAnthropicResponse_NoChoices 空 choices 应返回错误
func TestOpenAIToAnthropicResponse_NoChoices(t *testing.T) {
	_, err := OpenAIToAnthropicResponse([]byte(`{"id":"r","model":"m","choices":[]}`), "")
	if err == nil {
		t.Error("Expected error for empty choices")
	}
}

// TestOpenAIToAnthropicResponse_ModelOverride modelOverride 优先于响应中的 model
func TestOpenAIToAnthropicResponse_ModelOverride(t *testing.T) {
	respBody := []byte(`{
		"id": "r",
		"model": "internal-model",
		"choices": [{"index": 0, "message": {"role": "assistant", "content": "x"}, "finish_reason": "stop"}]
	}`)

	// 提供 override，应使用 override 而非响应中的 "internal-model"
	resp, err := OpenAIToAnthropicResponse(respBody, "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if resp.Model != "claude-sonnet-4-6" {
		t.Errorf("Expected model='claude-sonnet-4-6' (override), got '%s'", resp.Model)
	}
}

// TestOpenAIToAnthropicResponse_EmptyModelOverride 空 override 时使用响应中的 model
func TestOpenAIToAnthropicResponse_EmptyModelOverride(t *testing.T) {
	respBody := []byte(`{
		"id": "r",
		"model": "response-model",
		"choices": [{"index": 0, "message": {"role": "assistant", "content": "x"}, "finish_reason": "stop"}]
	}`)

	resp, err := OpenAIToAnthropicResponse(respBody, "")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if resp.Model != "response-model" {
		t.Errorf("Expected model='response-model', got '%s'", resp.Model)
	}
}
