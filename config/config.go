package config

import (
	"encoding/json"
	"os"
	"path/filepath"

	"nano-api/types"

	"gopkg.in/yaml.v3"
)

// LoadConfig 加载配置文件，优先读取yaml格式
func LoadConfig(filePath string) (*types.Config, error) {
	// 优先尝试读取yaml文件
	yamlPath := filepath.Join(filepath.Dir(filePath), "config.yaml")
	if data, err := os.ReadFile(yamlPath); err == nil {
		var config types.Config
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
		var config types.Config
		if err := yaml.Unmarshal(data, &config); err != nil {
			return nil, err
		}
		return &config, nil
	}

	// 尝试json格式
	var config types.Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

// GetDefaultConfig 获取默认配置
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
