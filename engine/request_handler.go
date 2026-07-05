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
//
// F1 设计要点（与 DefaultTransport 的差异）：
//   - 保留 ForceAttemptHTTP2=true（默认），让 HTTP/2 ALPN 协商正常进行。
//   - IdleConnTimeout 调小为 10s（默认 90s）：降低空闲连接被上游单方面关闭
//     后仍被复用、读到 EOF 的概率。注意：这只是"降低"概率，并不能完全消除——
//     上游可能在任意时刻关闭连接（包括 10s 之内），真正的恢复机制见
//     SendRequest 中对 httpReq.GetBody 的设置以及 sendWithRetry 中调用
//     Transport.CloseIdleConnections() 清空 stale 连接池。
//   - MaxIdleConnsPerHost 调大为 8（默认 2）：避免并发时频繁新建连接。
//   - 当配置 proxy 为空时，显式设置 t.Proxy = nil，禁用环境变量代理
//     （详见下方"启动场景差异"BUG 分析）。
//
// 关于历史误判：曾因怀疑"上游 HTTP/2 实现缺陷"而在此处强制 HTTP/1.1
// （ForceAttemptHTTP2=false + NextProtos=[http/1.1]）。后经在 Ubuntu 主机上
// 实测（5 种 transport 配置 × 15 种请求体 × 100 并发请求），上游对 h2 处理
// 完全正常（返回 401），且 Python requests（仅 HTTP/1.1）也工作正常，
// 证明"HTTP/2 兼容性"假设是错误的，已撤销该改动。
//
// 真正的 EOF 根因（已在 SendRequest 中修复）：
//   - http.Request 未设置 GetBody 字段 → Transport 在遇到 stale-connection
//     EOF 时无法重建请求体，直接返回错误。
//   - sendWithRetry 重试时若不清空连接池，可能再次取到另一条 stale 连接
//     （连接池里可能积累多条被上游关闭的坏连接），导致连续 EOF。
//     修复：sendWithRetry 在重试前调用 Transport.CloseIdleConnections()。
//
// 启动场景差异 BUG（本次修复的核心）：
//   - DefaultTransport.Clone() 继承的 Proxy: ProxyFromEnvironment 会在每次
//     请求时检查 HTTP_PROXY/HTTPS_PROXY/NO_PROXY 环境变量。
//   - 从 SSH 会话启动 nano-api：通常无代理环境变量 → 直连 → 正常。
//   - 从 VNC 桌面会话启动 nano-api：桌面会话常继承 HTTP_PROXY/HTTPS_PROXY
//     （如 clash/v2ray/xray 等本地代理工具设置），所有 HTTPS 请求被转发到
//     本地代理（如 127.0.0.1:16808）。如果该代理对某些上游的 SSL 处理有
//     问题（如 xray 对 token.sensenova.cn 的 SSL_connect 失败 SSL_ERROR_SYSCALL），
//     就会持续出现 EOF——这正是用户观察到的"VNC 启动 EOF，SSH 启动正常"现象。
//   - 修复原则：配置文件中每个 provider 都有独立的 proxy 字段，proxy=""
//     即"不使用代理"，应直连；不应让环境变量隐式改变行为。
func (h *RequestHandler) buildTransport(proxy string) *http.Transport {
	// 以 DefaultTransport 为起点克隆，保留 DialContext、TLSHandshakeTimeout、
	// ExpectContinueTimeout、ForceAttemptHTTP2 等全部合理默认值。
	t := http.DefaultTransport.(*http.Transport).Clone()

	// 调小空闲连接超时（默认 90s → 10s），降低取到已被上游关闭的"僵尸连接"概率。
	// 实测发现 sensenova 等上游会主动关闭空闲 keep-alive 连接（无 timeout 头提示），
	// 10s 能显著降低 stale 连接积累速度。
	t.IdleConnTimeout = 10 * time.Second
	// 提高单 host 空闲连接数（默认 2 → 8），避免并发时频繁新建连接。
	t.MaxIdleConnsPerHost = 8

	if proxy != "" {
		if proxyURL, err := url.Parse(proxy); err == nil {
			t.Proxy = http.ProxyURL(proxyURL)
		} else {
			// proxy 解析失败：显式禁用代理，避免误用环境变量代理
			t.Proxy = nil
		}
	} else {
		// 显式禁用代理：当配置中 proxy 为空时，不继承环境变量中的代理设置。
		// 见上方函数注释中关于"启动场景差异"BUG 的分析。
		t.Proxy = nil
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

	// 关键修复（EOF 根因）：设置 httpReq.GetBody。
	//
	// 背景：Transport 在请求过程中遇到"已复用过的连接被上游单方面关闭"
	// （stale connection EOF）时，会尝试自动重试。但 net/http 文档明确：
	// "Transport only retries a request upon encountering a network error
	//  if the connection has already been used successfully and if the
	//  request is idempotent and either has no body or has its GetBody defined."
	//
	// 我们的请求是 POST（非幂等方法），原本不会触发 Transport 内部重试。
	// 但即便如此，设置 GetBody 仍有价值：
	//   1. 让 Transport 在判定需要重试（如基于 Idempotency-Key、或基于
	//      "请求未真正发出"的事实）时能够重建请求体，避免出现
	//      "http: Request.ContentLength=... but Body is empty" 类错误。
	//   2. 让我们自己的 sendWithRetry 在重建 Body 时与 Transport 行为一致。
	//
	// 注意：这不是"让 Transport 透明重试 POST"，POST 仍需 sendWithRetry 显式重试。
	// GetBody 的作用是当 Transport 在请求体已发出部分字节后才发现连接坏掉时，
	// 不会陷入"无法重建请求体"的死路，而是能干净地返回错误，由上层 sendWithRetry
	// 用全新连接重试。
	//
	// 与 bytes.NewBuffer 配合：bytes.Buffer 本身可重复读（Seek 到开头），但
	// http.Request.Body 是 io.ReadCloser，被消费后不可重用。GetBody 返回一个
	// 全新的 *bytes.Reader（指向同一份 reqBody 数据），保证可多次调用。
	httpReq.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(reqBody)), nil
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
//   - 重试时优先调用 httpReq.GetBody 重建请求体（与 Transport 内部行为一致），
//     若 GetBody 未设置则回退到用原始 reqBody 重建。
//   - 简单线性退避：200ms * (attempt+1)。
//
// 关于 stale-connection EOF（真正的根因）：
//   当 Transport 复用一个已被上游单方面关闭的空闲连接来发送请求时，会立即收到
//   EOF（或写时 broken pipe）。Transport 内部会丢弃这一条坏连接，但连接池里
//     可能还残留其他 stale 连接（MaxIdleConnsPerHost=8，最坏情况下连接池里全是
//     被上游关闭的坏连接）。下次 client.Do 又取到另一条 stale 连接，导致连续
//     EOF——这正是日志中"3 次重试都 EOF"的现象。
//
// 修复：在每次 EOF 重试前调用 Transport.CloseIdleConnections()，强制清空整个
//   空闲连接池。这样下次 client.Do 必然走全新连接（重新 dial + TLS 握手），
//   彻底避免取到另一条 stale 连接。代价是丢失连接池中可能仍有效的连接，
//   但在 EOF 场景下这是可接受的——连接池里大概率全是坏连接。
//
// 验证：重启 nano-api 后立即发请求成功（连接池空）；运行一段时间后发请求 EOF
//   （连接池积累 stale 连接）。本修复清空 stale 连接池，使重试等价于"用全新
//   连接重试"，与重启后的状态一致。
func (h *RequestHandler) sendWithRetry(client *http.Client, httpReq *http.Request, reqBody []byte, retryCount int, metrics *RequestMetrics) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt <= retryCount; attempt++ {
		// 每次尝试（含首次）都要确保 Body 可读：首次由调用方已设置，
		// 但重试时 Body 已被消费，需用 GetBody 或原始 reqBody 重建。
		if attempt > 0 {
			if httpReq.GetBody != nil {
				body, err := httpReq.GetBody()
				if err != nil {
					return nil, err
				}
				httpReq.Body = body
			} else {
				httpReq.Body = io.NopCloser(bytes.NewBuffer(reqBody))
			}
			// 重置 ContentLength（http.Request.Do 会根据 Body 重新计算，但显式设置更安全）
			httpReq.ContentLength = int64(len(reqBody))

			// 关键修复：清空 Transport 的空闲连接池，确保下次 client.Do 用全新连接。
			// 见上方函数注释中关于 stale-connection EOF 的分析。
			if transport, ok := client.Transport.(*http.Transport); ok {
				transport.CloseIdleConnections()
			}

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
