package core

import (
	"encoding/json"
	"testing"

	"nano-api/types"
)

// ============================================================
// AnthropicToOpenAI 补充测试：覆盖 tools / tool_choice / system 边缘分支
// ============================================================

// TestAnthropicToOpenAI_NilTempAndTopP 未设置 temperature/top_p 时不覆盖零值
func TestAnthropicToOpenAI_NilTempAndTopP(t *testing.T) {
	ar := &types.AnthropicMessagesRequest{
		Model:     "claude",
		MaxTokens: 100,
		Messages: []types.AnthropicMessage{
			{Role: "user", Content: "hi"},
		},
	}

	req := AnthropicToOpenAI(ar)
	if req.Temperature != 0 {
		t.Errorf("Expected temperature=0 (nil pointer), got %f", req.Temperature)
	}
	if req.TopP != 0 {
		t.Errorf("Expected top_p=0 (nil pointer), got %f", req.TopP)
	}
}

// TestAnthropicToOpenAI_WithTools 工具定义转换
func TestAnthropicToOpenAI_WithTools(t *testing.T) {
	ar := &types.AnthropicMessagesRequest{
		Model:     "claude",
		MaxTokens: 100,
		Tools: []types.AnthropicTool{
			{
				Name:        "get_weather",
				Description: "Get weather",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"city": map[string]interface{}{"type": "string"},
					},
				},
			},
		},
		Messages: []types.AnthropicMessage{
			{Role: "user", Content: "hi"},
		},
	}

	req := AnthropicToOpenAI(ar)
	if len(req.Tools) != 1 {
		t.Fatalf("Expected 1 tool, got %d", len(req.Tools))
	}
	tool := req.Tools[0]
	if tool.Type != "function" {
		t.Errorf("Expected tool type='function', got '%s'", tool.Type)
	}
	if tool.Function.Name != "get_weather" {
		t.Errorf("Expected name='get_weather', got '%s'", tool.Function.Name)
	}
	if tool.Function.Description != "Get weather" {
		t.Errorf("Expected description='Get weather', got '%s'", tool.Function.Description)
	}
	if tool.Function.Parameters == nil {
		t.Error("Expected non-nil Parameters")
	}
}

// TestAnthropicToOpenAI_WithToolChoice 工具选择字段透传
func TestAnthropicToOpenAI_WithToolChoice(t *testing.T) {
	toolChoice := map[string]interface{}{"type": "auto"}
	ar := &types.AnthropicMessagesRequest{
		Model:      "claude",
		MaxTokens:  100,
		ToolChoice: toolChoice,
		Messages: []types.AnthropicMessage{
			{Role: "user", Content: "hi"},
		},
	}

	req := AnthropicToOpenAI(ar)
	if req.ToolChoice == nil {
		t.Fatal("Expected non-nil ToolChoice")
	}
	tc, ok := req.ToolChoice.(map[string]interface{})
	if !ok {
		t.Fatalf("Expected ToolChoice to be map, got %T", req.ToolChoice)
	}
	if tc["type"] != "auto" {
		t.Errorf("Expected type='auto', got '%v'", tc["type"])
	}
}

// TestAnthropicToOpenAI_SystemBlocksNonTextType system blocks 中非 text 类型被过滤
func TestAnthropicToOpenAI_SystemBlocksNonTextType(t *testing.T) {
	ar := &types.AnthropicMessagesRequest{
		Model:     "claude",
		MaxTokens: 100,
		System: []interface{}{
			map[string]interface{}{"type": "image", "text": "should-be-ignored"},
			map[string]interface{}{"type": "text", "text": "kept-text"},
		},
		Messages: []types.AnthropicMessage{
			{Role: "user", Content: "hi"},
		},
	}

	req := AnthropicToOpenAI(ar)
	// 只应保留 text 类型，过滤掉 image
	if len(req.Messages) != 2 {
		t.Fatalf("Expected 2 messages (system + user), got %d", len(req.Messages))
	}
	if req.Messages[0].Content != "kept-text" {
		t.Errorf("Expected system content='kept-text', got '%v'", req.Messages[0].Content)
	}
}

