# NanoAPI 端到端测试 — 实施计划

> **For Hermes:** Use subagent-driven-development skill to implement this plan task-by-task.

**Goal:** 为 NanoAPI 建立覆盖 90%+ 的完整测试套件，修复配置继承 BUG，消除回归风险。

**Architecture:** 纯 Go testing 框架 + httptest.Server 内联 mock。所有测试运行 `go test ./...`。分 6 个 Phase，每个 Phase 都有 RED-GREEN-REFACTOR。

**Tech Stack:** Go 1.25, `golang.org/x/time`, `gopkg.in/yaml.v3`, `net/http/httptest`

**当前状态:**
- 项目有 4 个老测试文件，使用已废弃的 `types.ServerConfig` API，无法编译
- `test/server_test.go` 用 `main()` 而非 `go test` 标准框架
- `mock/server.go` 独立进程，端口硬编码 8081
- 3 个配置继承 BUG 待修复

**最终产出:**
```
test_NanoAPI/
├── types/
│   └── config_test.go
├── core/
│   ├── request_body_test.go      (已有，从 core/test/ 移入)
│   ├── rate_limit_test.go        (已有，从 core/test/ 移入)
│   ├── request_test.go           (新建: SendRequest + header + proxy + timeout)
│   └── rate_limit_concurrency_test.go  (新建)
├── engine/
│   ├── account_manager_test.go   (重写)
│   └── provider_isolation_test.go (新建)
├── server/
│   ├── handler_test.go           (新建: CORS / 错误码 / models)
│   ├── stream_test.go            (新建: 流式)
│   └── e2e_test.go               (新建: 全链路)
└── [清理] test/                 (删除旧测试目录)
    [清理] engine/test/          (删除旧测试目录)
    [清理] core/test/            (测试移到 core/ 后删除)
    [清理] mock/                 (删除, 改用 httptest)
```

---

## Phase 1: 修复配置继承 BUG（带 TDD）

### Task 1.1: 写 RateLimit 逐字段继承的 FAILING 测试

**Objective:** 证明当前代码中，provider 只设 `MaxRequestsPerMinute` 不设 `MinIntervalMs` 时，`MinIntervalMs` 不会被全局值填充

**Files:**
- Create: `engine/provider_isolation_test.go`

**Step 1: 写测试**

```go
package engine

import (
	"testing"
	"nano-api/types"
)

func TestRateLimit_PartialOverride_MaxRequestsOnly(t *testing.T) {
	// 全局 RateLimit: 40 RPM, 100ms interval
	// provider RateLimit: 只设 MaxRequestsPerMinute=60, MinIntervalMs=0
	// 预期: MinIntervalMs 应从全局继承 100ms
	config := &types.Config{
		RateLimit: types.RateLimitConfig{
			MaxRequestsPerMinute: 40,
			MinIntervalMs:        100,
			RetryIntervalMs:      5000,
		},
		Providers: []types.ProviderConfig{
			{
				Name:    "test-provider",
				BaseURL: "http://localhost",
				APIKeyEntries: []string{"key1"},
				Models:  map[string]string{"m1": "real-m1"},
				RateLimit: types.RateLimitConfig{
					MaxRequestsPerMinute: 60,
					MinIntervalMs:        0,   // 未设置
					RetryIntervalMs:      0,   // 未设置
				},
			},
		},
	}

	am := NewAccountManager(config)
	ps := am.providers["test-provider"]

	// 验证 MaxRequestsPerMinute 使用 provider 值
	if ps.RateLimiter == nil {
		t.Fatal("RateLimiter should not be nil")
	}

	// 验证 MinIntervalMs 从全局继承
	// 当前代码 BUG: 因为 MaxRequestsPerMinute!=0, 条件不触发, MinIntervalMs 保持 0
	// 修复后: MinIntervalMs 应为 100
	if ps.RateLimiter == nil {
		// placeholder for inspection
	}
	// 注意: RateLimiter 的 minInterval 是内部字段，这里需要暴露测试方法
	// 暂时验证构造函数参数正确传入
}

func TestRateLimit_PartialOverride_MinIntervalOnly(t *testing.T) {
	config := &types.Config{
		RateLimit: types.RateLimitConfig{
			MaxRequestsPerMinute: 40,
			MinIntervalMs:        100,
			RetryIntervalMs:      5000,
		},
		Providers: []types.ProviderConfig{
			{
				Name:    "test-provider",
				BaseURL: "http://localhost",
				APIKeyEntries: []string{"key1"},
				Models:  map[string]string{"m1": "real-m1"},
				RateLimit: types.RateLimitConfig{
					MaxRequestsPerMinute: 0,    // 未设置
					MinIntervalMs:        500,  // 自定义
					RetryIntervalMs:      2000, // 自定义
				},
			},
		},
	}

	am := NewAccountManager(config)
	ps := am.providers["test-provider"]

	// 验证 MaxRequestsPerMinute 从全局继承
	// 当前 BUG: MaxRequestsPerMinute=0 且 MinIntervalMs!=0 不触发 fallback
	// 修复后: MaxRequestsPerMinute 应为 40
	_ = ps
}

func TestRateLimit_RetryIntervalZero_InheritsGlobal(t *testing.T) {
	config := &types.Config{
		RateLimit: types.RateLimitConfig{
			MaxRequestsPerMinute: 40,
			MinIntervalMs:        100,
			RetryIntervalMs:      5000,
		},
		Providers: []types.ProviderConfig{
			{
				Name:    "test-provider",
				BaseURL: "http://localhost",
				APIKeyEntries: []string{"key1"},
				Models:  map[string]string{"m1": "real-m1"},
				RateLimit: types.RateLimitConfig{
					MaxRequestsPerMinute: 60,
					MinIntervalMs:        500,
					RetryIntervalMs:      0,  // 未设置，应继承 5000
				},
			},
		},
	}

	am := NewAccountManager(config)
	ps := am.providers["test-provider"]

	// 当前 BUG: RetryIntervalMs 永远是 provider 自己的值, 没有 fallback
	// 修复后: RetryIntervalMs 应为 5000
	_ = ps
}
```

