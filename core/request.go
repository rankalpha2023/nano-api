package core

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"time"

	"nano-api/types"
)

// RequestHandler 请求处理器
type RequestHandler struct {
}

// NewRequestHandler 创建新的请求处理器
func NewRequestHandler() *RequestHandler {
	return &RequestHandler{}
}

// SendRequest 发送请求到nvidia服务器
func (h *RequestHandler) SendRequest(apiKey, baseURL string, req *types.ChatRequest, timeout int, proxy string, headers map[string]string) (*http.Response, error) {
	// 构建请求体
	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, err
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
		return nil, err
	}

	// 设置请求头
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

	// 添加自定义请求头（会覆盖已有的头）
	for key, value := range headers {
		httpReq.Header.Set(key, value)
	}

	// 发送请求
	return client.Do(httpReq)
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