// TestAnthropicToOpenAI_SystemBlocksEmptyText system blocks 中空 text 被过滤
func TestAnthropicToOpenAI_SystemBlocksEmptyText(t *testing.T) {
	ar := &types.AnthropicMessagesRequest{
		Model:     "claude",
		MaxTokens: 100,
		System: []interface{}{
			map[string]interface{}{"type": "text", "text": ""},
			map[string]interface{}{"type": "text", "text": "valid"},
		},
		Messages: []types.AnthropicMessage{
			{Role: "user", Content: "hi"},
		},
	}

	req := AnthropicToOpenAI(ar)
	// 空 text 被过滤，只保留 "valid"
	if req.Messages[0].Content != "valid" {
		t.Errorf("Expected system content='valid', got '%v'", req.Messages[0].Content)
	}
}

// TestAnthropicToOpenAI_SystemBlocksAllEmpty 所有 system blocks 都为空 → 不添加 system 消息
func TestAnthropicToOpenAI_SystemBlocksAllEmpty(t *testing.T) {
	ar := &types.AnthropicMessagesRequest{
		Model:     "claude",
		MaxTokens: 100,
		System: []interface{}{
			map[string]interface{}{"type": "image", "text": "x"},
			map[string]interface{}{"type": "text", "text": ""},
		},
		Messages: []types.AnthropicMessage{
			{Role: "user", Content: "hi"},
		},
	}

	req := AnthropicToOpenAI(ar)
	// 所有 system blocks 都被过滤，systemContent 为空 → 不添加 system 消息
	if len(req.Messages) != 1 {
		t.Fatalf("Expected 1 message (no system), got %d", len(req.Messages))
	}
	if req.Messages[0].Role != "user" {
		t.Errorf("Expected only user message, got role='%s'", req.Messages[0].Role)
	}
}

// TestAnthropicToOpenAI_SystemBlocksNonMapBlock system blocks 中非 map 类型被跳过
func TestAnthropicToOpenAI_SystemBlocksNonMapBlock(t *testing.T) {
	ar := &types.AnthropicMessagesRequest{
		Model:     "claude",
		MaxTokens: 100,
		System: []interface{}{
			"plain-string-block", // 非 map，应被跳过
			map[string]interface{}{"type": "text", "text": "valid"},
		},
		Messages: []types.AnthropicMessage{
			{Role: "user", Content: "hi"},
		},
	}

	req := AnthropicToOpenAI(ar)
	// 非 map block 被跳过，只保留 "valid"
	if req.Messages[0].Role != "system" || req.Messages[0].Content != "valid" {
		t.Errorf("Expected system message with 'valid', got: %+v", req.Messages[0])
	}
}

// TestAnthropicToOpenAI_SystemUnknownType system 字段为未知类型（非 string/[]interface{}）
func TestAnthropicToOpenAI_SystemUnknownType(t *testing.T) {
	ar := &types.AnthropicMessagesRequest{
		Model:     "claude",
		MaxTokens: 100,
		System:    12345, // int 类型，不匹配任何 case
		Messages: []types.AnthropicMessage{
			{Role: "user", Content: "hi"},
		},
	}

	req := AnthropicToOpenAI(ar)
	// 未知类型不产生 system 消息
	if len(req.Messages) != 1 {
		t.Fatalf("Expected 1 message (unknown system type ignored), got %d", len(req.Messages))
	}
}

// TestAnthropicToOpenAI_NoMessages 空消息列表
func TestAnthropicToOpenAI_NoMessages(t *testing.T) {
	ar := &types.AnthropicMessagesRequest{
		Model:     "claude",
		MaxTokens: 100,
		Messages:  []types.AnthropicMessage{},
	}

	req := AnthropicToOpenAI(ar)
	if len(req.Messages) != 0 {
		t.Errorf("Expected 0 messages, got %d", len(req.Messages))
	}
}

// ============================================================
// AnthropicMessageToOpenAI 补充测试：覆盖 tool_use / tool_result / 混合内容
// ============================================================

