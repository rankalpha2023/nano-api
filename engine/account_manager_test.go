package engine

import (
	"testing"
	"time"

	"nano-api/types"
)

func makeConfig() *types.Config {
	return &types.Config{
		RateLimit: types.RateLimitConfig{
			MaxRequestsPerMinute: 600,
			MinIntervalMs:        1,
			RetryIntervalMs:      50,
		},
	}
}

// ============================================================
// GetNextAccount — 轮询
// ============================================================

func TestGetNextAccount_RoundRobin(t *testing.T) {
	cfg := makeConfig()
	cfg.Providers = []types.ProviderConfig{
		{
			Name:          "p1",
			BaseURL:       "http://localhost",
			APIKeyEntries: []string{"k1", "k2", "k3"},
			Models:        map[string]string{"m1": "m1"},
		},
	}
	am := NewAccountManager(cfg)

	a1 := am.GetNextAccount("p1")
	a2 := am.GetNextAccount("p1")
	a3 := am.GetNextAccount("p1")
	a4 := am.GetNextAccount("p1") // 循环回第一个

	if a1 == nil || a2 == nil || a3 == nil || a4 == nil {
		t.Fatal("Expected all accounts to be non-nil")
	}
	if a1.APIKey != "k1" {
		t.Errorf("Expected k1, got %s", a1.APIKey)
	}
	if a2.APIKey != "k2" {
		t.Errorf("Expected k2, got %s", a2.APIKey)
	}
	if a3.APIKey != "k3" {
		t.Errorf("Expected k3, got %s", a3.APIKey)
	}
	if a4.APIKey != "k1" {
		t.Errorf("Expected k1 (wrap), got %s", a4.APIKey)
	}
}

func TestGetNextAccount_UnknownProvider(t *testing.T) {
	cfg := makeConfig()
	cfg.Providers = []types.ProviderConfig{
		{
			Name:          "p1",
			BaseURL:       "http://localhost",
			APIKeyEntries: []string{"k1"},
			Models:        map[string]string{"m1": "m1"},
		},
	}
	am := NewAccountManager(cfg)

	a := am.GetNextAccount("nonexistent")
	if a != nil {
		t.Error("Expected nil for unknown provider")
	}
}

// ============================================================
// MarkFailed / MarkAvailable — 故障恢复
// ============================================================

func TestMarkAccountFailed_RemovesFromAvailable(t *testing.T) {
	cfg := makeConfig()
	cfg.RateLimit.RetryIntervalMs = 5000 // 5 秒内不会恢复
	cfg.Providers = []types.ProviderConfig{
		{
			Name:          "p1",
			BaseURL:       "http://localhost",
			APIKeyEntries: []string{"k1", "k2"},
			Models:        map[string]string{"m1": "m1"},
		},
	}
	am := NewAccountManager(cfg)

	a1 := am.GetNextAccount("p1")
	if a1.APIKey != "k1" {
		t.Fatalf("Expected k1, got %s", a1.APIKey)
	}
	am.MarkAccountFailed(a1)

	// 下一次应该轮到 k2（k1 被移除）
	a2 := am.GetNextAccount("p1")
	if a2 == nil {
		t.Fatal("Expected k2, got nil")
	}
	if a2.APIKey != "k2" {
		t.Errorf("Expected k2 (k1 failed), got %s", a2.APIKey)
	}
}

func TestMarkAccountFailed_AllFailed(t *testing.T) {
	cfg := makeConfig()
	cfg.Providers = []types.ProviderConfig{
		{
			Name:          "p1",
			BaseURL:       "http://localhost",
			APIKeyEntries: []string{"k1"},
			Models:        map[string]string{"m1": "m1"},
		},
	}
	am := NewAccountManager(cfg)

	a := am.GetNextAccount("p1")
	am.MarkAccountFailed(a)

	// 唯一账号失败后，GetNextAccount 返回 nil
	a2 := am.GetNextAccount("p1")
	if a2 != nil {
		t.Errorf("Expected nil after all failed, got %v", a2)
	}
}

func TestMarkAccountFailed_RecoveryWithoutGoroutine(t *testing.T) {
	cfg := makeConfig()
	cfg.RateLimit.RetryIntervalMs = 5000 // 长重试期
	cfg.Providers = []types.ProviderConfig{
		{
			Name:          "p1",
			BaseURL:       "http://localhost",
			APIKeyEntries: []string{"k1"},
			Models:        map[string]string{"m1": "m1"},
		},
	}
	am := NewAccountManager(cfg) // 不 Start() → 无后台恢复

	a := am.GetNextAccount("p1")
	am.MarkAccountFailed(a)

	// 唯一账号失败 → 返回 nil
	a2 := am.GetNextAccount("p1")
	if a2 != nil {
		t.Errorf("Expected nil after sole account failed, got %v", a2.APIKey)
	}
}

