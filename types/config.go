package types

import (
	"encoding/json"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config 全局配置结构
type Config struct {
	Providers    []ProviderConfig `json:"providers" yaml:"providers"`    // 提供商配置
	Port         int              `json:"port" yaml:"port"`         // 服务器端口
	RequestRetry int              `json:"requestRetry" yaml:"requestRetry"` // 请求失败时的重试次数
	RateLimit    RateLimitConfig  `json:"rate-limit" yaml:"rate-limit"` // 全局限流配置
}

// ProviderConfig 提供商配置
type ProviderConfig struct {
	Name          string            `json:"name" yaml:"name"`            // 提供商名称
	BaseURL       string            `json:"base-url" yaml:"base-url"`    // 基础URL
	APIKeyEntries []string          `json:"api-key-entries" yaml:"api-key-entries"` // API密钥数组
	Models        map[string]string `json:"models" yaml:"models"`          // 支持的模型名称和别名
	RateLimit     RateLimitConfig   `json:"rate-limit" yaml:"rate-limit"`  // 限流配置
	ModelConfig   ModelConfig       `json:"model-config" yaml:"model-config"`    // 模型参数配置
	Timeout       int               `json:"timeout" yaml:"timeout"`         // 请求超时时间(秒)
	Proxy         string            `json:"proxy" yaml:"proxy"`           // 代理地址
	Headers       map[string]string `json:"headers" yaml:"headers"`       // 自定义请求头（会覆盖请求中的同名headers）
}

// RateLimitConfig 限流配置
type RateLimitConfig struct {
	MaxRequestsPerMinute int `json:"maxRequestsPerMinute" yaml:"maxRequestsPerMinute"` // 每分钟最大请求数
	MinIntervalMs        int `json:"minIntervalMs" yaml:"minIntervalMs"`        // 最小发送时间间隔(毫秒)
	RetryIntervalMs      int `json:"retryIntervalMs" yaml:"retryIntervalMs"`      // 失败重试间隔(毫秒)
}

// ModelConfig 模型参数配置
type ModelConfig struct {
	DefaultTemperature float64 `json:"defaultTemperature" yaml:"defaultTemperature"` // 默认温度参数
	DefaultTopP        float64 `json:"defaultTopP" yaml:"defaultTopP"`        // 默认Top P参数
	DefaultMaxTokens   int     `json:"defaultMaxTokens" yaml:"defaultMaxTokens"`   // 默认最大token数
}

// LoadConfig 加载配置文件，优先读取yaml格式
func LoadConfig(filePath string) (*Config, error) {
	// 优先尝试读取yaml文件
	yamlPath := filepath.Join(filepath.Dir(filePath), "config.yaml")
	if data, err := os.ReadFile(yamlPath); err == nil {
		var config Config
		if err := yaml.Unmarshal(data, &config); err == nil {
			return &config, nil
		}
	}

	// 如果yaml不存在或解析失败，尝试读取原始文件
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	// 根据文件扩展名判断格式
	ext := filepath.Ext(filePath)
	if ext == ".yaml" || ext == ".yml" {
		var config Config
		if err := yaml.Unmarshal(data, &config); err != nil {
			return nil, err
		}
		return &config, nil
	}

	// 尝试json格式
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

// GetDefaultConfig 获取默认配置
func GetDefaultConfig() *Config {
	return &Config{
		Providers: []ProviderConfig{
			{
				Name:          "nvidia",
				BaseURL:       "https://integrate.api.nvidia.com/v1",
				APIKeyEntries: []string{"your_nvidia_api_key_here"},
				Models: map[string]string{
					"high":   "z-ai/glm-5.1",
					"medium": "z-ai/glm-4",
					"low":    "z-ai/glm-3.1",
				},
				RateLimit: RateLimitConfig{
					MaxRequestsPerMinute: 40,   // nvidia限制
					MinIntervalMs:        100,  // 最小间隔100ms
					RetryIntervalMs:      5000, // 失败重试间隔5秒
				},
				ModelConfig: ModelConfig{
					DefaultTemperature: 1.0,  // 默认温度参数
					DefaultTopP:        1.0,  // 默认Top P参数
					DefaultMaxTokens:   1000, // 默认最大token数
				},
				Timeout: 30,
				Proxy:   "",
				Headers: make(map[string]string),
			},
		},
		Port:         8080, // 默认端口
		RequestRetry: 3,    // 默认重试次数
		RateLimit: RateLimitConfig{
			MaxRequestsPerMinute: 40,
			MinIntervalMs:        100,
			RetryIntervalMs:      5000,
		},
	}
}