// TestAnthropicMessageToOpenAI_TextBlocks 多个 text block 合并
func TestAnthropicMessageToOpenAI_TextBlocks(t *testing.T) {
	am := types.AnthropicMessage{
		Role: "user",
		Content: []interface{}{
			map[string]interface{}{"type": "text", "text": "part1"},
			map[string]interface{}{"type": "text", "text": "part2"},
		},
	}

	om := AnthropicMessageToOpenAI(am)
	if om.Role != "user" {
		t.Errorf("Expected role='user', got '%s'", om.Role)
	}
	if om.Content != "part1\npart2" {
		t.Errorf("Expected combined content 'part1\npart2', got '%v'", om.Content)
	}
	if len(om.ToolCalls) != 0 {
		t.Errorf("Expected 0 tool calls, got %d", len(om.ToolCalls))
	}
}

// TestAnthropicMessageToOpenAI_TextBlockEmptyText 空 text block 被过滤
func TestAnthropicMessageToOpenAI_TextBlockEmptyText(t *testing.T) {
	am := types.AnthropicMessage{
		Role: "user",
		Content: []interface{}{
			map[string]interface{}{"type": "text", "text": ""},
			map[string]interface{}{"type": "text", "text": "kept"},
		},
	}

	om := AnthropicMessageToOpenAI(am)
	if om.Content != "kept" {
		t.Errorf("Expected content='kept', got '%v'", om.Content)
	}
}

// TestAnthropicMessageToOpenAI_ToolUse 工具调用转换
func TestAnthropicMessageToOpenAI_ToolUse(t *testing.T) {
	am := types.AnthropicMessage{
		Role: "assistant",
		Content: []interface{}{
			map[string]interface{}{
				"type":  "tool_use",
				"id":    "call_1",
				"name":  "get_weather",
				"input": map[string]interface{}{"city": "SF"},
			},
		},
	}

	om := AnthropicMessageToOpenAI(am)
	if len(om.ToolCalls) != 1 {
		t.Fatalf("Expected 1 tool call, got %d", len(om.ToolCalls))
	}
	tc := om.ToolCalls[0]
	if tc.ID != "call_1" {
		t.Errorf("Expected id='call_1', got '%s'", tc.ID)
	}
	if tc.Type != "function" {
		t.Errorf("Expected type='function', got '%s'", tc.Type)
	}
	if tc.Function.Name != "get_weather" {
		t.Errorf("Expected name='get_weather', got '%s'", tc.Function.Name)
	}
	// input 应被序列化为 JSON 字符串
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		t.Fatalf("Arguments not valid JSON: %v", err)
	}
	if args["city"] != "SF" {
		t.Errorf("Expected city='SF', got '%v'", args["city"])
	}
}

// TestAnthropicMessageToOpenAI_ToolUseWithText 工具调用 + 文本混合
func TestAnthropicMessageToOpenAI_ToolUseWithText(t *testing.T) {
	am := types.AnthropicMessage{
		Role: "assistant",
		Content: []interface{}{
			map[string]interface{}{"type": "text", "text": "calling tool"},
			map[string]interface{}{
				"type":  "tool_use",
				"id":    "call_1",
				"name":  "search",
				"input": map[string]interface{}{"q": "hello"},
			},
		},
	}

	om := AnthropicMessageToOpenAI(am)
	if len(om.ToolCalls) != 1 {
		t.Fatalf("Expected 1 tool call, got %d", len(om.ToolCalls))
	}
	// 当存在 tool_calls 时，text 也应保留在 Content 中
	if om.Content != "calling tool" {
		t.Errorf("Expected content='calling tool', got '%v'", om.Content)
	}
}

