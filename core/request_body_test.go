package core_test

import (
	"encoding/json"
	"testing"

	"nano-api/core"
	"nano-api/types"
)

// TestBuildRequestBody_NoExtraFields 测试无 ExtraFields 时的快速路径
func TestBuildRequestBody_NoExtraFields(t *testing.T) {
	req := &types.ChatRequest{
		Model: "test-model",
		Messages: []types.Message{
			{Role: "user", Content: "hello"},
		},
	}

	body, err := core.BuildRequestBody(req, nil)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	if result["model"] != "test-model" {
		t.Errorf("Expected model 'test-model', got '%v'", result["model"])
	}

	if _, exists := result["thinking_enabled"]; exists {
		t.Error("thinking_enabled should not be present when no extra fields")
	}
}

// TestBuildRequestBody_WithBasicTypes 测试基础类型支持
func TestBuildRequestBody_WithBasicTypes(t *testing.T) {
	req := &types.ChatRequest{Model: "test"}

	extraFields := map[string]interface{}{
		"bool_field":   true,
		"int_field":    42,
		"float_field":  3.14,
		"string_field": "hello",
	}

	body, err := core.BuildRequestBody(req, extraFields)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	if result["bool_field"] != true {
		t.Errorf("Expected bool_field=true, got %v", result["bool_field"])
	}

	if int(result["int_field"].(float64)) != 42 {
		t.Errorf("Expected int_field=42, got %v", result["int_field"])
	}

	if result["float_field"].(float64) != 3.14 {
		t.Errorf("Expected float_field=3.14, got %v", result["float_field"])
	}

	if result["string_field"] != "hello" {
		t.Errorf("Expected string_field='hello', got '%v'", result["string_field"])
	}
}

// TestBuildRequestBody_OverrideStandardField 测试 ExtraFields 覆盖标准字段
func TestBuildRequestBody_OverrideStandardField(t *testing.T) {
	req := &types.ChatRequest{
		Model:       "original-model",
		Temperature: 0.5,
	}

	extraFields := map[string]interface{}{
		"model":       "overridden-model",
		"temperature": 1.0,
	}

	body, _ := core.BuildRequestBody(req, extraFields)

	var result map[string]interface{}
	json.Unmarshal(body, &result)

	if result["model"] != "overridden-model" {
		t.Errorf("Expected model to be overridden to 'overridden-model', got '%v'", result["model"])
	}

	if result["temperature"] != 1.0 {
		t.Errorf("Expected temperature to be overridden to 1.0, got %v", result["temperature"])
	}
}

// TestBuildRequestBody_ComplexTypes 测试嵌套对象和数组
func TestBuildRequestBody_ComplexTypes(t *testing.T) {
	req := &types.ChatRequest{Model: "test"}

	extraFields := map[string]interface{}{
		"nested": map[string]interface{}{
			"key1": "value1",
			"key2": 123,
		},
		"array_field": []interface{}{"a", "b", "c"},
	}

	body, _ := core.BuildRequestBody(req, extraFields)

	var result map[string]interface{}
	json.Unmarshal(body, &result)

	nested, ok := result["nested"].(map[string]interface{})
	if !ok {
		t.Fatal("nested should be a map")
	}

	if nested["key1"] != "value1" {
		t.Errorf("Expected nested.key1='value1', got '%v'", nested["key1"])
	}

	arr, ok := result["array_field"].([]interface{})
	if !ok {
		t.Fatal("array_field should be an array")
	}

	if len(arr) != 3 {
		t.Errorf("Expected array length 3, got %d", len(arr))
	}
}

// TestBuildRequestBody_PreservesOriginalFields 测试保留原始字段
func TestBuildRequestBody_PreservesOriginalFields(t *testing.T) {
	req := &types.ChatRequest{
		Model:       "gpt-4",
		Temperature: 0.7,
		MaxTokens:   2048,
		Stream:      true,
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
		},
	}

	extraFields := map[string]interface{}{
		"custom_field": "custom_value",
	}

	body, _ := core.BuildRequestBody(req, extraFields)

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	if result["model"] != "gpt-4" {
		t.Errorf("Original model should be preserved, got %v", result["model"])
	}

	if result["temperature"] != 0.7 {
		t.Errorf("Original temperature should be preserved, got %v", result["temperature"])
	}

	maxTokens := result["max_tokens"].(float64)
	if maxTokens != 2048 {
		t.Errorf("Original max_tokens should be preserved, got %v", maxTokens)
	}

	if result["stream"] != true {
		t.Errorf("Original stream should be preserved")
	}

	if result["custom_field"] != "custom_value" {
		t.Errorf("Custom field should be added")
	}
}

// TestBuildRequestBody_EmptyExtraFields 测试空的 ExtraFields
func TestBuildRequestBody_EmptyExtraFields(t *testing.T) {
	req := &types.ChatRequest{Model: "test"}

	extraFields := map[string]interface{}{}

	body, err := core.BuildRequestBody(req, extraFields)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	var result map[string]interface{}
	json.Unmarshal(body, &result)

	if result["model"] != "test" {
		t.Errorf("Model should be preserved with empty extra fields")
	}
}

// TestBuildRequestBody_MultipleExtraFields 测试多个额外字段
func TestBuildRequestBody_MultipleExtraFields(t *testing.T) {
	req := &types.ChatRequest{Model: "deepseek-v4"}

	extraFields := map[string]interface{}{
		"thinking_enabled": true,
		"custom_param":     "value1",
		"another_flag":     false,
		"numeric_value":    100,
		"floating_point":   2.5,
	}

	body, _ := core.BuildRequestBody(req, extraFields)

	var result map[string]interface{}
	json.Unmarshal(body, &result)

	expectedFields := []string{
		"thinking_enabled",
		"custom_param",
		"another_flag",
		"numeric_value",
		"floating_point",
	}

	for _, field := range expectedFields {
		if _, exists := result[field]; !exists {
			t.Errorf("Field '%s' should be present in result", field)
		}
	}
}

// TestBuildRequestBody_JsonStructure 验证输出是有效的 JSON
func TestBuildRequestBody_JsonStructure(t *testing.T) {
	req := &types.ChatRequest{
		Model: "test-model",
		Messages: []types.Message{
			{Role: "user", Content: "test message"},
		},
	}

	extraFields := map[string]interface{}{
		"extra_bool": true,
		"extra_str":  "test",
	}

	body, err := core.BuildRequestBody(req, extraFields)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if !json.Valid(body) {
		t.Error("Output should be valid JSON")
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("Result should be unmarshalable: %v", err)
	}
	_ = result // 使用 result 避免编译错误
}
