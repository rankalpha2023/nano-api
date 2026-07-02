package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"time"

	"nano-api/core"
	"nano-api/engine"
	"nano-api/types"
)

// generateTraceID 生成请求级别的唯一追踪 ID。
// 格式：req-MMDD-HHMMSS-XXXXXXXX，XXXXXXXX 为 8 位十六进制随机数。
// 用于在并发请求的交错日志中还原单个请求的完整链路（F4 修复）。
func generateTraceID() string {
	return fmt.Sprintf("req-%s-%08x",
		time.Now().Format("0102-150405"),
		rand.Uint32())
}

// handleListModels 处理 /v1/models 请求
func (s *Server) handleListModels(w http.ResponseWriter, r *http.Request) {
	// OPTIONS 预检请求
	if r.Method == "OPTIONS" {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != "GET" {
		log.Printf("Error: Invalid request method for /v1/models: %s", r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	models := s.accountManager.GetAllModels()
	resp := types.ModelListResponse{
		Object: "list",
		Data:   models,
	}

	body, err := json.Marshal(resp)
	if err != nil {
		log.Printf("Error marshaling model list: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Write(body)
	log.Printf("GET /v1/models returned %d models", len(models))
}

// Server HTTP服务器
type Server struct {
	accountManager *engine.AccountManager
}

func NewServer(accountManager *engine.AccountManager) *Server {
	return &Server{
		accountManager: accountManager,
	}
}

// Start 启动服务器
func (s *Server) Start(port int) error {
	// 注册路由
	log.Println("Registering routes...")
	http.HandleFunc("/v1/chat/completions", s.handleChatCompletions)
	http.HandleFunc("/v1/models", s.handleListModels)
	http.HandleFunc("/v1/messages", s.handleAnthropicMessages)

	// 启动服务器
	serverAddr := fmt.Sprintf(":%d", port)
	log.Printf("Starting HTTP server on %s...", serverAddr)
	err := http.ListenAndServe(serverAddr, nil)
	log.Printf("HTTP server exited with error: %v", err)
	return err
}

// handleChatCompletions 处理聊天完成请求。
//
// UI 层职责（规约：禁止超过 3 行业务逻辑计算）：
//   - HTTP 方法/CORS 检查、请求体读取与 JSON 解析（HTTP I/O）
//   - 调用 Engine 层 ProcessChatCompletion 编排业务流程（1 行）
//   - 复制响应头、转发响应体（流式/非流式）（HTTP I/O）
//   - 成功后调用 MarkAccountAvailable（1 行副作用调度）
//
// 业务逻辑（账户选择、限流、默认值填充、模型名映射、上游请求、失败标记）
// 全部下沉至 engine.AccountManager.ProcessChatCompletion。
func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	totalStart := time.Now()
	traceID := generateTraceID()

	// HTTP 方法/CORS 检查
	if r.Method == "OPTIONS" {
		log.Printf("[%s] Handling OPTIONS request (CORS preflight)", traceID)
		s.setCORSHeaders(w, "POST, GET, OPTIONS")
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.Method != "POST" {
		log.Printf("[%s] Error: Invalid request method: %s", traceID, r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 读取并解析请求体（HTTP I/O）
	req, err := s.readChatRequest(w, r, traceID)
	if err != nil {
		return
	}

	log.Printf("[%s] Request received: model=%s, stream=%v, messages=%d, temperature=%.2f, top_p=%.2f, max_tokens=%d",
		traceID, req.Model, req.Stream, len(req.Messages), req.Temperature, req.TopP, req.MaxTokens)

	// 调用 Engine 层编排（账户选择 → 限流 → 默认值 → 上游请求 → 失败标记）
	result, err := s.accountManager.ProcessChatCompletion(req)
	if err != nil {
		log.Printf("[%s] Error processing request: %v", traceID, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if result == nil {
		log.Printf("[%s] No available accounts", traceID)
		http.Error(w, "No available accounts", http.StatusServiceUnavailable)
		return
	}

	log.Printf("[%s] Using account: %s, BaseURL: %s, Timeout: %ds, Proxy: %s",
		traceID, result.Account.ProviderName, result.Account.BaseURL, result.Account.Timeout, result.Account.Proxy)
	log.Printf("[%s] Received response from %s: status=%d remote=%v retries=%d",
		traceID, result.Account.ProviderName, result.Response.StatusCode,
		result.Metrics.RemoteLatency, result.Metrics.Retries)

	// 转发上游响应（HTTP I/O），并在成功后标记账户可用
	s.forwardUpstreamResponse(w, result.Response, req.Stream, traceID, result.Account, result.AuthFailed,
		result.Metrics, result.RateLimitWait, req, totalStart)
}

// handleAnthropicMessages 处理 Anthropic Messages API 请求。
//
// UI 层职责（规约：禁止超过 3 行业务逻辑计算）：
//   - HTTP 方法/CORS 检查、请求体读取与 JSON 解析（HTTP I/O）
//   - 调用 core.AnthropicToOpenAI 转换请求格式（1 行调用纯函数）
//   - 调用 Engine 层 ProcessChatCompletion 编排业务流程（1 行）
//   - 调用 core.OpenAIToAnthropicResponse 转换响应格式（1 行调用纯函数）
//   - 复制响应头、转发响应体（HTTP I/O）
//   - 成功后调用 MarkAccountAvailable（1 行副作用调度）
//
// 模型名映射（如 claude-sonnet-4-6 → deepseek-v4-pro）由 Engine 层基于
// 配置（ProviderConfig.Models）在 ProcessChatCompletion 中处理，UI 层不参与。
func (s *Server) handleAnthropicMessages(w http.ResponseWriter, r *http.Request) {
	totalStart := time.Now()

	// HTTP 方法/CORS 检查
	if r.Method == "OPTIONS" {
		s.setCORSHeaders(w, "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, x-api-key, anthropic-version")
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 读取请求体（HTTP I/O）
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("[Anthropic] Error reading body: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var ar types.AnthropicMessagesRequest
	if err := json.Unmarshal(body, &ar); err != nil {
		log.Printf("[Anthropic] Error parsing request: %v", err)
		http.Error(w, "Invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}

	// 模型名映射由 Engine 层基于配置（ProviderConfig.Models）处理：
	// ProcessChatCompletion → SendRequest → GetRealModelName 会根据配置把
	// 客户端传入的模型名（如 claude-sonnet-4-6）映射到内部真实模型名。
	// UI 层只需保留原始模型名，用于在响应中填回（Anthropic 客户端期望响应
	// 的 model 字段与请求一致）。
	originalModel := ar.Model

	log.Printf("[Anthropic] Request: model=%s, max_tokens=%d, msgs=%d, stream=%v",
		ar.Model, ar.MaxTokens, len(ar.Messages), ar.Stream)

	// 转换为内部 ChatRequest（调用 core 层纯函数）
	chatReq := core.AnthropicToOpenAI(&ar)

	// 调用 Engine 层编排
	result, err := s.accountManager.ProcessChatCompletion(chatReq)
	if err != nil {
		log.Printf("[Anthropic] Error processing request: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if result == nil {
		log.Println("[Anthropic] No available accounts")
		http.Error(w, "No available accounts", http.StatusServiceUnavailable)
		return
	}
	defer result.Response.Body.Close()

	log.Printf("[Anthropic] Using provider=%s baseURL=%s model=%s",
		result.Account.ProviderName, result.Account.BaseURL, chatReq.Model)

	// 读取上游响应并转换为 Anthropic 格式
	respBody, err := io.ReadAll(result.Response.Body)
	if err != nil {
		log.Printf("[Anthropic] Error reading upstream response: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	anthropicResp, err := core.OpenAIToAnthropicResponse(respBody, originalModel)
	if err != nil {
		log.Printf("[Anthropic] Error converting to Anthropic format: %v", err)
		// 转换失败时原始返回 OpenAI 格式
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(result.Response.StatusCode)
		w.Write(respBody)
		return
	}

	// F3: 非 401/403 且 body 读取成功 → 标记账户可用
	if !result.AuthFailed {
		s.accountManager.MarkAccountAvailable(result.Account)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	respJSON, _ := json.Marshal(anthropicResp)
	w.Write(respJSON)

	totalElapsed := time.Since(totalStart)
	log.Printf("[Anthropic] Done: model=%s provider=%s latency=%v remote=%v rate_limit_wait=%v",
		originalModel, result.Account.ProviderName, totalElapsed,
		result.Metrics.RemoteLatency, result.RateLimitWait)
}

// setCORSHeaders 设置通用 CORS 响应头
func (s *Server) setCORSHeaders(w http.ResponseWriter, methods string) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", methods)
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
}

// readChatRequest 读取并解析 ChatRequest 请求体（HTTP I/O，UI 层职责）
func (s *Server) readChatRequest(w http.ResponseWriter, r *http.Request, traceID string) (*types.ChatRequest, error) {
	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		log.Printf("[%s] Error: Missing Content-Type header", traceID)
		http.Error(w, "Missing Content-Type header", http.StatusBadRequest)
		return nil, fmt.Errorf("missing content-type")
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("[%s] Error reading request body: %v", traceID, err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return nil, err
	}
	log.Printf("[%s] Request body length: %d bytes", traceID, len(body))
	if len(body) == 0 {
		log.Printf("[%s] Error: Empty request body", traceID)
		http.Error(w, "Empty request body", http.StatusBadRequest)
		return nil, fmt.Errorf("empty body")
	}

	r.Body = io.NopCloser(bytes.NewBuffer(body))
	defer r.Body.Close()

	var req types.ChatRequest
	if err := json.Unmarshal(body, &req); err != nil {
		log.Printf("[%s] Error parsing request: %v", traceID, err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return nil, err
	}
	return &req, nil
}

// forwardUpstreamResponse 转发上游响应到客户端（流式/非流式分支），并在成功后标记账户可用。
//
// 这是纯 HTTP I/O 操作（UI 层职责），不包含业务逻辑计算。
func (s *Server) forwardUpstreamResponse(
	w http.ResponseWriter,
	resp *http.Response,
	isStream bool,
	traceID string,
	account *types.Account,
	authFailed bool,
	metrics *engine.RequestMetrics,
	rateLimitWait time.Duration,
	req *types.ChatRequest,
	totalStart time.Time,
) {
	// 复制响应头
	log.Printf("[%s] Copying response headers...", traceID)
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// F4: 在响应头中返回 traceID
	w.Header().Set("X-Trace-ID", traceID)

	// 流式响应特殊头
	if isStream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Content-Type-Options", "nosniff")
	}

	w.WriteHeader(resp.StatusCode)

	// 转发响应体（流式/非流式统一逻辑：循环读取并写入）
	var forwardedBytes int64
	buf := make([]byte, 1024)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			w.Write(buf[:n])
			forwardedBytes += int64(n)
			if isStream {
				if flusher, ok := w.(http.Flusher); ok {
					flusher.Flush()
				}
			}
		}
		if err != nil {
			if err != io.EOF {
				streamLabel := "non-stream"
				if isStream {
					streamLabel = "stream"
				}
				log.Printf("[%s] Error reading %s body: %v", traceID, streamLabel, err)
			}
			break
		}
	}
	resp.Body.Close()

	// F3: 非 401/403 且 body 转发完成 → 标记账户可用
	if !authFailed {
		s.accountManager.MarkAccountAvailable(account)
	}

	streamLabel := "false"
	if isStream {
		streamLabel = "true"
		log.Printf("[%s] Handling streaming response", traceID)
	} else {
		log.Printf("[%s] Handling non-streaming response", traceID)
	}

	totalElapsed := time.Since(totalStart)
	log.Printf("[%s][Metrics] provider=%s model=%s stream=%s req_bytes=%d resp_bytes=%d rate_limit_wait=%v remote=%v retries=%d total=%v",
		traceID, account.ProviderName, req.Model, streamLabel,
		metrics.RequestBytes, forwardedBytes,
		rateLimitWait, metrics.RemoteLatency, metrics.Retries, totalElapsed)
	log.Printf("[%s] Request processed successfully", traceID)
}