// TestAnthropicMessageToOpenAI_ToolResultString tool_result 内容为字符串
func TestAnthropicMessageToOpenAI_ToolResultString(t *testing.T) {
	am := types.AnthropicMessage{
		Role: "user",
		Content: []interface{}{
			map[string]interface{}{
				"type":       "tool_result",
				"tool_use_id": "call_1",
				"content":    "result string",
			},
		},
	}

	om := AnthropicMessageToOpenAI(am)
	if om.Role != "tool" {
		t.Errorf("Expected role='tool', got '%s'", om.Role)
	}
	if om.ToolCallID != "call_1" {
		t.Errorf("Expected tool_call_id='call_1', got '%s'", om.ToolCallID)
	}
	if om.Content != "result string" {
		t.Errorf("Expected content='result string', got '%v'", om.Content)
	}
}

// TestAnthropicMessageToOpenAI_ToolResultBlocks tool_result 内容为 block 数组
func TestAnthropicMessageToOpenAI_ToolResultBlocks(t *testing.T) {
	am := types.AnthropicMessage{
		Role: "user",
		Content: []interface{}{
			map[string]interface{}{
				"type":        "tool_result",
				"tool_use_id": "call_1",
				"content": []interface{}{
					map[string]interface{}{"type": "text", "text": "part1"},
					map[string]interface{}{"type": "text", "text": "part2"},
				},
			},
		},
	}

	om := AnthropicMessageToOpenAI(am)
	if om.Role != "tool" {
		t.Errorf("Expected role='tool', got '%s'", om.Role)
	}
	if om.ToolCallID != "call_1" {
		t.Errorf("Expected tool_call_id='call_1', got '%s'", om.ToolCallID)
	}
	// 多个 text block 应累加
	if om.Content != "part1part2" {
		t.Errorf("Expected content='part1part2', got '%v'", om.Content)
	}
}

// TestAnthropicMessageToOpenAI_ToolResultOtherContent tool_result 内容为其他类型（json.Marshal）
func TestAnthropicMessageToOpenAI_ToolResultOtherContent(t *testing.T) {
	am := types.AnthropicMessage{
		Role: "user",
		Content: []interface{}{
			map[string]interface{}{
				"type":        "tool_result",
				"tool_use_id": "call_1",
				"content":     42, // 非 string 非 []interface{}
			},
		},
	}

	om := AnthropicMessageToOpenAI(am)
	if om.Role != "tool" {
		t.Errorf("Expected role='tool', got '%s'", om.Role)
	}
	// int 会被 json.Marshal 成 "42"
	if om.Content != "42" {
		t.Errorf("Expected content='42', got '%v'", om.Content)
	}
}

// TestAnthropicMessageToOpenAI_ToolResultNoContent tool_result 无 content 字段
func TestAnthropicMessageToOpenAI_ToolResultNoContent(t *testing.T) {
	am := types.AnthropicMessage{
		Role: "user",
		Content: []interface{}{
			map[string]interface{}{
				"type":        "tool_result",
				"tool_use_id": "call_1",
				// 无 content 字段
			},
		},
	}

	om := AnthropicMessageToOpenAI(am)
	if om.Role != "tool" {
		t.Errorf("Expected role='tool', got '%s'", om.Role)
	}
	if om.ToolCallID != "call_1" {
		t.Errorf("Expected tool_call_id='call_1', got '%s'", om.ToolCallID)
	}
	// content 应为零值
	if om.Content != "" {
		t.Errorf("Expected empty content, got '%v'", om.Content)
	}
}

// TestAnthropicMessageToOpenAI_NonMapBlock block 非 map 类型被跳过
func TestAnthropicMessageToOpenAI_NonMapBlock(t *testing.T) {
	am := types.AnthropicMessage{
		Role: "user",
		Content: []interface{}{
			"plain-string", // 非 map，应被跳过
			map[string]interface{}{"type": "text", "text": "kept"},
		},
	}

	om := AnthropicMessageToOpenAI(am)
	if om.Content != "kept" {
		t.Errorf("Expected content='kept', got '%v'", om.Content)
	}
}