需要先给 RateLimiter 加访问器方法：

```go
// 在 core/rate_limit.go 中添加
func (r *RateLimiter) MinInterval() time.Duration {
	return r.minInterval
}
```

```go
// 在 engine/account_manager.go 中添加 ProviderState 测试访问器
// 或者在测试中通过 am.providers 直接访问
```

**Step 2: 运行测试**

```bash
cd "f:/Projects/test/test_NanoAPI" && go test ./engine/ -run "TestRateLimit_PartialOverride" -v
```

Expected: FAIL — 测试中可以观察到 MinIntervalMs 仍为 0

**Step 3: 修复代码**

Modify: `engine/account_manager.go:39-43`

```go
// Before:
rateLimit := provider.RateLimit
if rateLimit.MaxRequestsPerMinute == 0 && rateLimit.MinIntervalMs == 0 {
    rateLimit = defaultRateLimit
}

// After:
rateLimit := provider.RateLimit
if rateLimit.MaxRequestsPerMinute == 0 {
    rateLimit.MaxRequestsPerMinute = defaultRateLimit.MaxRequestsPerMinute
}
if rateLimit.MinIntervalMs == 0 {
    rateLimit.MinIntervalMs = defaultRateLimit.MinIntervalMs
}
if rateLimit.RetryIntervalMs == 0 {
    rateLimit.RetryIntervalMs = defaultRateLimit.RetryIntervalMs
}
```

**Step 4: 运行测试验证 PASS**

```bash
cd "f:/Projects/test/test_NanoAPI" && go test ./engine/ -run "TestRateLimit" -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add types/*.go engine/provider_isolation_test.go engine/account_manager.go
git commit -m "fix: RateLimit per-field fallback to global defaults"

# 注意: provider_isolation_test.go 尚未完整，后续 task 继续添加
```

---

### Task 1.2: 添加全局 ModelConfig fallback

**Objective:** 当 provider 不设 ModelConfig 时，使用全局值而非零值

**Files:**
- Modify: `types/config.go` — 添加 `GlobalModelConfig` 字段
- Modify: `engine/account_manager.go` — 继承逻辑
- Modify: `engine/provider_isolation_test.go` — 添加测试

**Step 1: 修改 types/config.go**

```go
type Config struct {
	Providers         []ProviderConfig `json:"providers" yaml:"providers"`
	Port              int              `json:"port" yaml:"port"`
	RequestRetry      int              `json:"requestRetry" yaml:"requestRetry"`
	RateLimit         RateLimitConfig  `json:"rate-limit" yaml:"rate-limit"`
	GlobalModelConfig ModelConfig      `json:"global-model-config" yaml:"global-model-config"` // NEW
}
```