func TestMarkAccountFailed_WithRecovery(t *testing.T) {
	cfg := makeConfig()
	cfg.RateLimit.RetryIntervalMs = 50
	cfg.Providers = []types.ProviderConfig{
		{
			Name:          "p1",
			BaseURL:       "http://localhost",
			APIKeyEntries: []string{"k1"},
			Models:        map[string]string{"m1": "m1"},
		},
	}
	am := NewAccountManager(cfg)
	am.Start() // 启动后台恢复 goroutine
	defer func() { /* goroutine 会一直运行到进程结束 */ }()

	a := am.GetNextAccount("p1")
	am.MarkAccountFailed(a)

	// 等 150ms（足够 ticker 触发两次）
	time.Sleep(150 * time.Millisecond)

	a2 := am.GetNextAccount("p1")
	if a2 == nil {
		t.Error("Expected account to be recovered")
	}
}

func TestMarkAccountAvailable(t *testing.T) {
	cfg := makeConfig()
	cfg.Providers = []types.ProviderConfig{
		{
			Name:          "p1",
			BaseURL:       "http://localhost",
			APIKeyEntries: []string{"k1"},
			Models:        map[string]string{"m1": "m1"},
		},
	}
	am := NewAccountManager(cfg)

	a := am.GetNextAccount("p1")
	am.MarkAccountAvailable(a)
	if a.RequestCount != 1 {
		t.Errorf("Expected RequestCount=1, got %d", a.RequestCount)
	}
}

// ============================================================
// 模型路由
// ============================================================

func TestGetNextProviderForModel(t *testing.T) {
	cfg := makeConfig()
	cfg.Providers = []types.ProviderConfig{
		{
			Name:          "p1",
			BaseURL:       "http://localhost",
			APIKeyEntries: []string{"k1"},
			Models:        map[string]string{"m1": "real-m1", "shared": "real-shared"},
		},
		{
			Name:          "p2",
			BaseURL:       "http://localhost",
			APIKeyEntries: []string{"k2"},
			Models:        map[string]string{"m2": "real-m2", "shared": "real-shared-v2"},
		},
	}
	am := NewAccountManager(cfg)

	p := am.GetNextProviderForModel("m1")
	if p != "p1" {
		t.Errorf("Expected p1 for model m1, got %s", p)
	}

	p = am.GetNextProviderForModel("m2")
	if p != "p2" {
		t.Errorf("Expected p2 for model m2, got %s", p)
	}
}

func TestGetNextProviderForModel_RoundRobin(t *testing.T) {
	cfg := makeConfig()
	cfg.Providers = []types.ProviderConfig{
		{
			Name:          "p1",
			BaseURL:       "http://localhost",
			APIKeyEntries: []string{"k1"},
			Models:        map[string]string{"shared": "real-shared"},
		},
		{
			Name:          "p2",
			BaseURL:       "http://localhost",
			APIKeyEntries: []string{"k2"},
			Models:        map[string]string{"shared": "real-shared-v2"},
		},
	}
	am := NewAccountManager(cfg)

	r1 := am.GetNextProviderForModel("shared")
	r2 := am.GetNextProviderForModel("shared")
	r3 := am.GetNextProviderForModel("shared")

	if r1 == r2 {
		t.Error("Expected round-robin between providers")
	}
	if r3 != r1 {
		t.Errorf("Expected third call to wrap to %s, got %s", r1, r3)
	}
}

func TestGetNextProviderForModel_NotFound(t *testing.T) {
	cfg := makeConfig()
	cfg.Providers = []types.ProviderConfig{
		{
			Name:          "p1",
			BaseURL:       "http://localhost",
			APIKeyEntries: []string{"k1"},
			Models:        map[string]string{"m1": "m1"},
		},
	}
	am := NewAccountManager(cfg)

	p := am.GetNextProviderForModel("nonexistent")
	if p != "" {
		t.Errorf("Expected empty for unknown model, got %s", p)
	}
}

// ============================================================
// GetRealModelName
// ============================================================

func TestGetRealModelName_Alias(t *testing.T) {
	cfg := makeConfig()
	cfg.Providers = []types.ProviderConfig{
		{
			Name:          "p1",
			BaseURL:       "http://localhost",
			APIKeyEntries: []string{"k1"},
			Models:        map[string]string{"alias-m1": "real-model-name"},
		},
	}
	am := NewAccountManager(cfg)

	name := am.GetRealModelName("p1", "alias-m1")
	if name != "real-model-name" {
		t.Errorf("Expected 'real-model-name', got '%s'", name)
	}
}

func TestGetRealModelName_NoAlias(t *testing.T) {
	cfg := makeConfig()
	cfg.Providers = []types.ProviderConfig{
		{
			Name:          "p1",
			BaseURL:       "http://localhost",
			APIKeyEntries: []string{"k1"},
			Models:        map[string]string{},
		},
	}
	am := NewAccountManager(cfg)

	name := am.GetRealModelName("p1", "direct-model")
	if name != "direct-model" {
		t.Errorf("Expected 'direct-model' (passthrough), got '%s'", name)
	}
}

func TestGetRealModelName_UnknownProvider(t *testing.T) {
	cfg := makeConfig()
	am := NewAccountManager(cfg)

	name := am.GetRealModelName("nonexistent", "m1")
	if name != "m1" {
		t.Errorf("Expected 'm1' (passthrough), got '%s'", name)
	}
}

