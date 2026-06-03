package types

import "time"

// Account 帐号结构
type Account struct {
	ProviderName string                 `json:"providerName"` // 提供商名称
	BaseURL      string                 `json:"baseURL"`      // 基础URL
	APIKey       string                 `json:"apiKey"`       // API密钥
	Timeout      int                    `json:"timeout"`      // 请求超时时间(秒)
	Proxy        string                 `json:"proxy"`       // 代理地址
	Headers      map[string]string      `json:"headers"` // 自定义请求头
	ExtraFields  map[string]interface{} `json:"extraFields"` // 额外的请求体字段（从 ProviderConfig 复制）
	Status       AccountStatus          `json:"status"`       // 帐号状态
	LastUsed     time.Time              `json:"lastUsed"`     // 最后使用时间
	LastFailed   time.Time              `json:"lastFailed"`   // 最后失败时间
	RequestCount int                    `json:"requestCount"` // 请求计数
}

// AccountStatus 帐号状态枚举
type AccountStatus string

const (
	AccountStatusAvailable AccountStatus = "available" // 可用
	AccountStatusFailed    AccountStatus = "failed"    // 失败
	AccountStatusDisabled  AccountStatus = "disabled"  // 禁用
)