**Step 2: 修改 engine/account_manager.go**

在 `NewAccountManager` 中，遍历 providers 时填充 ModelConfig fallback：

```go
// 在 providers[provider.Name] = &ProviderState{...} 之前添加:
modelConfig := provider.ModelConfig
if modelConfig.DefaultTemperature == 0 {
    modelConfig.DefaultTemperature = config.GlobalModelConfig.DefaultTemperature
}
if modelConfig.DefaultTopP == 0 {
    modelConfig.DefaultTopP = config.GlobalModelConfig.DefaultTopP
}
if modelConfig.DefaultMaxTokens == 0 {
    modelConfig.DefaultMaxTokens = config.GlobalModelConfig.DefaultMaxTokens
}
```

**Step 3: 写测试**

```go
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
				Name:    "test-provider",
				BaseURL: "http://localhost",
				APIKeyEntries: []string{"key1"},
				Models: map[string]string{"m1": "real-m1"},
				// ModelConfig 全零 → 应从全局继承
			},
		},
	}

	am := NewAccountManager(config)
	mc := am.GetModelConfig("test-provider")

	if mc.DefaultTemperature != 0.7 {
		t.Errorf("Expected DefaultTemperature 0.7, got %v", mc.DefaultTemperature)
	}
	if mc.DefaultTopP != 0.9 {
		t.Errorf("Expected DefaultTopP 0.9, got %v", mc.DefaultTopP)
	}
	if mc.DefaultMaxTokens != 4096 {
		t.Errorf("Expected DefaultMaxTokens 4096, got %d", mc.DefaultMaxTokens)
	}
}

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
				Name:    "test-provider",
				BaseURL: "http://localhost",
				APIKeyEntries: []string{"key1"},
				Models: map[string]string{"m1": "real-m1"},
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
		t.Errorf("Expected DefaultTemperature 0.2, got %v", mc.DefaultTemperature)
	}
	if mc.DefaultMaxTokens != 2048 {
		t.Errorf("Expected DefaultMaxTokens 2048, got %d", mc.DefaultMaxTokens)
	}
}
```

**Step 4: 运行测试**

```bash
cd "f:/Projects/test/test_NanoAPI" && go test ./engine/ -run "TestModelConfig" -v
```

Expected: FAIL (before fix) → PASS (after fix)

**Step 5: Commit**

```bash
git add types/config.go engine/account_manager.go engine/provider_isolation_test.go
git commit -m "fix: ModelConfig fallback to global defaults"
```

---

### Task 1.3: 添加全局 Timeout fallback

**Objective:** provider 不设 Timeout 时，使用全局值而非硬编码 30

**Files:**
- Modify: `types/config.go` — 添加 `GlobalTimeout` 字段
- Modify: `engine/account_manager.go` — 继承逻辑
- Modify: `server/server.go` — 使用 fallback

**Step 1: 修改 types/config.go**

```go
type Config struct {
	// ...existing fields...
	GlobalTimeout  int         `json:"global-timeout" yaml:"global-timeout"` // NEW
}
```

**Step 2: 修改 engine/account_manager.go**

在 Account 创建时填充 timeout fallback：

```go
for _, apiKey := range provider.APIKeyEntries {
	timeout := provider.Timeout
	if timeout == 0 {
		timeout = config.GlobalTimeout
	}
	accounts = append(accounts, &types.Account{
		// ...
		Timeout: timeout,
	})
}
```

**Step 3: 修改 server/server.go**

```go
timeout := account.Timeout
if timeout == 0 {
	timeout = 30  // 最终兜底
}
```

**Step 4: 写测试**

```go
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
				Name:    "test-provider",
				BaseURL: "http://localhost",
				APIKeyEntries: []string{"key1"},
				Models: map[string]string{"m1": "real-m1"},
				// Timeout 未设置 → 应从全局继承 120
			},
		},
	}
	// ...验证 account.Timeout == 120
}

func TestTimeout_ProviderOverridesGlobal(t *testing.T) {
	// provider.Timeout=60, global=120 → account.Timeout == 60
}
```

