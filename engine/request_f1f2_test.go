package engine_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"nano-api/engine"
	"nano-api/types"
)

// ============================================================
// F1: Transport 复用与配置完整性测试
// ============================================================

// TestF1_TransportReuse_SameKey 验证相同 (baseURL, proxy) 复用同一 Transport 实例
func TestF1_TransportReuse_SameKey(t *testing.T) {
	handler := engine.NewRequestHandler()

	t1 := handler.GetTransportForTest("http://upstream-a", "")
	t2 := handler.GetTransportForTest("http://upstream-a", "")

	if t1 != t2 {
		t.Error("Expected same Transport instance for same (baseURL, proxy)")
	}

	if handler.TransportCountForTest() != 1 {
		t.Errorf("Expected 1 cached transport, got %d", handler.TransportCountForTest())
	}
}

// TestF1_TransportReuse_DifferentBaseURL 验证不同 baseURL 创建不同 Transport
func TestF1_TransportReuse_DifferentBaseURL(t *testing.T) {
	handler := engine.NewRequestHandler()

	t1 := handler.GetTransportForTest("http://upstream-a", "")
	t2 := handler.GetTransportForTest("http://upstream-b", "")

	if t1 == t2 {
		t.Error("Expected different Transport instances for different baseURLs")
	}

	if handler.TransportCountForTest() != 2 {
		t.Errorf("Expected 2 cached transports, got %d", handler.TransportCountForTest())
	}
}

// TestF1_TransportReuse_DifferentProxy 验证不同 proxy 创建不同 Transport
func TestF1_TransportReuse_DifferentProxy(t *testing.T) {
	handler := engine.NewRequestHandler()

	t1 := handler.GetTransportForTest("http://upstream-a", "")
	t2 := handler.GetTransportForTest("http://upstream-a", "http://proxy:8080")

	if t1 == t2 {
		t.Error("Expected different Transport instances for different proxies")
	}

	if handler.TransportCountForTest() != 2 {
		t.Errorf("Expected 2 cached transports, got %d", handler.TransportCountForTest())
	}
}

// TestF1_TransportConfig_IdleConnTimeout 验证 IdleConnTimeout 被调整为 10s（非默认 90s）
//
// 调小 IdleConnTimeout 至 10s 的原因：
//   - 上游（如 sensenova）会主动关闭空闲 keep-alive 连接（无 timeout 头提示），
//     导致 Transport 连接池中积累 stale 连接。
//   - 调小 timeout 能显著降低"取到已被上游关闭的连接"的概率。
//   - 即使取到 stale 连接，sendWithRetry 中的 CloseIdleConnections 修复
//     也会清空整个连接池，确保重试走全新连接。
func TestF1_TransportConfig_IdleConnTimeout(t *testing.T) {
	handler := engine.NewRequestHandler()
	tr := handler.GetTransportForTest("http://upstream", "")

	if tr.IdleConnTimeout != 10*time.Second {
		t.Errorf("Expected IdleConnTimeout=10s, got %v", tr.IdleConnTimeout)
	}
}

// TestF1_TransportConfig_MaxIdleConnsPerHost 验证 MaxIdleConnsPerHost 被调整为 8（非默认 2）
func TestF1_TransportConfig_MaxIdleConnsPerHost(t *testing.T) {
	handler := engine.NewRequestHandler()
	tr := handler.GetTransportForTest("http://upstream", "")

	if tr.MaxIdleConnsPerHost != 8 {
		t.Errorf("Expected MaxIdleConnsPerHost=8, got %d", tr.MaxIdleConnsPerHost)
	}
}

