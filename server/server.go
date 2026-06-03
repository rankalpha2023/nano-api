package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"

	"nano-api/engine"
	"nano-api/types"
)

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

	// 启动服务器
	serverAddr := fmt.Sprintf(":%d", port)
	log.Printf("Starting HTTP server on %s...", serverAddr)
	err := http.ListenAndServe(serverAddr, nil)
	log.Printf("HTTP server exited with error: %v", err)
	return err
}

// handleChatCompletions 处理聊天完成请求
func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	// 确保日志立即输出
	//log.SetFlags(log.LstdFlags | log.Lshortfile)
	//log.Println("=== START handleChatCompletions ===")

	// 检查请求方法
	if r.Method == "OPTIONS" {
		log.Println("Handling OPTIONS request (CORS preflight)")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != "POST" {
		log.Printf("Error: Invalid request method: %s", r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 检查Content-Type
	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		log.Println("Error: Missing Content-Type header")
		http.Error(w, "Missing Content-Type header", http.StatusBadRequest)
		return
	}

	// 读取请求体
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("Error reading request body: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	log.Printf("Request body length: %d bytes", len(body))
	if len(body) == 0 {
		log.Println("Error: Empty request body")
		http.Error(w, "Empty request body", http.StatusBadRequest)
		return
	}

	// 重新创建请求体读取器
	r.Body = io.NopCloser(bytes.NewBuffer(body))

	// 解析请求
	var req types.ChatRequest
	if err := json.Unmarshal(body, &req); err != nil {
		log.Printf("Error parsing request: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	log.Printf("Request received: model=%s, stream=%v, messages=%d, temperature=%.2f, top_p=%.2f, max_tokens=%d",
		req.Model, req.Stream, len(req.Messages), req.Temperature, req.TopP, req.MaxTokens)

	log.Println("Getting account from account manager...")
	requestHandler, account, realModel, err := s.accountManager.SendRequest(&req)
	if err != nil {
		log.Printf("Error getting account: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if account == nil {
		log.Println("No available accounts")
		http.Error(w, "No available accounts", http.StatusServiceUnavailable)
		return
	}

	modelConfig := s.accountManager.GetModelConfig(account.ProviderName)
	if req.Temperature == 0 {
		req.Temperature = modelConfig.DefaultTemperature
		log.Printf("Using default temperature from %s: %.2f", account.ProviderName, modelConfig.DefaultTemperature)
	}
	if req.TopP == 0 {
		req.TopP = modelConfig.DefaultTopP
		log.Printf("Using default top_p from %s: %.2f", account.ProviderName, modelConfig.DefaultTopP)
	}
	if req.MaxTokens == 0 {
		req.MaxTokens = modelConfig.DefaultMaxTokens
		log.Printf("Using default max_tokens from %s: %d", account.ProviderName, modelConfig.DefaultMaxTokens)
	}

	// 如果模型名被映射了，更新请求
	if realModel != "" && realModel != req.Model {
		log.Printf("Updating request model: %s -> %s", req.Model, realModel)
		req.Model = realModel
	}

	log.Printf("Using account: %s, BaseURL: %s, Timeout: %ds, Proxy: %s", account.ProviderName, account.BaseURL, account.Timeout, account.Proxy)

	// 发送请求到服务器
	log.Printf("Sending request to %s server...", account.ProviderName)

	// 设置默认超时时间
	timeout := account.Timeout
	if timeout == 0 {
		timeout = 30
	}

	resp, err := requestHandler.SendRequest(account.APIKey, account.BaseURL, &req, timeout, account.Proxy, account.Headers, account.ExtraFields)
	if err != nil {
		log.Printf("Error sending request to %s: %v", account.ProviderName, err)
		// 标记帐号失败
		s.accountManager.MarkAccountFailed(account)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("Received response from %s: status=%d", account.ProviderName, resp.StatusCode)

	// 标记帐号可用
	s.accountManager.MarkAccountAvailable(account)

	// 复制响应头
	log.Println("Copying response headers...")
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// 对于流式响应，设置适当的响应头
	if req.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Content-Type-Options", "nosniff")
	}

	w.WriteHeader(resp.StatusCode)

	// 处理流式响应
	if req.Stream {
		log.Println("Handling streaming response")
		// 直接转发流式响应
		buf := make([]byte, 1024)
		for {
			n, err := resp.Body.Read(buf)
			if n > 0 {
				w.Write(buf[:n])
				if flusher, ok := w.(http.Flusher); ok {
					flusher.Flush()
				}
			}
			if err != nil {
				break
			}
		}
	} else {
		log.Println("Handling non-streaming response")
		// 处理非流式响应
		buf := make([]byte, 1024)
		for {
			n, err := resp.Body.Read(buf)
			if n > 0 {
				w.Write(buf[:n])
			}
			if err != nil {
				break
			}
		}
	}

	resp.Body.Close()
	log.Println("Request processed successfully")
}