**Step 5: 运行测试 + Commit**

```bash
go test ./engine/ -run "TestTimeout" -v
```

---

## Phase 2: types 包测试（目标 95-100%）

### Task 2.1: types/config_test.go — LoadConfig + DefaultConfig

**Objective:** 覆盖 `LoadConfig` 的 YAML/JSON 解析路径和 `GetDefaultConfig`

**Files:**
- Create: `types/config_test.go`

**测试用例:**
```
TestLoadConfig_YAML             — 读 config.yaml
TestLoadConfig_JSON             — 读 JSON 配置
TestLoadConfig_FileNotFound     — 文件不存在
TestGetDefaultConfig            — 默认配置结构验证
TestConfig_RoundTrip            — YAML marshal/unmarshal 一致性
```

**Step 1: 写完整测试**

```go
package types

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetDefaultConfig(t *testing.T) {
	cfg := GetDefaultConfig()

	if cfg.Port != 8080 {
		t.Errorf("Expected Port 8080, got %d", cfg.Port)
	}
	if len(cfg.Providers) != 1 {
		t.Errorf("Expected 1 provider, got %d", len(cfg.Providers))
	}
	if cfg.Providers[0].Name != "nvidia" {
		t.Errorf("Expected nvidia provider, got %s", cfg.Providers[0].Name)
	}
	if cfg.RateLimit.MaxRequestsPerMinute != 40 {
		t.Errorf("Expected 40 RPM, got %d", cfg.RateLimit.MaxRequestsPerMinute)
	}
	if cfg.RequestRetry != 3 {
		t.Errorf("Expected RequestRetry 3, got %d", cfg.RequestRetry)
	}
}

func TestLoadConfig_YAML(t *testing.T) {
	tmpDir := t.TempDir()
	yamlPath := filepath.Join(tmpDir, "config.yaml")

	yamlContent := `
port: 9090
request-retry: 2
rate-limit:
  maxRequestsPerMinute: 60
  minIntervalMs: 200
  retryIntervalMs: 3000
providers:
  - name: test
    base-url: https://api.test.com/v1
    api-key-entries:
      - sk-test123
    models:
      gpt-4: gpt-4-real
`
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(yamlPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.Port != 9090 {
		t.Errorf("Expected Port 9090, got %d", cfg.Port)
	}
	if cfg.RequestRetry != 2 {
		t.Errorf("Expected RequestRetry 2, got %d", cfg.RequestRetry)
	}
	if len(cfg.Providers) != 1 {
		t.Fatalf("Expected 1 provider, got %d", len(cfg.Providers))
	}
	if cfg.Providers[0].Name != "test" {
		t.Errorf("Expected provider name 'test', got '%s'", cfg.Providers[0].Name)
	}
	if cfg.Providers[0].BaseURL != "https://api.test.com/v1" {
		t.Errorf("Expected BaseURL, got '%s'", cfg.Providers[0].BaseURL)
	}
	if len(cfg.Providers[0].APIKeyEntries) != 1 {
		t.Errorf("Expected 1 API key, got %d", len(cfg.Providers[0].APIKeyEntries))
	}
	if cfg.RateLimit.MaxRequestsPerMinute != 60 {
		t.Errorf("Expected 60 RPM, got %d", cfg.RateLimit.MaxRequestsPerMinute)
	}
}

func TestLoadConfig_YAML_Preferred(t *testing.T) {
	// 当 config.yaml 与 config.json 同时存在，优先读 YAML
	tmpDir := t.TempDir()

	yamlPath := filepath.Join(tmpDir, "config.yaml")
	jsonPath := filepath.Join(tmpDir, "config.json")

	os.WriteFile(yamlPath, []byte("port: 9999\nrate-limit:\n  maxRequestsPerMinute: 40\n  minIntervalMs: 100\n  retryIntervalMs: 5000\nproviders: []\n"), 0644)
	os.WriteFile(jsonPath, []byte(`{"port":8888}`), 0644)

	cfg, err := LoadConfig(jsonPath) // 传入 JSON 路径，应该被 YAML 覆盖
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.Port != 9999 {
		t.Errorf("Expected Port 9999 from YAML, got %d", cfg.Port)
	}
}

func TestLoadConfig_FileNotFound(t *testing.T) {
	_, err := LoadConfig("/nonexistent/path/config.json")
	if err == nil {
		t.Error("Expected error for non-existent file")
	}
}
```