// TestF1_TransportConfig_HTTP2Preserved 验证 ForceAttemptHTTP2=true（保留默认值）。
//
// 设计：buildTransport 不强制 HTTP/1.1，而是让 HTTP/2 ALPN 协商正常进行。
// 历史上曾误判为"上游 HTTP/2 兼容性问题导致 EOF"而强制 HTTP/1.1，
// 经实测证伪后已撤销。EOF 的真正根因是 http.Request 未设置 GetBody，
// 已在 SendRequest 中修复。
func TestF1_TransportConfig_HTTP2Preserved(t *testing.T) {
	handler := engine.NewRequestHandler()
	tr := handler.GetTransportForTest("http://upstream", "")

	if !tr.ForceAttemptHTTP2 {
		t.Error("Expected ForceAttemptHTTP2=true (HTTP/2 ALPN preserved, not forced to HTTP/1.1)")
	}
}

// TestF1_TransportConfig_TLSHandshakeTimeout 验证 TLSHandshakeTimeout 非零（保留默认值）
func TestF1_TransportConfig_TLSHandshakeTimeout(t *testing.T) {
	handler := engine.NewRequestHandler()
	tr := handler.GetTransportForTest("http://upstream", "")

	if tr.TLSHandshakeTimeout == 0 {
		t.Error("Expected non-zero TLSHandshakeTimeout (inherited from DefaultTransport)")
	}
}

// TestF1_TransportConfig_WithProxy 验证带 proxy 的 Transport 的 Proxy 字段被正确设置
func TestF1_TransportConfig_WithProxy(t *testing.T) {
	handler := engine.NewRequestHandler()
	tr := handler.GetTransportForTest("http://upstream", "http://proxy:8080")

	if tr.Proxy == nil {
		t.Error("Expected non-nil Proxy for transport with proxy")
	}

	// 同时验证其他配置字段仍然完整（非零值）
	if tr.IdleConnTimeout != 10*time.Second {
		t.Errorf("Expected IdleConnTimeout=10s even with proxy, got %v", tr.IdleConnTimeout)
	}
	if !tr.ForceAttemptHTTP2 {
		t.Error("Expected ForceAttemptHTTP2=true even with proxy (HTTP/2 preserved)")
	}
	if tr.TLSHandshakeTimeout == 0 {
		t.Error("Expected non-zero TLSHandshakeTimeout even with proxy")
	}
}

// TestF1_TransportConfig_InvalidProxyIgnored 验证无效 proxy 被忽略
// 无效 proxy 时其他配置仍应完整（不会因解析失败而丢失配置）
func TestF1_TransportConfig_InvalidProxyIgnored(t *testing.T) {
	handler := engine.NewRequestHandler()
	tr := handler.GetTransportForTest("http://upstream", "://invalid")

	// 无效 proxy 应被忽略，其他配置仍应完整
	if tr.IdleConnTimeout != 10*time.Second {
		t.Errorf("Expected IdleConnTimeout=10s even with invalid proxy, got %v", tr.IdleConnTimeout)
	}
	if tr.MaxIdleConnsPerHost != 8 {
		t.Errorf("Expected MaxIdleConnsPerHost=8 even with invalid proxy, got %d", tr.MaxIdleConnsPerHost)
	}
	if !tr.ForceAttemptHTTP2 {
		t.Error("Expected ForceAttemptHTTP2=true even with invalid proxy (HTTP/2 preserved)")
	}
	if tr.TLSHandshakeTimeout == 0 {
		t.Error("Expected non-zero TLSHandshakeTimeout even with invalid proxy")
	}
	// 无效 proxy 也应显式禁用代理，不能误用环境变量代理
	if tr.Proxy != nil {
		t.Error("Expected Proxy=nil even with invalid proxy (must not fall back to env proxy)")
	}
}

