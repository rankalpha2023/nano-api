package engine

import (
	"testing"
	"time"

	"nano-api/types"
)

// ============================================================
// Phase 1: 配置继承 BUG 测试
// ============================================================

// TestRateLimit_PartialOverride_MaxRequestsOnly 测试:
// provider 只设 MaxRequestsPerMinute=60，不设 MinIntervalMs
// 预期: MinIntervalMs 从全局继承 100ms
func TestRateLimit_PartialOverride_MaxRequestsOnly(t *testing.T) {
	config := &types.Config{
		RateLimit: types.RateLimitConfig{
			MaxRequestsPerMinute: 40,
			MinIntervalMs:        100,
			RetryIntervalMs:      5000,
		},
		Providers: []types.ProviderConfig{
			{
				Name:          "test-provider",
				BaseURL:       "http://localhost",
				APIKeyEntries: []string{"key1"},
				Models:        map[string]string{"m1": "real-m1"},
				RateLimit: types.RateLimitConfig{
					MaxRequestsPerMinute: 60,
					MinIntervalMs:        0, // 未设置，应继承全局 100
					RetryIntervalMs:      0, // 未设置，应继承全局 5000
				},
			},
		},
	}

	am := NewAccountManager(config)
	ps := am.GetProviderState("test-provider")

	if ps == nil {
		t.Fatal("Expected provider state, got nil")
	}

	// 验证 MinIntervalMs 从全局继承
	gotMinInterval := ps.RateLimiter.MinInterval()
	expectedMinInterval := 100 * time.Millisecond
	if gotMinInterval != expectedMinInterval {
		t.Errorf("MinInterval: expected %v (inherited from global), got %v", expectedMinInterval, gotMinInterval)
	}

	// 验证 MaxRequestsPerMinute 使用 provider 自定义值
	gotMaxRPM := ps.RateLimiter.MaxRequestsPerMinute()
	expectedMaxRPM := 60
	if gotMaxRPM != expectedMaxRPM {
		t.Errorf("MaxRequestsPerMinute: expected %d (provider value), got %d", expectedMaxRPM, gotMaxRPM)
	}

	// 验证 RetryIntervalMs 从全局继承
	gotRetryInterval := ps.GetRetryInterval()
	expectedRetryInterval := 5000
	if gotRetryInterval != expectedRetryInterval {
		t.Errorf("RetryIntervalMs: expected %d (inherited from global), got %d", expectedRetryInterval, gotRetryInterval)
	}
}

// TestRateLimit_PartialOverride_MinIntervalOnly 测试:
// provider 只设 MinIntervalMs=500，不设 MaxRequestsPerMinute
// 预期: MaxRequestsPerMinute 从全局继承 40
func TestRateLimit_PartialOverride_MinIntervalOnly(t *testing.T) {
	config := &types.Config{
		RateLimit: types.RateLimitConfig{
			MaxRequestsPerMinute: 40,
			MinIntervalMs:        100,
			RetryIntervalMs:      5000,
		},
		Providers: []types.ProviderConfig{
			{
				Name:          "test-provider",
				BaseURL:       "http://localhost",
				APIKeyEntries: []string{"key1"},
				Models:        map[string]string{"m1": "real-m1"},
				RateLimit: types.RateLimitConfig{
					MaxRequestsPerMinute: 0,   // 未设置，应继承全局 40
					MinIntervalMs:        500, // 自定义
					RetryIntervalMs:      2000, // 自定义
				},
			},
		},
	}

	am := NewAccountManager(config)
	ps := am.GetProviderState("test-provider")

	if ps == nil {
		t.Fatal("Expected provider state, got nil")
	}

	// 验证 MaxRequestsPerMinute 从全局继承
	gotMaxRPM := ps.RateLimiter.MaxRequestsPerMinute()
	expectedMaxRPM := 40
	if gotMaxRPM != expectedMaxRPM {
		t.Errorf("MaxRequestsPerMinute: expected %d (inherited from global), got %d", expectedMaxRPM, gotMaxRPM)
	}

	// 验证 MinIntervalMs 使用 provider 自定义值
	gotMinInterval := ps.RateLimiter.MinInterval()
	expectedMinInterval := 500 * time.Millisecond
	if gotMinInterval != expectedMinInterval {
		t.Errorf("MinInterval: expected %v (provider value), got %v", expectedMinInterval, gotMinInterval)
	}

	// 验证 RetryIntervalMs 使用 provider 自定义值
	gotRetryInterval := ps.GetRetryInterval()
	expectedRetryInterval := 2000
	if gotRetryInterval != expectedRetryInterval {
		t.Errorf("RetryIntervalMs: expected %d (provider value), got %d", expectedRetryInterval, gotRetryInterval)
	}
}

