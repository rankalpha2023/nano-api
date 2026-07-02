package types

import (
	"encoding/json"
	"testing"
)

// ============================================================
// JSON 序列化/反序列化回归测试
//
// Types 层按架构规约严禁包含任何逻辑，因此本层无可执行语句
// （go test -cover 显示 "no statements"），覆盖率技术上为 N/A。
// 这些测试为 Data Schema 的 JSON tag 提供回归保护：确保字段名、
// omitempty 行为符合 OpenAI / Anthropic API 规范。
// ============================================================

// TestChatRequest_JSONRoundTrip 验证 ChatRequest 的 JSON tag
func TestChatRequest_JSONRoundTrip(t *testing.T) {
	seed := 42
	original := ChatRequest{
		Model:            "gpt-4",
		Temperature:      0.7,
		TopP:             0.9,
		MaxTokens:        1024,
		Stream:           true,
		User:             "u1",
		FrequencyPenalty: 0.1,
		PresencePenalty:  0.2,
		Seed:             &seed,
		Messages: []Message{
			{Role: "user", Content: "hi"},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	// 关键字段必须使用 snake_case（OpenAI 规范）
	requiredKeys := []string{"model", "temperature", "top_p", "max_tokens", "stream", "user", "frequency_penalty", "presence_penalty", "seed"}
	for _, k := range requiredKeys {
		if _, ok := decoded[k]; !ok {
			t.Errorf("Expected key '%s' in JSON, got: %v", k, decoded)
		}
	}
}

// TestChatRequest_OmitEmpty 验证 omitempty 字段在零值时不输出
func TestChatRequest_OmitEmpty(t *testing.T) {
	original := ChatRequest{Model: "m1", MaxTokens: 100, Messages: []Message{{Role: "user", Content: "hi"}}}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	omitKeys := []string{"tools", "tool_choice", "user", "stop", "frequency_penalty", "presence_penalty", "logit_bias", "response_format", "seed", "extra_body"}
	for _, k := range omitKeys {
		if _, exists := decoded[k]; exists {
			t.Errorf("Key '%s' should be omitted when empty, but present", k)
		}
	}
}

// TestAnthropicMessagesRequest_JSONTags 验证 Anthropic 请求的 JSON tag
func TestAnthropicMessagesRequest_JSONTags(t *testing.T) {
	temp := 0.5
	topP := 0.9
	topK := 40
	original := AnthropicMessagesRequest{
		Model:         "claude-sonnet-4-6",
		MaxTokens:     1024,
		Temperature:   &temp,
		TopP:          &topP,
		TopK:          &topK,
		StopSequences: []string{"stop1"},
		Stream:        true,
		Tools: []AnthropicTool{
			{Name: "tool1", Description: "desc", InputSchema: map[string]interface{}{"type": "object"}},
		},
		ToolChoice: map[string]interface{}{"type": "auto"},
		Metadata:   map[string]interface{}{"user_id": "u1"},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	// Anthropic 规范要求 snake_case
	requiredKeys := []string{"model", "max_tokens", "temperature", "top_p", "top_k", "stop_sequences", "stream", "tools", "tool_choice", "metadata"}
	for _, k := range requiredKeys {
		if _, ok := decoded[k]; !ok {
			t.Errorf("Expected key '%s' in Anthropic JSON, got: %v", k, decoded)
		}
	}
}

// TestAnthropicMessagesResponse_JSONTags 验证 Anthropic 响应的 JSON tag
func TestAnthropicMessagesResponse_JSONTags(t *testing.T) {
	stopSeq := "done"
	original := AnthropicMessagesResponse{
		ID:           "msg-1",
		Type:         "message",
		Role:         "assistant",
		Model:        "claude-sonnet-4-6",
		StopReason:   "end_turn",
		StopSequence: &stopSeq,
		Usage: AnthropicUsage{
			InputTokens:              10,
			OutputTokens:             5,
			CacheCreationInputTokens: 2,
			CacheReadInputTokens:     1,
		},
		Content: []AnthropicContentBlock{
			{Type: "text", Text: "hello"},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	requiredKeys := []string{"id", "type", "role", "model", "stop_reason", "stop_sequence", "usage", "content"}
	for _, k := range requiredKeys {
		if _, ok := decoded[k]; !ok {
			t.Errorf("Expected key '%s' in Anthropic response JSON, got: %v", k, decoded)
		}
	}
}

// TestAccount_JSONRoundTrip 验证 Account 的 JSON 序列化
func TestAccount_JSONRoundTrip(t *testing.T) {
	original := Account{
		ProviderName: "p1",
		BaseURL:      "http://x",
		APIKey:       "k1",
		Timeout:      30,
		Proxy:        "http://proxy",
		Headers:      map[string]string{"X-Custom": "v"},
		ExtraFields:  map[string]interface{}{"k": "v"},
		Status:       AccountStatusAvailable,
		RequestCount: 5,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded Account
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.ProviderName != "p1" || decoded.BaseURL != "http://x" || decoded.Status != AccountStatusAvailable {
		t.Errorf("Round-trip mismatch: %+v", decoded)
	}
}

// TestModelInfo_JSONTags 验证 ModelInfo 的 JSON tag（OpenAI/Anthropic /v1/models 格式）
func TestModelInfo_JSONTags(t *testing.T) {
	original := ModelInfo{
		ID:          "gpt-4",
		Object:      "model",
		Created:     1700000000,
		OwnedBy:     "openai",
		DisplayName: "GPT-4",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	// OwnedBy 必须序列化为 owned_by（snake_case）
	if decoded["owned_by"] != "openai" {
		t.Errorf("Expected owned_by='openai', got %v", decoded["owned_by"])
	}
	if decoded["display_name"] != "GPT-4" {
		t.Errorf("Expected display_name='GPT-4', got %v", decoded["display_name"])
	}
}

// TestModelListResponse_EmptyData 验证空列表也能正确序列化
func TestModelListResponse_EmptyData(t *testing.T) {
	original := ModelListResponse{
		Object: "list",
		Data:   []ModelInfo{},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded ModelListResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.Object != "list" || len(decoded.Data) != 0 {
		t.Errorf("Round-trip mismatch: %+v", decoded)
	}
}

// TestUsage_JSONTags 验证 Usage 字段命名
func TestUsage_JSONTags(t *testing.T) {
	original := Usage{
		PromptTokens:     10,
		CompletionTokens: 20,
		TotalTokens:      30,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded["prompt_tokens"].(float64) != 10 {
		t.Errorf("Expected prompt_tokens=10, got %v", decoded["prompt_tokens"])
	}
	if decoded["completion_tokens"].(float64) != 20 {
		t.Errorf("Expected completion_tokens=20, got %v", decoded["completion_tokens"])
	}
	if decoded["total_tokens"].(float64) != 30 {
		t.Errorf("Expected total_tokens=30, got %v", decoded["total_tokens"])
	}
}
