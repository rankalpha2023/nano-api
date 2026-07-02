package core

import (
	"encoding/json"
	"fmt"
	"log"

	"nano-api/types"
)

// BuildRequestBody 构建请求体，合并 ExtraFields 到 ChatRequest（纯函数）。
// 从 RequestHandler 方法提取为独立纯函数，保留在 core 层。
// engine 层的 RequestHandler 通过调用此函数完成请求体构建。
func BuildRequestBody(req *types.ChatRequest, extraFields map[string]interface{}) ([]byte, error) {
	// 如果没有 ExtraFields，直接序列化原始请求（快速路径）
	if len(extraFields) == 0 {
		return json.Marshal(req)
	}

	// 将 ChatRequest 序列化为 map
	reqMap := make(map[string]interface{})
	reqData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	if err := json.Unmarshal(reqData, &reqMap); err != nil {
		return nil, fmt.Errorf("failed to unmarshal request to map: %w", err)
	}

	// 合并 ExtraFields（ExtraFields 会覆盖同名的 ChatRequest 字段）
	for key, value := range extraFields {
		reqMap[key] = value
		log.Printf("[ExtraFields] Applied field: %s = %v", key, value)
	}

	// 重新序列化为 JSON
	result, err := json.Marshal(reqMap)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal merged request: %w", err)
	}

	return result, nil
}
