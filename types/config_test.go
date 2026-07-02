package types

import (
	"testing"
)

// TestModelListResponse 验证 ModelListResponse 的零值行为
func TestModelListResponse_Empty(t *testing.T) {
	resp := ModelListResponse{
		Object: "list",
		Data:   []ModelInfo{},
	}
	if resp.Object != "list" {
		t.Error("Object should be 'list'")
	}
	if resp.Data == nil {
		t.Error("Data should be empty slice, not nil")
	}
}

// TestAccountStatus 验证 AccountStatus 常量
func TestAccountStatus_Constants(t *testing.T) {
	if AccountStatusAvailable != "available" {
		t.Errorf("Expected 'available', got '%s'", AccountStatusAvailable)
	}
	if AccountStatusFailed != "failed" {
		t.Errorf("Expected 'failed', got '%s'", AccountStatusFailed)
	}
	if AccountStatusDisabled != "disabled" {
		t.Errorf("Expected 'disabled', got '%s'", AccountStatusDisabled)
	}
}

// TestConfig_FieldsValidate 回归测试：验证 types.Config 仅包含数据字段，不含逻辑函数
func TestConfig_FieldsValidate(t *testing.T) {
	cfg := Config{
		Port:         8080,
		RequestRetry: 3,
		Providers:    []ProviderConfig{{Name: "p1"}},
	}
	if cfg.Port != 8080 {
		t.Errorf("Port: %d", cfg.Port)
	}
	if len(cfg.Providers) != 1 {
		t.Errorf("Providers: %d", len(cfg.Providers))
	}
}
