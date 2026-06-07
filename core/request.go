package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"time"

	"nano-api/types"
)

// RequestMetrics 单次远程请求的性能指标
type RequestMetrics struct {
	RequestBytes  int           // 出站请求体字节数
	RemoteLatency time.Duration // 从 client.Do 发出到收到 HTTP 响应的耗时
}

// RequestHandler 请求处理器
type RequestHandler struct {
}

// NewRequestHandler 创建新的请求处理器
func NewRequestHandler() *RequestHandler {
	return &RequestHandler{}
}

// SendRequest 发送请求到目标服务器，同时返回性能指标
func (h *RequestHandler) SendRequest(apiKey, baseURL string, req *types.ChatRequest, timeout int, proxy string, headers map[string]string, extraFields map[string]interface{}) (*http.Response, *RequestMetrics, error) {
	// 构建请求体（包含 ExtraFields 合并逻辑）
	reqBody, err := h.buildRequestBody(req, extraFields)
	if err != nil {
		return nil, nil, err
	}

	// 创建HTTP客户端
	client := &http.Client{
		Timeout: time.Duration(timeout) * time.Second,
	}

	// 设置代理
	if proxy != "" {
		proxyURL, err := url.Parse(proxy)
		if err == nil {
			client.Transport = &http.Transport{Proxy: http.ProxyURL(proxyURL)}
		}
	}

	// 创建HTTP请求
	httpReq, err := http.NewRequest("POST", baseURL+"/chat/completions", bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, nil, err
	}

	// 设置请求头
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

	// 添加自定义请求头（会覆盖已有的头）
	for key, value := range headers {
		httpReq.Header.Set(key, value)
	}

	// 记录性能指标
	metrics := &RequestMetrics{
		RequestBytes: len(reqBody),
	}

	// 发送请求并计时
	start := time.Now()
	resp, err := client.Do(httpReq)
	metrics.RemoteLatency = time.Since(start)

	return resp, metrics, err
}

// buildRequestBody 构建请求体，合并 ExtraFields 到 ChatRequest
func (h *RequestHandler) buildRequestBody(req *types.ChatRequest, extraFields map[string]interface{}) ([]byte, error) {
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

// BuildRequestBodyForTest 测试辅助方法：暴露 buildRequestBody 功能
func (h *RequestHandler) BuildRequestBodyForTest(req *types.ChatRequest, extraFields map[string]interface{}) ([]byte, error) {
	return h.buildRequestBody(req, extraFields)
}

// StreamResponse 处理流式响应
func (h *RequestHandler) StreamResponse(resp *http.Response, callback func(*types.Choice) error) error {
	defer resp.Body.Close()

	// 创建JSON解码器
	decoder := json.NewDecoder(resp.Body)

	// 逐行读取响应
	for {
		var chunk struct {
			Choices []types.Choice `json:"choices"`
		}

		if err := decoder.Decode(&chunk); err != nil {
			if err == io.EOF {
				break
			}
			return err
		}

		// 处理每个选择
		for _, choice := range chunk.Choices {
			if err := callback(&choice); err != nil {
				return err
			}
		}
	}

	return nil
}