// TestAnthropicMessageToOpenAI_UnknownBlockType 未知 block 类型被忽略
func TestAnthropicMessageToOpenAI_UnknownBlockType(t *testing.T) {
	am := types.AnthropicMessage{
		Role: "user",
		Content: []interface{}{
			map[string]interface{}{"type": "image", "source": "data"},
			map[string]interface{}{"type": "text", "text": "kept"},
		},
	}

	om := AnthropicMessageToOpenAI(am)
	// 未知类型被忽略，只保留 text
	if om.Content != "kept" {
		t.Errorf("Expected content='kept', got '%v'", om.Content)
	}
}

// TestAnthropicMessageToOpenAI_EmptyBlocksArray 空 block 数组
func TestAnthropicMessageToOpenAI_EmptyBlocksArray(t *testing.T) {
	am := types.AnthropicMessage{
		Role:    "user",
		Content: []interface{}{},
	}

	om := AnthropicMessageToOpenAI(am)
	if om.Role != "user" {
		t.Errorf("Expected role='user', got '%s'", om.Role)
	}
	// 空数组 → textParts 和 toolCalls 都为空，Content 保持 nil
	if om.Content != nil {
		t.Errorf("Expected nil content for empty blocks, got '%v'", om.Content)
	}
	if len(om.ToolCalls) != 0 {
		t.Errorf("Expected 0 tool calls, got %d", len(om.ToolCalls))
	}
}

// TestAnthropicMessageToOpenAI_NonStringNonArrayContent 非 string/[]interface{} 的 content
func TestAnthropicMessageToOpenAI_NonStringNonArrayContent(t *testing.T) {
	am := types.AnthropicMessage{
		Role:    "user",
		Content: 12345, // int 类型，不匹配任何 case
	}

	om := AnthropicMessageToOpenAI(am)
	if om.Role != "user" {
		t.Errorf("Expected role='user', got '%s'", om.Role)
	}
	// 不匹配 case → Content 保持 nil
	if om.Content != nil {
		t.Errorf("Expected nil content for unknown type, got '%v'", om.Content)
	}
}

// TestAnthropicMessageToOpenAI_ToolResultBlockWithNonTextBlock tool_result block 数组中含非 text 类型
func TestAnthropicMessageToOpenAI_ToolResultBlockWithNonTextBlock(t *testing.T) {
	am := types.AnthropicMessage{
		Role: "user",
		Content: []interface{}{
			map[string]interface{}{
				"type":        "tool_result",
				"tool_use_id": "call_1",
				"content": []interface{}{
					map[string]interface{}{"type": "image", "text": "ignored"},
					map[string]interface{}{"type": "text", "text": "kept"},
				},
			},
		},
	}

	om := AnthropicMessageToOpenAI(am)
	if om.Content != "kept" {
		t.Errorf("Expected content='kept', got '%v'", om.Content)
	}
}

// TestAnthropicMessageToOpenAI_ToolResultBlockNonMap tool_result block 数组中含非 map 元素
func TestAnthropicMessageToOpenAI_ToolResultBlockNonMap(t *testing.T) {
	am := types.AnthropicMessage{
		Role: "user",
		Content: []interface{}{
			map[string]interface{}{
				"type":        "tool_result",
				"tool_use_id": "call_1",
				"content": []interface{}{
					"plain-string", // 非 map，应被跳过
					map[string]interface{}{"type": "text", "text": "kept"},
				},
			},
		},
	}

	om := AnthropicMessageToOpenAI(am)
	if om.Content != "kept" {
		t.Errorf("Expected content='kept', got '%v'", om.Content)
	}
}

// ============================================================
// OpenAIToAnthropicResponse 补充测试：覆盖 nil usage / 空内容 / 空参数
// ============================================================

// TestOpenAIToAnthropicResponse_NilUsage 响应无 usage 字段
func TestOpenAIToAnthropicResponse_NilUsage(t *testing.T) {
	respBody := []byte(`{
		"id": "r",
		"model": "m",
		"choices": [{"index": 0, "message": {"role": "assistant", "content": "x"}, "finish_reason": "stop"}]
	}`)

	resp, err := OpenAIToAnthropicResponse(respBody, "")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if resp.Usage.InputTokens != 0 || resp.Usage.OutputTokens != 0 {
		t.Errorf("Expected zero usage when nil, got input=%d output=%d", resp.Usage.InputTokens, resp.Usage.OutputTokens)
	}
}