// TestRateLimit_BothZero_UsesGlobal 测试:
// provider RateLimit 两字段都为 0 → 完全继承全局
func TestRateLimit_BothZero_UsesGlobal(t *testing.T) {
	config := &types.Config{
		RateLimit: types.RateLimitConfig{
			MaxRequestsPerMinute: 40,
			MinIntervalMs:        100,
			RetryIntervalMs:      5000,
		},
		Providers: []types.ProviderConfig{
			{
				Name:          "test-provider",
				BaseURL:       "http://localhost",
				APIKeyEntries: []string{"key1"},
				Models:        map[string]string{"m1": "real-m1"},
				RateLimit: types.RateLimitConfig{
					MaxRequestsPerMinute: 0,
					MinIntervalMs:        0,
					RetryIntervalMs:      0,
				},
			},
		},
	}

	am := NewAccountManager(config)
	ps := am.GetProviderState("test-provider")

	if ps == nil {
		t.Fatal("Expected provider state, got nil")
	}

	// 全部从全局继承
	gotMaxRPM := ps.RateLimiter.MaxRequestsPerMinute()
	if gotMaxRPM != 40 {
		t.Errorf("MaxRequestsPerMinute: expected 40 (global), got %d", gotMaxRPM)
	}

	gotMinInterval := ps.RateLimiter.MinInterval()
	expectedMinInterval := 100 * time.Millisecond
	if gotMinInterval != expectedMinInterval {
		t.Errorf("MinInterval: expected %v (global), got %v", expectedMinInterval, gotMinInterval)
	}

	gotRetryInterval := ps.GetRetryInterval()
	if gotRetryInterval != 5000 {
		t.Errorf("RetryIntervalMs: expected 5000 (global), got %d", gotRetryInterval)
	}
}

// TestRateLimit_BothSet_UsesProviderValues 测试:
// provider 明确设置了 RateLimit 所有字段 → 使用 provider 值
func TestRateLimit_BothSet_UsesProviderValues(t *testing.T) {
	config := &types.Config{
		RateLimit: types.RateLimitConfig{
			MaxRequestsPerMinute: 40,
			MinIntervalMs:        100,
			RetryIntervalMs:      5000,
		},
		Providers: []types.ProviderConfig{
			{
				Name:          "test-provider",
				BaseURL:       "http://localhost",
				APIKeyEntries: []string{"key1"},
				Models:        map[string]string{"m1": "real-m1"},
				RateLimit: types.RateLimitConfig{
					MaxRequestsPerMinute: 60,
					MinIntervalMs:        500,
					RetryIntervalMs:      2000,
				},
			},
		},
	}

	am := NewAccountManager(config)
	ps := am.GetProviderState("test-provider")

	gotMaxRPM := ps.RateLimiter.MaxRequestsPerMinute()
	if gotMaxRPM != 60 {
		t.Errorf("MaxRequestsPerMinute: expected 60 (provider), got %d", gotMaxRPM)
	}

	gotMinInterval := ps.RateLimiter.MinInterval()
	expectedMinInterval := 500 * time.Millisecond
	if gotMinInterval != expectedMinInterval {
		t.Errorf("MinInterval: expected %v (provider), got %v", expectedMinInterval, gotMinInterval)
	}

	gotRetryInterval := ps.GetRetryInterval()
	if gotRetryInterval != 2000 {
		t.Errorf("RetryIntervalMs: expected 2000 (provider), got %d", gotRetryInterval)
	}
}

// ============================================================
// Phase 1.2: ModelConfig fallback 测试
// ============================================================

// TestModelConfig_FallbackToGlobal 测试:
// provider 不设 ModelConfig（全零值）→ 从全局继承
func TestModelConfig_FallbackToGlobal(t *testing.T) {
	config := &types.Config{
		GlobalModelConfig: types.ModelConfig{
			DefaultTemperature: 0.7,
			DefaultTopP:        0.9,
			DefaultMaxTokens:   4096,
		},
		RateLimit: types.RateLimitConfig{
			MaxRequestsPerMinute: 40,
			MinIntervalMs:        100,
			RetryIntervalMs:      5000,
		},
		Providers: []types.ProviderConfig{
			{
				Name:          "test-provider",
				BaseURL:       "http://localhost",
				APIKeyEntries: []string{"key1"},
				Models:        map[string]string{"m1": "real-m1"},
				// ModelConfig 全零 → 应从全局继承
			},
		},
	}

	am := NewAccountManager(config)
	mc := am.GetModelConfig("test-provider")

	if mc.DefaultTemperature != 0.7 {
		t.Errorf("DefaultTemperature: expected 0.7 (global), got %v", mc.DefaultTemperature)
	}
	if mc.DefaultTopP != 0.9 {
		t.Errorf("DefaultTopP: expected 0.9 (global), got %v", mc.DefaultTopP)
	}
	if mc.DefaultMaxTokens != 4096 {
		t.Errorf("DefaultMaxTokens: expected 4096 (global), got %d", mc.DefaultMaxTokens)
	}
}

