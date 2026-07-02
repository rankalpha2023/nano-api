package engine

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"

	"nano-api/core"
	"nano-api/types"
)

// RequestMetrics 单次远程请求的性能指标
type RequestMetrics struct {
	RequestBytes  int           // 出站请求体字节数
	RemoteLatency time.Duration // 从 client.Do 发出到收到 HTTP 响应的耗时
	Retries       int           // 因连接级错误触发的重试次数
}

// RequestHandler 请求处理器（I/O + 状态，属 Engine 层）。
//
// F1 修复：缓存 per-(baseURL, proxy) 的 *http.Transport，使连接池跨请求共享，
// 并保证无论是否使用代理，Transport 配置都完整（TLS、拨号、连接池、HTTP/2、超时）。
// *http.Client 仍按请求创建（轻量），但复用缓存的 Transport。
//
// 架构注：从 core 层迁入 engine 层。Core 层禁止副作用与外部环境访问，
// HTTP I/O 属副作用，归 Engine 层职责。纯逻辑（BuildRequestBody / IsRetryableConnErr）
// 已提取至 core 包，由本类型调用。
type RequestHandler struct {
	mu         sync.Mutex
	transports map[string]*http.Transport // key = baseURL + "|" + proxy
	retryCount int                        // F2: 连接级错误重试次数（默认 1）
}

// NewRequestHandler 创建新的请求处理器（默认重试 1 次）
func NewRequestHandler() *RequestHandler {
	return &RequestHandler{
		transports: make(map[string]*http.Transport),
		retryCount: 1,
	}
}

// NewRequestHandlerWithRetry 创建带指定重试次数的请求处理器
// retryCount < 0 时视为 0；= 0 表示不重试
func NewRequestHandlerWithRetry(retryCount int) *RequestHandler {
	if retryCount < 0 {
		retryCount = 0
	}
	return &RequestHandler{
		transports: make(map[string]*http.Transport),
		retryCount: retryCount,
	}
}

// GetRetryCount 返回当前重试次数配置（供测试使用）
func (h *RequestHandler) GetRetryCount() int {
	return h.retryCount
}

// getTransport 返回 (baseURL, proxy) 对应的共享 Transport，不存在则创建。
// F1 核心：用 http.DefaultTransport.Clone() 作为起点，保证所有字段都有合理默认值，
// 避免出现"只有 Proxy 字段被赋值、其余为零值"的残缺 Transport。
func (h *RequestHandler) getTransport(baseURL, proxy string) *http.Transport {
	key := baseURL + "|" + proxy
	h.mu.Lock()
	defer h.mu.Unlock()
	if t, ok := h.transports[key]; ok {
		return t
	}
	t := h.buildTransport(proxy)
	h.transports[key] = t
	return t
}

// buildTransport 统一构建 Transport，保证无论是否带代理都配置完整。
func (h *RequestHandler) buildTransport(proxy string) *http.Transport {
	// 以 DefaultTransport 为起点克隆，保留 ForceAttemptHTTP2、TLSClientConfig、
	// DialContext、TLSHandshakeTimeout、ExpectContinueTimeout 等全部合理默认值。
	t := http.DefaultTransport.(*http.Transport).Clone()

	// 调小空闲连接超时（默认 90s → 30s），降低取到已被上游关闭的"僵尸连接"概率。
	t.IdleConnTimeout = 30 * time.Second
	// 提高单 host 空闲连接数（默认 2 → 8），避免并发时频繁新建连接。
	t.MaxIdleConnsPerHost = 8

	if proxy != "" {
		if proxyURL, err := url.Parse(proxy); err == nil {
			t.Proxy = http.ProxyURL(proxyURL)
		}
		// proxy 解析失败时保持 Proxy 为 nil（等价于不使用代理），与旧行为一致
	}
	return t
}

// SendRequest 发送请求到目标服务器，同时返回性能指标
//
// F1: 通过共享 Transport 复用连接池；F2: 对连接级错误进行有限次重试。
// 函数签名保持不变，以兼容现有调用方与测试。
func (h *RequestHandler) SendRequest(apiKey, baseURL string, req *types.ChatRequest, timeout int,
	proxy string, headers map[string]string, extraFields map[string]interface{}) (*http.Response, *RequestMetrics, error) {

	// 构建请求体（调用 core 层纯函数）
	reqBody, err := core.BuildRequestBody(req, extraFields)
	if err != nil {
		return nil, nil, err
	}

	// F1: 复用共享 Transport，Client 按请求创建（轻量），Timeout 由 Client 控制
	transport := h.getTransport(baseURL, proxy)
	client := &http.Client{
		Timeout:   time.Duration(timeout) * time.Second,
		Transport: transport,
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

	// F2: 带重试的发送
	start := time.Now()
	resp, err := h.sendWithRetry(client, httpReq, reqBody, h.retryCount, metrics)
	metrics.RemoteLatency = time.Since(start)

	return resp, metrics, err
}

// sendWithRetry 在连接级错误时重试 POST 请求。
//
// 重试策略：
//   - 仅对"连接级错误"重试（EOF、connection reset、broken pipe、TLS 握手失败、dial 失败），
//     这些错误表明请求尚未被上游处理，重试是安全的。
//   - 一旦拿到 response header（err == nil），绝不重试（即使后续 body 读取失败），
//     因为 POST 非幂等，上游可能已开始处理。
//   - 每次重试前用原始 reqBody 重建 httpReq.Body（Body 会被消费，不可复用）。
//   - 简单线性退避：200ms * (attempt+1)。
func (h *RequestHandler) sendWithRetry(client *http.Client, httpReq *http.Request, reqBody []byte, retryCount int, metrics *RequestMetrics) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt <= retryCount; attempt++ {
		// 每次尝试（含首次）都要确保 Body 可读：首次由调用方已设置，
		// 但重试时 Body 已被消费，需用原始 reqBody 重建。
		if attempt > 0 {
			httpReq.Body = io.NopCloser(bytes.NewBuffer(reqBody))
			// 重置 ContentLength（http.Request.Do 会根据 Body 重新计算，但显式设置更安全）
			httpReq.ContentLength = int64(len(reqBody))
			metrics.Retries++
		}

		resp, err := client.Do(httpReq)
		if err == nil {
			return resp, nil
		}
		lastErr = err

		// 非连接级错误，不重试（调用 core 层纯函数）
		if !core.IsRetryableConnErr(err) {
			return nil, err
		}

		if attempt < retryCount {
			backoff := time.Duration(attempt+1) * 200 * time.Millisecond
			log.Printf("[Retry] connection-level error, attempt=%d/%d backoff=%v err=%v",
				attempt+1, retryCount, backoff, err)
			time.Sleep(backoff)
		}
	}
	return nil, lastErr
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

// BuildRequestBodyForTest 测试辅助方法：暴露 core.BuildRequestBody 功能
func (h *RequestHandler) BuildRequestBodyForTest(req *types.ChatRequest, extraFields map[string]interface{}) ([]byte, error) {
	return core.BuildRequestBody(req, extraFields)
}

// GetTransportForTest 测试辅助方法：暴露 getTransport 功能，用于验证 Transport 复用
func (h *RequestHandler) GetTransportForTest(baseURL, proxy string) *http.Transport {
	return h.getTransport(baseURL, proxy)
}

// TransportCountForTest 测试辅助方法：返回缓存中 Transport 的数量
func (h *RequestHandler) TransportCountForTest() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.transports)
}