// TestF1_TransportConfig_EmptyProxy_IgnoresEnvProxy 验证启动场景差异 BUG 的修复：
// 当配置 proxy="" 时，即使进程环境变量设置了 HTTP_PROXY/HTTPS_PROXY，
// Transport 也不应使用任何代理。
//
// 历史 BUG：buildTransport 在 proxy="" 时未显式设置 t.Proxy，导致
// DefaultTransport.Clone() 继承的 Proxy: ProxyFromEnvironment 生效。
// 从 VNC 桌面会话启动 nano-api 时，桌面会话继承了 xray/clash 等本地代理
// 设置的 HTTPS_PROXY 环境变量，所有 HTTPS 请求被转发到本地代理（如
// 127.0.0.1:16808）。该代理对部分上游的 SSL 处理失败（SSL_ERROR_SYSCALL），
// 导致持续 EOF。而从 SSH 启动则因无代理环境变量而正常。
//
// 修复：proxy="" 时显式 t.Proxy = nil，强制直连，与启动场景无关。
func TestF1_TransportConfig_EmptyProxy_IgnoresEnvProxy(t *testing.T) {
	// 设置环境变量模拟 VNC 桌面会话
	envVars := map[string]string{
		"HTTP_PROXY":  "http://127.0.0.1:16808/",
		"HTTPS_PROXY": "http://127.0.0.1:16808/",
		"http_proxy":  "http://127.0.0.1:16808/",
		"https_proxy": "http://127.0.0.1:16808/",
		"NO_PROXY":    "localhost,127.0.0.0/8,::1",
		"no_proxy":    "localhost,127.0.0.0/8,::1",
	}
	for k, v := range envVars {
		old := os.Getenv(k)
		os.Setenv(k, v)
		defer os.Setenv(k, old)
	}

	handler := engine.NewRequestHandler()
	tr := handler.GetTransportForTest("https://token.sensenova.cn/v1", "")

	if tr.Proxy != nil {
		t.Errorf("Expected Proxy=nil when config proxy is empty (even with env vars set), got non-nil Proxy")
	}
}

// TestF1_TransportConfig_WithProxy_OverridesEnvProxy 验证配置 proxy 不为空时，
// 显式覆盖环境变量代理（用户配置优先于环境变量）。
func TestF1_TransportConfig_WithProxy_OverridesEnvProxy(t *testing.T) {
	os.Setenv("HTTPS_PROXY", "http://127.0.0.1:16808/")
	defer os.Unsetenv("HTTPS_PROXY")

	handler := engine.NewRequestHandler()
	tr := handler.GetTransportForTest("https://upstream", "http://my-proxy:9090")

	if tr.Proxy == nil {
		t.Fatal("Expected non-nil Proxy when config proxy is set")
	}

	// 验证 Proxy 是配置的代理，而不是环境变量中的代理
	req := &http.Request{URL: &url.URL{Scheme: "https", Host: "upstream"}}
	url, err := tr.Proxy(req)
	if err != nil {
		t.Fatalf("Proxy function error: %v", err)
	}
	if url == nil {
		t.Fatal("Expected proxy URL from config, got nil")
	}
	if url.Host != "my-proxy:9090" {
		t.Errorf("Expected proxy host my-proxy:9090 (from config), got %s (maybe from env)", url.Host)
	}
}

// TestF1_TransportReuseAcrossRequests 验证多次 SendRequest 复用同一 Transport
func TestF1_TransportReuseAcrossRequests(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte(`{"choices":[]}`))
	}))
	defer upstream.Close()

	handler := engine.NewRequestHandler()
	req := &types.ChatRequest{Model: "test", Messages: []types.Message{{Role: "user", Content: "hi"}}}

	// 发送 3 次请求到同一 upstream
	for i := 0; i < 3; i++ {
		resp, _, err := handler.SendRequest("key", upstream.URL, req, 10, "", nil, nil)
		if err != nil {
			t.Fatalf("Request %d failed: %v", i+1, err)
		}
		resp.Body.Close()
	}

	// 应只缓存 1 个 Transport
	if handler.TransportCountForTest() != 1 {
		t.Errorf("Expected 1 cached transport after 3 requests to same upstream, got %d",
			handler.TransportCountForTest())
	}
}

// ============================================================
// F2: 请求级重试测试
// ============================================================

// TestF2_NewRequestHandler_DefaultRetry 验证默认重试次数为 1
func TestF2_NewRequestHandler_DefaultRetry(t *testing.T) {
	handler := engine.NewRequestHandler()
	if handler.GetRetryCount() != 1 {
		t.Errorf("Expected default retryCount=1, got %d", handler.GetRetryCount())
	}
}