// TestOpenAIToAnthropicResponse_EmptyContent 响应无文本/工具调用 → 回退空 text block
func TestOpenAIToAnthropicResponse_EmptyContent(t *testing.T) {
	respBody := []byte(`{
		"id": "r",
		"model": "m",
		"choices": [{"index": 0, "message": {"role": "assistant", "content": ""}, "finish_reason": "stop"}]
	}`)

	resp, err := OpenAIToAnthropicResponse(respBody, "")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(resp.Content) != 1 {
		t.Fatalf("Expected 1 content block (fallback empty text), got %d", len(resp.Content))
	}
	if resp.Content[0].Type != "text" || resp.Content[0].Text != "" {
		t.Errorf("Expected fallback empty text block, got %+v", resp.Content[0])
	}
}

// TestOpenAIToAnthropicResponse_NonStringContent 响应 content 非字符串（如数组）
func TestOpenAIToAnthropicResponse_NonStringContent(t *testing.T) {
	respBody := []byte(`{
		"id": "r",
		"model": "m",
		"choices": [{"index": 0, "message": {"role": "assistant", "content": ["part1", "part2"]}, "finish_reason": "stop"}]
	}`)

	resp, err := OpenAIToAnthropicResponse(respBody, "")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	// 非字符串 content 不会被添加为 text block → 回退到空 text block
	if len(resp.Content) != 1 {
		t.Fatalf("Expected 1 content block (fallback), got %d", len(resp.Content))
	}
	if resp.Content[0].Type != "text" {
		t.Errorf("Expected fallback text block, got type='%s'", resp.Content[0].Type)
	}
}

// TestOpenAIToAnthropicResponse_ToolCallEmptyArguments 工具调用参数为空字符串
func TestOpenAIToAnthropicResponse_ToolCallEmptyArguments(t *testing.T) {
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
					"function": {"name": "no_args", "arguments": ""}
				}]
			},
			"finish_reason": "tool_calls"
		}]
	}`)

	resp, err := OpenAIToAnthropicResponse(respBody, "")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(resp.Content) != 1 {
		t.Fatalf("Expected 1 content block, got %d", len(resp.Content))
	}
	if resp.Content[0].Type != "tool_use" {
		t.Errorf("Expected type='tool_use', got '%s'", resp.Content[0].Type)
	}
	// 空 arguments → input 应为 nil（不调用 Unmarshal）
	if resp.Content[0].Input != nil {
		t.Errorf("Expected nil input for empty arguments, got %v", resp.Content[0].Input)
	}
}

// TestOpenAIToAnthropicResponse_ToolCallInvalidArguments 工具调用参数为无效 JSON
func TestOpenAIToAnthropicResponse_ToolCallInvalidArguments(t *testing.T) {
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
					"function": {"name": "x", "arguments": "not-valid-json"}
				}]
			},
			"finish_reason": "tool_calls"
		}]
	}`)

	resp, err := OpenAIToAnthropicResponse(respBody, "")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	// 无效 JSON 时 Unmarshal 失败，input 保持 nil（不报错）
	if len(resp.Content) != 1 {
		t.Fatalf("Expected 1 content block, got %d", len(resp.Content))
	}
	if resp.Content[0].Type != "tool_use" {
		t.Errorf("Expected type='tool_use', got '%s'", resp.Content[0].Type)
	}
}

// TestOpenAIToAnthropicResponse_UnknownFinishReason 未知 finish_reason 默认 end_turn
func TestOpenAIToAnthropicResponse_UnknownFinishReason(t *testing.T) {
	respBody := []byte(`{
		"id": "r",
		"model": "m",
		"choices": [{"index": 0, "message": {"role": "assistant", "content": "x"}, "finish_reason": "unknown_reason"}]
	}`)

	resp, err := OpenAIToAnthropicResponse(respBody, "")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if resp.StopReason != "end_turn" {
		t.Errorf("Expected stop_reason='end_turn' for unknown, got '%s'", resp.StopReason)
	}
}

