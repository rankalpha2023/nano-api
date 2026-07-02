package config

import (
	"os"
	"path/filepath"
	"testing"

	"nano-api/types"
)

// makeTempDir 创建临时目录（跨平台，使用 t.TempDir 替代硬编码 /tmp）
func makeTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return dir
}

func TestGetDefaultConfig(t *testing.T) {
	cfg := GetDefaultConfig()

	if cfg.Port != 8080 {
		t.Errorf("Expected Port 8080, got %d", cfg.Port)
	}
	if cfg.RequestRetry != 3 {
		t.Errorf("Expected RequestRetry 3, got %d", cfg.RequestRetry)
	}
	if len(cfg.Providers) != 1 {
		t.Fatalf("Expected 1 provider, got %d", len(cfg.Providers))
	}
	if cfg.Providers[0].Name != "nvidia" {
		t.Errorf("Expected nvidia provider, got %s", cfg.Providers[0].Name)
	}
	if cfg.RateLimit.MaxRequestsPerMinute != 40 {
		t.Errorf("Expected 40 RPM, got %d", cfg.RateLimit.MaxRequestsPerMinute)
	}
}

func TestLoadConfig_YAML(t *testing.T) {
	dir := makeTempDir(t)
	p := filepath.Join(dir, "config.yaml")

	os.WriteFile(p, []byte(`
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
`), 0644)

	cfg, err := LoadConfig(p)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if cfg.Port != 9090 {
		t.Errorf("Port: got %d", cfg.Port)
	}
	if cfg.Providers[0].Name != "test" {
		t.Errorf("Name: got %s", cfg.Providers[0].Name)
	}
	if cfg.Providers[0].APIKeyEntries[0] != "sk-test123" {
		t.Errorf("APIKey: got %s", cfg.Providers[0].APIKeyEntries[0])
	}
	if cfg.RateLimit.MaxRequestsPerMinute != 60 {
		t.Errorf("RPM: got %d", cfg.RateLimit.MaxRequestsPerMinute)
	}
}

func TestLoadConfig_YAML_PrefersConfigYAML(t *testing.T) {
	dir := makeTempDir(t)
	os.WriteFile(filepath.Join(dir, "config.yaml"),
		[]byte("port: 9999\nrate-limit:\n  maxRequestsPerMinute: 40\n  minIntervalMs: 100\n  retryIntervalMs: 5000\nproviders: []\n"), 0644)
	os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"port":8888}`), 0644)

	cfg, err := LoadConfig(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if cfg.Port != 9999 {
		t.Errorf("Expected 9999 from config.yaml, got %d", cfg.Port)
	}
}

func TestLoadConfig_FileNotFound(t *testing.T) {
	_, err := LoadConfig(filepath.Join(os.TempDir(), "nonexistent-config-"+uniqueSuffix(), "config.json"))
	if err == nil {
		t.Error("Expected error")
	}
}

func TestLoadConfig_MultiProvider(t *testing.T) {
	dir := makeTempDir(t)
	p := filepath.Join(dir, "config.yaml")
	os.WriteFile(p, []byte(`
rate-limit:
  maxRequestsPerMinute: 40
  minIntervalMs: 100
  retryIntervalMs: 5000
providers:
  - name: p1
    base-url: https://api1.com/v1
    api-key-entries: [key1]
    models:
      gpt-4: gpt-4-real
  - name: p2
    base-url: https://api2.com/v1
    api-key-entries: [key2a, key2b]
    models:
      claude: claude-real
`), 0644)

	cfg, err := LoadConfig(p)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if len(cfg.Providers) != 2 {
		t.Fatalf("Expected 2 providers, got %d", len(cfg.Providers))
	}
	if len(cfg.Providers[1].APIKeyEntries) != 2 {
		t.Errorf("Expected 2 keys for p2, got %d", len(cfg.Providers[1].APIKeyEntries))
	}
}

func TestLoadConfig_ExtraFields(t *testing.T) {
	dir := makeTempDir(t)
	p := filepath.Join(dir, "config.yaml")
	os.WriteFile(p, []byte(`
rate-limit:
  maxRequestsPerMinute: 40
  minIntervalMs: 100
  retryIntervalMs: 5000
providers:
  - name: test
    base-url: https://api.test.com/v1
    api-key-entries: [sk-test]
    models:
      m1: real-m1
    extra-fields:
      thinking_enabled: true
      custom_param: "hello"
