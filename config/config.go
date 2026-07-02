package config

import (
	"encoding/json"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"nano-api/types"
)

// LoadConfig 加载配置文件，优先读取同级目录下的 config.yaml。
// 此函数执行文件 I/O（属副作用编排），按四段式架构归入 Engine 层职责，
// 不应放在 Types 层（Types 层严禁包含任何逻辑）。
func LoadConfig(filePath string) (*types.Config, error) {
	// 优先尝试读取 yaml 文件
	yamlPath := filepath.Join(filepath.Dir(filePath), "config.yaml")
	if data, err := os.ReadFile(yamlPath); err == nil {
		var config types.Config
		if err := yaml.Unmarshal(data, &config); err == nil {
			return &config, nil
		}
	}

	// 如果 yaml 不存在或解析失败，尝试读取原始文件
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	// 根据文件扩展名判断格式
	ext := filepath.Ext(filePath)
	if ext == ".yaml" || ext == ".yml" {
		var config types.Config
		if err := yaml.Unmarshal(data, &config); err != nil {
			return nil, err
		}
		return &config, nil
	}

	// 尝试 json 格式
	var config types.Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

// GetDefaultConfig 获取默认配置（数据工厂）。
func GetDefaultConfig() *types.Config {
	return &types.Config{
		Providers: []types.ProviderConfig{
			{
				Name:          "nvidia",
				BaseURL:       "https://integrate.api.nvidia.com/v1",
				APIKeyEntries: []string{"your_nvidia_api_key_here"},
				Models: map[string]string{
					"high":   "z-ai/glm-5.1",
					"medium": "z-ai/glm-4",
					"low":    "z-ai/glm-3.1",
				},
				RateLimit: types.RateLimitConfig{
					MaxRequestsPerMinute: 40,   // nvidia限制
					MinIntervalMs:        100,  // 最小间隔100ms
					RetryIntervalMs:      5000, // 失败重试间隔5秒
				},
				ModelConfig: types.ModelConfig{
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
		RateLimit: types.RateLimitConfig{
			MaxRequestsPerMinute: 40,
			MinIntervalMs:        100,
			RetryIntervalMs:      5000,
		},
	}
}