// TestOpenAIToAnthropicResponse_NoFinishReason 缺少 finish_reason 字段
func TestOpenAIToAnthropicResponse_NoFinishReason(t *testing.T) {
	respBody := []byte(`{
		"id": "r",
		"model": "m",
		"choices": [{"index": 0, "message": {"role": "assistant", "content": "x"}}]
	}`)

	resp, err := OpenAIToAnthropicResponse(respBody, "")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	// 缺少 finish_reason → 默认 end_turn
	if resp.StopReason != "end_turn" {
		t.Errorf("Expected stop_reason='end_turn' for missing, got '%s'", resp.StopReason)
	}
}

// TestOpenAIToAnthropicResponse_TextAndToolCalls 文本 + 工具调用并存
func TestOpenAIToAnthropicResponse_TextAndToolCalls(t *testing.T) {
	respBody := []byte(`{
		"id": "r",
		"model": "m",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"content": "Let me search",
				"tool_calls": [{
					"id": "call_1",
					"type": "function",
					"function": {"name": "search", "arguments": "{\"q\":\"hi\"}"}
				}]
			},
			"finish_reason": "tool_calls"
		}]
	}`)

	resp, err := OpenAIToAnthropicResponse(respBody, "")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(resp.Content) != 2 {
		t.Fatalf("Expected 2 content blocks (text + tool_use), got %d", len(resp.Content))
	}
	// 第一个应为 text
	if resp.Content[0].Type != "text" || resp.Content[0].Text != "Let me search" {
		t.Errorf("Unexpected first block: %+v", resp.Content[0])
	}
	// 第二个应为 tool_use
	if resp.Content[1].Type != "tool_use" {
		t.Errorf("Expected second block type='tool_use', got '%s'", resp.Content[1].Type)
	}
}

// TestOpenAIToAnthropicResponse_StopReasonContentFilter content_filter 映射为 end_turn
func TestOpenAIToAnthropicResponse_StopReasonContentFilter(t *testing.T) {
	respBody := []byte(`{
		"id": "r",
		"model": "m",
		"choices": [{"index": 0, "message": {"role": "assistant", "content": "x"}, "finish_reason": "content_filter"}]
	}`)

	resp, err := OpenAIToAnthropicResponse(respBody, "")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if resp.StopReason != "end_turn" {
		t.Errorf("Expected stop_reason='end_turn' for content_filter, got '%s'", resp.StopReason)
	}
}

// ============================================================
// BuildRequestBody 边缘测试
// ============================================================

// TestBuildRequestBody_NilRequest nil 请求应不崩溃（json.Marshal(nil) 返回 "null"）
func TestBuildRequestBody_NilRequest(t *testing.T) {
	body, err := BuildRequestBody(nil, nil)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if string(body) != "null" {
		t.Errorf("Expected 'null' for nil request, got '%s'", string(body))
	}
}

// TestBuildRequestBody_ExtraFieldsOverridesEverything ExtraFields 覆盖同名字段
func TestBuildRequestBody_ExtraFieldsOverridesEverything(t *testing.T) {
	req := &types.ChatRequest{
		Model:       "original",
		Temperature: 0.5,
		MaxTokens:   100,
	}
	extraFields := map[string]interface{}{
		"model":       "overridden",
		"temperature": 1.5,
		"max_tokens":  999,
		"new_field":   "new_value",
	}

	body, err := BuildRequestBody(req, extraFields)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if result["model"] != "overridden" {
		t.Errorf("Expected model='overridden', got '%v'", result["model"])
	}
	if result["temperature"].(float64) != 1.5 {
		t.Errorf("Expected temperature=1.5, got %v", result["temperature"])
	}
	if int(result["max_tokens"].(float64)) != 999 {
		t.Errorf("Expected max_tokens=999, got %v", result["max_tokens"])
	}
	if result["new_field"] != "new_value" {
		t.Errorf("Expected new_field='new_value', got '%v'", result["new_field"])
	}
}