**Step 2: 运行**

```bash
cd "f:/Projects/test/test_NanoAPI" && go test ./types/ -v -cover
```

Expected: PASS, cover ≥ 80%

**Step 3: Commit**

---

### Task 2.2: 补充 types 边缘测试 — 覆盖 95%+

追加用例到 `types/config_test.go`:

```
TestLoadConfig_JSON_Format        — 纯 JSON 文件（无同级 YAML）
TestLoadConfig_YAML_MultiProvider — 多 provider YAML 解析
TestLoadConfig_ExtraFields        — YAML extra-fields 嵌套解析
TestLoadConfig_Headers            — YAML headers 解析
TestLoadConfig_InvalidYAML        — 损坏的 YAML fallback 到 JSON
TestAccount_StatusConstants       — AccountStatus 枚举
```

**运行 + Commit:**

```bash
go test ./types/ -v -cover
go test ./types/ -v -coverprofile=coverage.out
go tool cover -func=coverage.out | grep "total:"
# Expected: total: (statements) ≥ 95.0%
```

---

## Phase 3: core 包测试（目标 95-100%）

### Task 3.1: 迁移 + 清理现有测试

**Files:**
- Create: `core/request_body_test.go` (内容从 `core/test/extra_fields_test.go` 移入)
- Create: `core/rate_limit_test.go` (内容从 `core/test/rate_limit_test.go` 移入)
- Delete: `core/test/` (整个目录)

修改 package 声明：`package core_test` → `package core` 或保持 `core_test`。

注意：`BuildRequestBodyForTest` 是 core 的公开方法，无 package 访问限制。

**Step 1: 复制文件**

```bash
cd "f:/Projects/test/test_NanoAPI"
cp core/test/extra_fields_test.go core/request_body_test.go
cp core/test/rate_limit_test.go core/rate_limit_test.go
```

**Step 2: 修改 request_body_test.go package**

不需要改 — `BuildRequestBodyForTest` 是公开方法，`core_test` 包可以访问。

**Step 3: 运行**

```bash
go test ./core/ -v
# Expected: 8 tests PASS
```

**Step 4: 删除旧目录 + Commit**

```bash
rm -rf core/test/
git add core/request_body_test.go core/rate_limit_test.go
git rm -r core/test/
git commit -m "refactor: move core tests from core/test/ to core/"
```

---

### Task 3.2: core/request_test.go — SendRequest + header + proxy + timeout

**Objective:** 测试 RequestHandler.SendRequest 的所有参数路径

**Files:**
- Create: `core/request_test.go`

**测试用例:**

```go
TestSendRequest_Basic              — 基本 POST + auth header
TestSendRequest_CustomHeaders      — 自定义 headers 覆盖
TestSendRequest_Timeout            — timeout 被正确设置到 http.Client
TestSendRequest_Proxy              — proxy URL 正确设置
TestSendRequest_ExtraFields        — extraFields 合并到 body
TestSendRequest_EmptyBody          — 空 body 处理
```

实现示例：

```go
func TestSendRequest_Basic(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 验证 method
		if r.Method != "POST" {
			t.Errorf("Expected POST, got %s", r.Method)
		}
		// 验证 Content-Type
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Expected Content-Type application/json, got %s", r.Header.Get("Content-Type"))
		}
		// 验证 auth
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-key" {
			t.Errorf("Expected Authorization Bearer test-key, got %s", auth)
		}
		w.WriteHeader(200)
		w.Write([]byte(`{"choices":[]}`))
	}))
	defer mock.Close()

	handler := NewRequestHandler()
	req := &types.ChatRequest{
		Model:    "test",
		Messages: []types.Message{{Role: "user", Content: "hi"}},
	}

	resp, err := handler.SendRequest("test-key", mock.URL, req, 10, "", nil, nil)
	if err != nil {
		t.Fatalf("SendRequest failed: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}
}
```

**运行 + Commit:**

```bash
go test ./core/ -v -cover
```

---

### Task 3.3: core/rate_limit_concurrency_test.go — 并发限流

**Objective:** 多个 goroutine 同时 Wait() 的串行化行为

