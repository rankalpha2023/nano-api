package core

import "strings"

// MaskAPIKey 对 API-KEY 进行打码处理（纯函数，无副作用）。
// 从 main.go 迁入 core 层：符合"Core 层 = 纯函数计算"的架构约束。
func MaskAPIKey(apiKey string) string {
	if len(apiKey) <= 8 {
		return apiKey
	}
	prefix := apiKey[:4]
	suffix := apiKey[len(apiKey)-4:]
	stars := strings.Repeat("*", len(apiKey)-8)
	return prefix + stars + suffix
}