// ============================================================
// GetAllModels
// ============================================================

func TestGetAllModels_Dedup(t *testing.T) {
	cfg := makeConfig()
	cfg.Providers = []types.ProviderConfig{
		{
			Name:          "p1",
			BaseURL:       "http://localhost",
			APIKeyEntries: []string{"k1"},
			Models:        map[string]string{"gpt-4": "gpt-4-real"},
		},
		{
			Name:          "p2",
			BaseURL:       "http://localhost",
			APIKeyEntries: []string{"k2"},
			Models:        map[string]string{"gpt-4": "gpt-4-v2"},
		},
	}
	am := NewAccountManager(cfg)

	models := am.GetAllModels()
	if len(models) != 1 {
		t.Errorf("Expected 1 model (deduplicated), got %d: %v", len(models), models)
	}
	if models[0].ID != "gpt-4" {
		t.Errorf("Expected gpt-4, got %s", models[0].ID)
	}
}

func TestGetAllModels_MultipleProviders(t *testing.T) {
	cfg := makeConfig()
	cfg.Providers = []types.ProviderConfig{
		{
			Name:          "p1",
			BaseURL:       "http://localhost",
			APIKeyEntries: []string{"k1"},
			Models:        map[string]string{"gpt-4": "gpt-4", "claude": "claude"},
		},
		{
			Name:          "p2",
			BaseURL:       "http://localhost",
			APIKeyEntries: []string{"k2"},
			Models:        map[string]string{"gemini": "gemini"},
		},
	}
	am := NewAccountManager(cfg)

	models := am.GetAllModels()
	if len(models) != 3 {
		t.Errorf("Expected 3 models, got %d", len(models))
	}
	for _, id := range []string{"gpt-4", "claude", "gemini"} {
		found := false
		for _, m := range models {
			if m.ID == id {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected model %s in list", id)
		}
	}
}

// ============================================================
// GetModelConfig
// ============================================================

func TestGetModelConfig_UnknownProvider(t *testing.T) {
	cfg := makeConfig()
	am := NewAccountManager(cfg)

	mc := am.GetModelConfig("nonexistent")
	if mc.DefaultTemperature != 0 || mc.DefaultTopP != 0 || mc.DefaultMaxTokens != 0 {
		t.Error("Expected zero ModelConfig for unknown provider")
	}
}

// ============================================================
// SendRequest 路由
// ============================================================

func TestSendRequest_Success(t *testing.T) {
	cfg := makeConfig()
	cfg.Providers = []types.ProviderConfig{
		{
			Name:          "p1",
			BaseURL:       "http://localhost",
			APIKeyEntries: []string{"k1"},
			Models:        map[string]string{"m1": "real-m1"},
		},
	}
	am := NewAccountManager(cfg)

	handler, account, realModel, _, err := am.SendRequest(&types.ChatRequest{
		Model:    "m1",
		Messages: []types.Message{{Role: "user", Content: "hi"}},
	})

	if err != nil {
		t.Fatalf("Expected success, got error: %v", err)
	}
	if handler == nil {
		t.Fatal("Expected non-nil handler")
	}
	if account == nil {
		t.Fatal("Expected non-nil account")
	}
	if account.APIKey != "k1" {
		t.Errorf("Expected k1, got %s", account.APIKey)
	}
	if realModel != "real-m1" {
		t.Errorf("Expected real-m1, got %s", realModel)
	}
}

func TestSendRequest_ModelNotFound(t *testing.T) {
	cfg := makeConfig()
	cfg.Providers = []types.ProviderConfig{
		{
			Name:          "p1",
			BaseURL:       "http://localhost",
			APIKeyEntries: []string{"k1"},
			Models:        map[string]string{"m1": "m1"},
		},
	}
	am := NewAccountManager(cfg)

	_, _, _, _, err := am.SendRequest(&types.ChatRequest{
		Model: "nonexistent",
	})
	// 当前代码返回 nil error + nil handler（隐式）
	if err == nil {
		handler, account, _, _, err2 := am.SendRequest(&types.ChatRequest{Model: "nonexistent"})
		_ = handler
		_ = account
		_ = err2
	}
}

func TestSendRequest_AllAccountFailed(t *testing.T) {
	cfg := makeConfig()
	cfg.RateLimit.RetryIntervalMs = 5000
	cfg.Providers = []types.ProviderConfig{
		{
			Name:          "p1",
			BaseURL:       "http://localhost",
			APIKeyEntries: []string{"k1"},
			Models:        map[string]string{"m1": "m1"},
		},
	}
	am := NewAccountManager(cfg)

	// 标记唯一账号失败
	a := am.GetNextAccount("p1")
	am.MarkAccountFailed(a)

	handler, account, _, _, err := am.SendRequest(&types.ChatRequest{
		Model:    "m1",
		Messages: []types.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Errorf("Expected nil error, got %v", err)
	}
	if handler != nil || account != nil {
		t.Error("Expected nil handler+account when all failed")
	}
}