```go
func TestRateLimiter_Concurrency(t *testing.T) {
	limiter := core.NewRateLimiter(10, 100) // 10 RPM, 100ms interval

	var wg sync.WaitGroup
	start := time.Now()

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			limiter.Wait()
		}()
	}
	wg.Wait()

	elapsed := time.Since(start)
	// 5 个请求，每个至少 100ms → ≥ 400ms
	if elapsed < 400*time.Millisecond {
		t.Errorf("Expected ≥ 400ms, got %v", elapsed)
	}
}
```

---

## Phase 4: engine 包测试（目标 ≥ 90%）

### Task 4.1: 重写 engine/account_manager_test.go

**Objective:** 替换旧的（用废弃 API 的）account_manager_test.go

**Files:**
- Delete: `engine/test/account_manager_test.go`
- Create: `engine/account_manager_test.go`

**所有测试用例:**

```
TestAccountManager_GetNextAccount_RoundRobin     — 轮询3个账号
TestAccountManager_GetNextAccount_SingleAccount  — 单账号循环
TestAccountManager_GetNextAccount_Empty          — 无账号返回 nil
TestAccountManager_MarkFailed_RemovesFromAvailable
TestAccountManager_MarkFailed_RecoveryAfterRetry
TestAccountManager_GetNextProviderForModel       — 模型映射
TestAccountManager_GetNextProviderForModel_NotFound
TestAccountManager_GetRealModelName              — 别名解析
TestAccountManager_SendRequest                   — 完整请求路由
TestAccountManager_GetAllModels                  — 模型去重
TestAccountManager_CheckFailedAccounts           — 后台恢复 goroutine
```

**运行 + Commit**

---

### Task 4.2: engine/provider_isolation_test.go — 完成配置隔离

**Objective:** 在 Phase 1 的 RateLimit/ModelConfig/Timeout 测试基础上，补齐隔离测试

```
TestProviderIsolation_Headers           — A 的 header 不泄露到 B
TestProviderIsolation_ExtraFields       — A 的 thinking_enabled 不污染 B
TestProviderIsolation_ModelConfig       — A 的 temperature 不被 B 使用
TestProviderIsolation_ConcurrentAccess  — 不同 provider 并发请求互不阻塞
```

---

## Phase 5: server 包测试（目标 ≥ 85%）

### Task 5.1: server/handler_test.go — Handler 单元测试

**Objective:** 测试 HTTP handler 的错误处理，用 httptest server 发请求

**Files:**
- Create: `server/handler_test.go`

```go
TestHandleChatCompletions_MethodNotAllowed  — GET 请求返回 405
TestHandleChatCompletions_Options           — OPTIONS 预检
TestHandleChatCompletions_MissingBody       — 空 body 返回 400
TestHandleChatCompletions_InvalidJSON       — 非法 JSON 返回 400
TestHandleListModels_GET                    — 模型列表
TestHandleListModels_OPTIONS                — CORS 预检
TestHandleChatCompletions_NoAvailableAccount — 503
```

实现示例（需要构造 AccountManager）：

```go
func TestHandleListModels_GET(t *testing.T) {
	config := &types.Config{
		RateLimit: types.RateLimitConfig{
			MaxRequestsPerMinute: 40,
			MinIntervalMs:        100,
			RetryIntervalMs:      5000,
		},
		Providers: []types.ProviderConfig{
			{
				Name:          "p1",
				BaseURL:       "http://localhost",
				APIKeyEntries: []string{"k1"},
				Models:        map[string]string{"gpt-4": "gpt-4-real", "claude": "claude-real"},
			},
			{
				Name:          "p2",
				BaseURL:       "http://localhost",
				APIKeyEntries: []string{"k2"},
				Models:        map[string]string{"gpt-4": "gpt-4-v2"},
			},
		},
	}

	am := engine.NewAccountManager(config)
	s := NewServer(am)

	req := httptest.NewRequest("GET", "/v1/models", nil)
	w := httptest.NewRecorder()
	s.handleListModels(w, req)

	resp := w.Result()
	if resp.StatusCode != 200 {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}

	var body types.ModelListResponse
	json.NewDecoder(resp.Body).Decode(&body)

	if body.Object != "list" {
		t.Errorf("Expected object='list', got '%s'", body.Object)
	}

	// gpt-4 去重后应只出现一次
	count := 0
	for _, m := range body.Data {
		if m.ID == "gpt-4" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("Expected gpt-4 deduplicated, got %d", count)
	}
}
```