`), 0644)
	cfg, err := LoadConfig(p)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	ef := cfg.Providers[0].ExtraFields
	if len(ef) == 0 {
		t.Fatal("Expected ExtraFields")
	}
	if ef["thinking_enabled"] != true {
		t.Errorf("thinking_enabled: %v", ef["thinking_enabled"])
	}
	if ef["custom_param"] != "hello" {
		t.Errorf("custom_param: %v", ef["custom_param"])
	}
}

func TestLoadConfig_Headers(t *testing.T) {
	dir := makeTempDir(t)
	p := filepath.Join(dir, "config.yaml")
	os.WriteFile(p, []byte(`
rate-limit:
  maxRequestsPerMinute: 40
  minIntervalMs: 100
  retryIntervalMs: 5000
providers:
  - name: test
    base-url: https://api.test.com/v1
    api-key-entries: [sk-test]
    models:
      m1: real-m1
    headers:
      X-Custom: custom-value
      User-Agent: TestAgent/1.0
`), 0644)
	cfg, err := LoadConfig(p)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if cfg.Providers[0].Headers["X-Custom"] != "custom-value" {
		t.Errorf("X-Custom: %s", cfg.Providers[0].Headers["X-Custom"])
	}
}

func TestLoadConfig_InvalidYAML(t *testing.T) {
	dir := makeTempDir(t)
	p := filepath.Join(dir, "config.yaml")
	os.WriteFile(p, []byte(": invalid yaml :::"), 0644)
	_, err := LoadConfig(p)
	if err == nil {
		t.Error("Expected error for invalid YAML")
	}
}

// TestLoadConfig_JSON_Fallback 测试 JSON-only 路径（无 config.yaml）
func TestLoadConfig_JSON_Fallback(t *testing.T) {
	dir := makeTempDir(t)
	p := filepath.Join(dir, "config.json")
	os.WriteFile(p, []byte(`{"port":7777,"requestRetry":5,"rate-limit":{"maxRequestsPerMinute":30,"minIntervalMs":50,"retryIntervalMs":3000},"providers":[{"name":"json-provider","base-url":"https://json.example.com","api-key-entries":["json-key"],"models":{"m1":"real-m1"}}]}`), 0644)

	cfg, err := LoadConfig(p)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if cfg.Port != 7777 {
		t.Errorf("Port: got %d", cfg.Port)
	}
}

// TestLoadConfig_YAML_ExtensionNoConfigYAML 测试：传入 .yaml 文件但同级无 config.yaml
func TestLoadConfig_YAML_ExtensionNoConfigYAML(t *testing.T) {
	dir := makeTempDir(t)
	// 不创建 config.yaml，只创建传入的 .yaml 文件
	p := filepath.Join(dir, "test.yaml")
	os.WriteFile(p, []byte("port: 5555\nrate-limit:\n  maxRequestsPerMinute: 40\n  minIntervalMs: 100\n  retryIntervalMs: 5000\nproviders: []\n"), 0644)

	cfg, err := LoadConfig(p)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if cfg.Port != 5555 {
		t.Errorf("Port: expected 5555, got %d", cfg.Port)
	}
}

// TestLoadConfig_InvalidJSON 测试：无效 JSON（无 config.yaml，传入损坏 JSON 文件）
func TestLoadConfig_InvalidJSON(t *testing.T) {
	dir := makeTempDir(t)
	p := filepath.Join(dir, "config.json")
	os.WriteFile(p, []byte("not json"), 0644)

	_, err := LoadConfig(p)
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}

// uniqueSuffix 生成唯一后缀用于不存在的路径
func uniqueSuffix() string {
	return "1234567890"
}

// TestLoadConfig_ReturnsTypesConfig 验证返回值是 *types.Config 类型
// 回归测试：确保迁移到 config 包后类型不变
func TestLoadConfig_ReturnsTypesConfig(t *testing.T) {
	dir := makeTempDir(t)
	p := filepath.Join(dir, "config.yaml")
	os.WriteFile(p, []byte("port: 1234\nrate-limit:\n  maxRequestsPerMinute: 40\n  minIntervalMs: 100\n  retryIntervalMs: 5000\nproviders: []\n"), 0644)

	cfg, err := LoadConfig(p)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	// 显式验证类型
	var _ *types.Config = cfg
	if cfg.Port != 1234 {
		t.Errorf("Port: expected 1234, got %d", cfg.Port)
	}
}