// TestModelConfig_ProviderOverridesGlobal 测试:
// provider 自定义了 ModelConfig → 用 provider 值，覆盖全局
func TestModelConfig_ProviderOverridesGlobal(t *testing.T) {
	config := &types.Config{
		GlobalModelConfig: types.ModelConfig{
			DefaultTemperature: 0.7,
			DefaultTopP:        0.9,
			DefaultMaxTokens:   4096,
		},
		RateLimit: types.RateLimitConfig{
			MaxRequestsPerMinute: 40,
			MinIntervalMs:        100,
			RetryIntervalMs:      5000,
		},
		Providers: []types.ProviderConfig{
			{
				Name:          "test-provider",
				BaseURL:       "http://localhost",
				APIKeyEntries: []string{"key1"},
				Models:        map[string]string{"m1": "real-m1"},
				ModelConfig: types.ModelConfig{
					DefaultTemperature: 0.2,
					DefaultTopP:        0.5,
					DefaultMaxTokens:   2048,
				},
			},
		},
	}

	am := NewAccountManager(config)
	mc := am.GetModelConfig("test-provider")

	if mc.DefaultTemperature != 0.2 {
		t.Errorf("DefaultTemperature: expected 0.2 (provider), got %v", mc.DefaultTemperature)
	}
	if mc.DefaultTopP != 0.5 {
		t.Errorf("DefaultTopP: expected 0.5 (provider), got %v", mc.DefaultTopP)
	}
	if mc.DefaultMaxTokens != 2048 {
		t.Errorf("DefaultMaxTokens: expected 2048 (provider), got %d", mc.DefaultMaxTokens)
	}
}

// TestModelConfig_PartialOverride 测试:
// provider 只覆盖部分 ModelConfig 字段 → 剩余从全局继承
func TestModelConfig_PartialOverride(t *testing.T) {
	config := &types.Config{
		GlobalModelConfig: types.ModelConfig{
			DefaultTemperature: 0.7,
			DefaultTopP:        0.9,
			DefaultMaxTokens:   4096,
		},
		RateLimit: types.RateLimitConfig{
			MaxRequestsPerMinute: 40,
			MinIntervalMs:        100,
			RetryIntervalMs:      5000,
		},
		Providers: []types.ProviderConfig{
			{
				Name:          "test-provider",
				BaseURL:       "http://localhost",
				APIKeyEntries: []string{"key1"},
				Models:        map[string]string{"m1": "real-m1"},
				ModelConfig: types.ModelConfig{
					DefaultTemperature: 0.2,   // 自定义
					DefaultTopP:        0,     // 未设置，应继承全局 0.9
					DefaultMaxTokens:   2048,  // 自定义
				},
			},
		},
	}

	am := NewAccountManager(config)
	mc := am.GetModelConfig("test-provider")

	if mc.DefaultTemperature != 0.2 {
		t.Errorf("DefaultTemperature: expected 0.2 (provider), got %v", mc.DefaultTemperature)
	}
	if mc.DefaultTopP != 0.9 {
		t.Errorf("DefaultTopP: expected 0.9 (inherited from global), got %v", mc.DefaultTopP)
	}
	if mc.DefaultMaxTokens != 2048 {
		t.Errorf("DefaultMaxTokens: expected 2048 (provider), got %d", mc.DefaultMaxTokens)
	}
}

// ============================================================
// Phase 1.3: Timeout fallback 测试
// ============================================================

// TestTimeout_FallbackToGlobal 测试:
// provider 不设 timeout → 从全局继承
func TestTimeout_FallbackToGlobal(t *testing.T) {
	config := &types.Config{
		GlobalTimeout: 120,
		RateLimit: types.RateLimitConfig{
			MaxRequestsPerMinute: 40,
			MinIntervalMs:        100,
			RetryIntervalMs:      5000,
		},
		Providers: []types.ProviderConfig{
			{
				Name:          "test-provider",
				BaseURL:       "http://localhost",
				APIKeyEntries: []string{"key1"},
				Models:        map[string]string{"m1": "real-m1"},
				// Timeout 未设置 → 应继承全局 120
			},
		},
	}

	am := NewAccountManager(config)
	account := am.GetNextAccount("test-provider")

	if account == nil {
		t.Fatal("Expected account, got nil")
	}
	if account.Timeout != 120 {
		t.Errorf("Timeout: expected 120 (global), got %d", account.Timeout)
	}
}

// TestTimeout_ProviderOverridesGlobal 测试:
// provider 自定义了 timeout → 用 provider 值
func TestTimeout_ProviderOverridesGlobal(t *testing.T) {
	config := &types.Config{
		GlobalTimeout: 120,
		RateLimit: types.RateLimitConfig{
			MaxRequestsPerMinute: 40,
			MinIntervalMs:        100,
			RetryIntervalMs:      5000,
		},
		Providers: []types.ProviderConfig{
			{
				Name:          "test-provider",
				BaseURL:       "http://localhost",
				APIKeyEntries: []string{"key1"},
				Models:        map[string]string{"m1": "real-m1"},
				Timeout:       60,
			},
		},
	}

	am := NewAccountManager(config)
	account := am.GetNextAccount("test-provider")

	if account == nil {
		t.Fatal("Expected account, got nil")
	}
	if account.Timeout != 60 {
		t.Errorf("Timeout: expected 60 (provider), got %d", account.Timeout)
	}
}