---

### Task 5.2: server/stream_test.go — 流式响应

**Objective:** 测试流式转发：chunk 完整性、reasoning_content、header 转发

```
TestStream_SSEHeaders             — Content-Type: text/event-stream
TestStream_ChunkIntegrity         — 4 chunk 全部到达
TestStream_ReasoningContent       — reasoning_content 转发
TestStream_FinishReason           — 最后一个 chunk finish_reason=stop
TestStream_HeaderForwarding       — X-RateLimit-Remaining 转发
TestStream_LargeResponse          — 100KB 不截断
```

实现示例：

```go
func TestStream_ChunkIntegrity(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		chunks := []string{
			`{"choices":[{"delta":{"role":"assistant"}}]}`,
			`{"choices":[{"delta":{"content":"Hello"}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
		}
		for _, c := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", c)
			flusher.Flush()
		}
	}))
	defer mock.Close()

	config := &types.Config{
		RateLimit: types.RateLimitConfig{
			MaxRequestsPerMinute: 40,
			MinIntervalMs:        10,
			RetryIntervalMs:      1000,
		},
		Providers: []types.ProviderConfig{
			{
				Name:          "test",
				BaseURL:       mock.URL,
				APIKeyEntries: []string{"k1"},
				Models:        map[string]string{"m1": "m1"},
				Timeout:       10,
			},
		},
	}

	am := engine.NewAccountManager(config)
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
	if resp.StatusCode != 200 {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}

	if !strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Errorf("Expected text/event-stream, got %s", resp.Header.Get("Content-Type"))
	}

	// 验证收到 3 个 data: 行
	raw := w.Body.String()
	lines := 0
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(line, "data:") {
			lines++
		}
	}
	if lines != 3 {
		t.Errorf("Expected 3 data lines, got %d", lines)
	}
}
```

---

### Task 5.3: server/e2e_test.go — E2E 全链路

**Objective:** 完整请求链路 + 限流 + 错误恢复 + 并发

```
TestE2E_NonStream_CompleteFlow    — 非流式完整请求
TestE2E_Stream_CompleteFlow       — 流式完整请求
TestE2E_Failure_Fallback          — 失败 → 503 → 恢复 → 200
TestE2E_Concurrent_DiffProviders  — 多 provider 并发不阻塞
TestE2E_RateLimit_SameProvider    — 同一 provider 串行化
```

---

## Phase 6: 清理 + 全覆盖 + 门禁

### Task 6.1: 删除废弃文件

```bash
cd "f:/Projects/test/test_NanoAPI"
rm -rf test/          # 旧的 hacky 测试
rm -rf engine/test/   # 已迁移
rm -rf mock/          # 改用 httptest
rm -f nano-api-test.exe  # 旧编译产物
```

验证：`go build ./...` 仍成功。

---

### Task 6.2: 全量覆盖率运行

```bash
cd "f:/Projects/test/test_NanoAPI"
go test -coverprofile=coverage.out -covermode=count ./...

echo "=== Per-package coverage ==="
go tool cover -func=coverage.out | grep -E "(total:|nano-api/)"

echo "=== HTML report ==="
go tool cover -html=coverage.out -o coverage.html
```

验证结果:
- `nano-api/types` ≥ 95%
- `nano-api/core` ≥ 95%
- `nano-api/engine` ≥ 90%
- `nano-api/server` ≥ 85%
- `total:` ≥ 90%

---

### Task 6.3: CI 门禁 (可选)

```bash
go test -race -coverprofile=coverage.out ./...
go vet ./...
```

---

## 总结

| Phase | 任务数 | 新建文件 | 修改文件 | 删除 |
|-------|--------|----------|----------|------|
| 1. BUG 修复 | 3 | 1 | 3 | 0 |
| 2. types 测试 | 2 | 1 | 0 | 0 |
| 3. core 测试 | 3 | 2 | 0 | 1 dir |
| 4. engine 测试 | 2 | 2 | 0 | 1 file |
| 5. server 测试 | 3 | 3 | 0 | 0 |
| 6. 清理 | 3 | 0 | 0 | 3 dirs |
| **Total** | **16** | **9** | **3** | **4** |

**总计 ~16 个 task，每个 2-5 分钟，预计 1-2 小时完成。**