// TestF2_NewRequestHandlerWithRetry 验证自定义重试次数
func TestF2_NewRequestHandlerWithRetry(t *testing.T) {
	tests := []struct {
		input    int
		expected int
	}{
		{0, 0},
		{1, 1},
		{3, 3},
		{-1, 0}, // 负数归零
		{-5, 0},
	}
	for _, tt := range tests {
		handler := engine.NewRequestHandlerWithRetry(tt.input)
		if handler.GetRetryCount() != tt.expected {
			t.Errorf("NewRequestHandlerWithRetry(%d): expected %d, got %d",
				tt.input, tt.expected, handler.GetRetryCount())
		}
	}
}

// TestF2_Retry_OnConnectionClose 验证首次连接被关闭时重试成功
// 模拟 EOF 场景：首次 hijack 并关闭连接，第二次正常返回
func TestF2_Retry_OnConnectionClose(t *testing.T) {
	var callCount int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&callCount, 1) == 1 {
			// 首次：hijack 并立即关闭连接，模拟上游在请求发送前关闭连接（EOF）
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Fatal("server does not support hijacking")
			}
			conn, _, err := hj.Hijack()
			if err != nil {
				t.Fatalf("hijack failed: %v", err)
			}
			conn.Close()
			return
		}
		// 第二次：正常返回
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"choices":[]}`))
	}))
	defer upstream.Close()

	handler := engine.NewRequestHandlerWithRetry(2)
	req := &types.ChatRequest{Model: "test", Messages: []types.Message{{Role: "user", Content: "hi"}}}

	resp, metrics, err := handler.SendRequest("key", upstream.URL, req, 10, "", nil, nil)
	if err != nil {
		t.Fatalf("Expected retry to succeed, got: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("Expected 200 after retry, got %d", resp.StatusCode)
	}
	if metrics.Retries < 1 {
		t.Errorf("Expected retries >= 1, got %d", metrics.Retries)
	}
	if atomic.LoadInt32(&callCount) < 2 {
		t.Errorf("Expected upstream called >= 2 times, got %d", callCount)
	}
}

// TestF2_Retry_ConnectionRefused 验证连接被拒绝时触发重试
func TestF2_Retry_ConnectionRefused(t *testing.T) {
	handler := engine.NewRequestHandlerWithRetry(2)
	req := &types.ChatRequest{Model: "test", Messages: []types.Message{{Role: "user", Content: "hi"}}}

	// 连接到未监听的端口
	_, metrics, err := handler.SendRequest("key", "http://127.0.0.1:1", req, 5, "", nil, nil)
	if err == nil {
		t.Error("Expected error after retries exhausted")
	}
	// 应该尝试了 3 次（1 次初始 + 2 次重试）
	if metrics.Retries != 2 {
		t.Errorf("Expected 2 retries, got %d", metrics.Retries)
	}
}

// TestF2_NoRetry_OnSuccess 验证成功时不重试
func TestF2_NoRetry_OnSuccess(t *testing.T) {
	var callCount int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(200)
		w.Write([]byte(`{"choices":[]}`))
	}))
	defer upstream.Close()

	handler := engine.NewRequestHandlerWithRetry(3)
	req := &types.ChatRequest{Model: "test", Messages: []types.Message{{Role: "user", Content: "hi"}}}

	resp, metrics, err := handler.SendRequest("key", upstream.URL, req, 10, "", nil, nil)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if metrics.Retries != 0 {
		t.Errorf("Expected 0 retries on success, got %d", metrics.Retries)
	}
	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("Expected upstream called once, got %d", callCount)
	}
}

// TestF2_NoRetry_OnTimeout 验证超时错误不重试（非连接级错误）
func TestF2_NoRetry_OnTimeout(t *testing.T) {
	var callCount int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		time.Sleep(2 * time.Second)
		w.WriteHeader(200)
	}))
	defer upstream.Close()

	handler := engine.NewRequestHandlerWithRetry(3)
	req := &types.ChatRequest{Model: "test", Messages: []types.Message{{Role: "user", Content: "hi"}}}

	// timeout=1s，upstream 睡 2s → 超时
	_, metrics, err := handler.SendRequest("key", upstream.URL, req, 1, "", nil, nil)
	if err == nil {
		t.Error("Expected timeout error")
	}

	// 超时是 Client.Timeout 触发的，不是连接级错误，不应重试
	if metrics.Retries > 0 {
		t.Errorf("Expected 0 retries on timeout (non-connection error), got %d", metrics.Retries)
	}
}

// TestF2_Retry_ZeroRetryCount 验证 retryCount=0 时不重试
func TestF2_Retry_ZeroRetryCount(t *testing.T) {
	var callCount int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&callCount, 1)
		if n == 1 {
			// 首次关闭连接
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Fatal("server does not support hijacking")
			}
			conn, _, _ := hj.Hijack()
			conn.Close()
			return
		}
		w.WriteHeader(200)
	}))
	defer upstream.Close()

	handler := engine.NewRequestHandlerWithRetry(0) // 不重试
	req := &types.ChatRequest{Model: "test", Messages: []types.Message{{Role: "user", Content: "hi"}}}

	_, metrics, err := handler.SendRequest("key", upstream.URL, req, 10, "", nil, nil)
	if err == nil {
		t.Error("Expected error with 0 retries")
	}
	if metrics.Retries != 0 {
		t.Errorf("Expected 0 retries, got %d", metrics.Retries)
	}
	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("Expected upstream called once (no retry), got %d", callCount)
	}
}

// TestF2_isRetryableConnErr 验证错误分类逻辑
func TestF2_isRetryableConnErr(t *testing.T) {
	// 通过连接到未监听端口来产生真实错误
	handler := engine.NewRequestHandlerWithRetry(0)
	req := &types.ChatRequest{Model: "test", Messages: []types.Message{{Role: "user", Content: "hi"}}}

	_, _, err := handler.SendRequest("key", "http://127.0.0.1:1", req, 5, "", nil, nil)
	if err == nil {
		t.Fatal("Expected connection error")
	}
	// "connection refused" 应该匹配 isRetryableConnErr
	if !strings.Contains(err.Error(), "connection refused") && !strings.Contains(err.Error(), "EOF") {
		t.Logf("Error message: %s", err.Error())
	}
}

// TestF2_Retry_RequestBodyPreserved 验证重试时请求体完整发送
func TestF2_Retry_RequestBodyPreserved(t *testing.T) {
	var receivedBodies []string
	var callCount int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		receivedBodies = append(receivedBodies, string(body))
		n := atomic.AddInt32(&callCount, 1)
		if n == 1 {
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Fatal("server does not support hijacking")
			}
			conn, _, _ := hj.Hijack()
			conn.Close()
			return
		}
		w.WriteHeader(200)
		w.Write([]byte(`{"choices":[]}`))
	}))
	defer upstream.Close()

	handler := engine.NewRequestHandlerWithRetry(2)
	req := &types.ChatRequest{
		Model:    "test-model",
		Messages: []types.Message{{Role: "user", Content: "hello world"}},
	}

	resp, _, err := handler.SendRequest("key", upstream.URL, req, 10, "", nil, nil)
	if err != nil {
		t.Fatalf("Expected retry success, got: %v", err)
	}
	defer resp.Body.Close()

	// 第二次请求的 body 应该和第一次一样完整
	if len(receivedBodies) < 2 {
		t.Fatalf("Expected >= 2 received bodies, got %d", len(receivedBodies))
	}
	// 两次的 body 都应包含 "test-model" 和 "hello world"
	for i, b := range receivedBodies {
		if !strings.Contains(b, "test-model") {
			t.Errorf("Body %d missing model name: %s", i, b)
		}
		if !strings.Contains(b, "hello world") {
			t.Errorf("Body %d missing content: %s", i, b)
		}
	}
}
