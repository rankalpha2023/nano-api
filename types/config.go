package types

// Config 全局配置结构
type Config struct {
	Providers         []ProviderConfig `json:"providers" yaml:"providers"`             // 提供商配置
	Port              int              `json:"port" yaml:"port"`                       // 服务器端口
	RequestRetry      int              `json:"requestRetry" yaml:"requestRetry"`       // 请求失败时的重试次数
	RateLimit         RateLimitConfig  `json:"rate-limit" yaml:"rate-limit"`           // 全局限流配置
	GlobalModelConfig ModelConfig      `json:"global-model-config" yaml:"global-model-config"` // 全局模型参数默认值
	GlobalTimeout     int              `json:"global-timeout" yaml:"global-timeout"`   // 全局请求超时(秒)
}

// ProviderConfig 提供商配置
type ProviderConfig struct {
	Name          string                 `json:"name" yaml:"name"`            // 提供商名称
	BaseURL       string                 `json:"base-url" yaml:"base-url"`    // 基础URL
	APIKeyEntries []string               `json:"api-key-entries" yaml:"api-key-entries"` // API密钥数组
	Models        map[string]string      `json:"models" yaml:"models"`          // 支持的模型名称和别名
	RateLimit     RateLimitConfig        `json:"rate-limit" yaml:"rate-limit"`  // 限流配置
	ModelConfig   ModelConfig            `json:"model-config" yaml:"model-config"`    // 模型参数配置
	Timeout       int                    `json:"timeout" yaml:"timeout"`         // 请求超时时间(秒)
	Proxy         string                 `json:"proxy" yaml:"proxy"`           // 代理地址
	Headers       map[string]string      `json:"headers" yaml:"headers"`       // 自定义请求头（会覆盖请求中的同名headers）
	ExtraFields   map[string]interface{} `json:"extra-fields" yaml:"extra-fields"` // 额外的请求体字段
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
