package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"nano-api/engine"
	"nano-api/types"
)

// ============================================================
// 边缘路径测试：覆盖 io.ReadAll 错误、非 EOF 读取错误等罕见分支
// ============================================================

// failingReader 实现 io.ReadCloser，Read 总是返回错误（模拟请求体读取失败）
type failingReader struct {
	err error
}

func (f failingReader) Read(p []byte) (int, error) {
	return 0, f.err
}

func (f failingReader) Close() error {
	return nil
}

// ============================================================
// readChatRequest: io.ReadAll(r.Body) 错误分支
// ============================================================

// TestReadChatRequest_BodyReadError 请求体读取失败 → 400
func TestReadChatRequest_BodyReadError(t *testing.T) {
	am := engine.NewAccountManager(&types.Config{})
	s := NewServer(am)

	req := httptest.NewRequest("POST", "/v1/chat/completions", failingReader{err: errors.New("simulated read error")})
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	_, err := s.readChatRequest(w, req, "trace-test")
	if err == nil {
		t.Error("Expected error from failing reader")
	}
	if w.Result().StatusCode != 400 {
		t.Errorf("Expected 400 on body read error, got %d", w.Result().StatusCode)
	}
}

// TestReadChatRequest_MissingContentType 缺少 Content-Type → 400
func TestReadChatRequest_MissingContentType(t *testing.T) {
	am := engine.NewAccountManager(&types.Config{})
	s := NewServer(am)

	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader([]byte(`{"model":"m1"}`)))
	// 不设置 Content-Type
	w := httptest.NewRecorder()

	_, err := s.readChatRequest(w, req, "trace-test")
	if err == nil {
		t.Error("Expected error for missing Content-Type")
	}
	if w.Result().StatusCode != 400 {
		t.Errorf("Expected 400 for missing Content-Type, got %d", w.Result().StatusCode)
	}
}

// TestReadChatRequest_EmptyBody 空请求体 → 400
func TestReadChatRequest_EmptyBody(t *testing.T) {
	am := engine.NewAccountManager(&types.Config{})
	s := NewServer(am)

	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader([]byte{}))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	_, err := s.readChatRequest(w, req, "trace-test")
	if err == nil {
		t.Error("Expected error for empty body")
	}
	if w.Result().StatusCode != 400 {
		t.Errorf("Expected 400 for empty body, got %d", w.Result().StatusCode)
	}
}

// TestReadChatRequest_InvalidJSON 无效 JSON → 400
func TestReadChatRequest_InvalidJSON(t *testing.T) {
	am := engine.NewAccountManager(&types.Config{})
	s := NewServer(am)

	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader([]byte(`not json`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	_, err := s.readChatRequest(w, req, "trace-test")
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
	if w.Result().StatusCode != 400 {
		t.Errorf("Expected 400 for invalid JSON, got %d", w.Result().StatusCode)
	}
}

// TestReadChatRequest_Success 正常请求 → 返回 ChatRequest
func TestReadChatRequest_Success(t *testing.T) {
	am := engine.NewAccountManager(&types.Config{})
	s := NewServer(am)

	body, _ := json.Marshal(types.ChatRequest{Model: "m1", Messages: []types.Message{{Role: "user", Content: "hi"}}})
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	result, err := s.readChatRequest(w, req, "trace-test")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("Expected non-nil result")
	}
	if result.Model != "m1" {
		t.Errorf("Expected model='m1', got '%s'", result.Model)
	}
}

// ============================================================
// handleChatCompletions: 请求体读取失败分支
// ============================================================

// TestHandleChatCompletions_BodyReadError 请求体读取失败 → 400
func TestHandleChatCompletions_BodyReadError(t *testing.T) {
	am := engine.NewAccountManager(&types.Config{})
	s := NewServer(am)

	req := httptest.NewRequest("POST", "/v1/chat/completions", failingReader{err: errors.New("read error")})
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.handleChatCompletions(w, req)

	if w.Result().StatusCode != 400 {
		t.Errorf("Expected 400 on body read error, got %d", w.Result().StatusCode)
	}
}

// ============================================================
// handleAnthropicMessages: 请求体读取失败分支
// ============================================================

// TestHandleAnthropicMessages_BodyReadError 请求体读取失败 → 400
func TestHandleAnthropicMessages_BodyReadError(t *testing.T) {
	am := engine.NewAccountManager(&types.Config{})
	s := NewServer(am)

	req := httptest.NewRequest("POST", "/v1/messages", failingReader{err: errors.New("read error")})
	w := httptest.NewRecorder()

	s.handleAnthropicMessages(w, req)

	if w.Result().StatusCode != 400 {
		t.Errorf("Expected 400 on body read error, got %d", w.Result().StatusCode)
	}
}

// ============================================================
// forwardUpstreamResponse: 非 EOF 读取错误分支
// ============================================================

// TestForwardUpstreamResponse_NonEOFReadError 上游响应体读取非 EOF 错误
// 通过 hijack 连接，发送 Content-Length 不匹配的响应体，触发 unexpected EOF
func TestForwardUpstreamResponse_NonEOFReadError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("server does not support hijacking")
		}
		conn, _, _ := hj.Hijack()
		defer conn.Close()
		// 写入 HTTP 响应头，声明 Content-Length: 1000，但只发送 5 字节 body
		fmt.Fprint(conn, "HTTP/1.1 200 OK\r\nContent-Length: 1000\r\nContent-Type: application/json\r\n\r\nhello")
	}))
	defer upstream.Close()

	am := makeTestAM(t, upstream.URL)
	s := NewServer(am)

	body, _ := json.Marshal(types.ChatRequest{Model: "m1", Messages: []types.Message{{Role: "user", Content: "hi"}}})
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.handleChatCompletions(w, req)

	// 上游返回 200，部分数据被转发，但 body 读取会出错
	// forwardUpstreamResponse 会 break 循环，不返回错误给客户端
	resp := w.Result()
	if resp.StatusCode != 200 {
		t.Errorf("Expected 200 (status already written before body error), got %d", resp.StatusCode)
	}
	// 应收到部分数据
	if w.Body.String() != "hello" {
		t.Errorf("Expected partial body 'hello', got '%s'", w.Body.String())
	}
}

// TestForwardUpstreamResponse_NonEOFReadErrorStream 流式响应中非 EOF 读取错误
func TestForwardUpstreamResponse_NonEOFReadErrorStream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("server does not support hijacking")
		}
		conn, _, _ := hj.Hijack()
		defer conn.Close()
		// 写入流式响应头，发送部分 chunk 后断开
		fmt.Fprint(conn, "HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\nTransfer-Encoding: chunked\r\n\r\n")
		fmt.Fprint(conn, "5\r\nhello\r\n")
		// 不发送结束 chunk，直接关闭连接
	}))
	defer upstream.Close()

	am := makeTestAM(t, upstream.URL)
	s := NewServer(am)

	body, _ := json.Marshal(types.ChatRequest{
		Model:    "m1",
		Messages: []types.Message{{Role: "user", Content: "hi"}},
		Stream:   true,
	})
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.handleChatCompletions(w, req)

	resp := w.Result()
	if resp.StatusCode != 200 {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}
	// 流式响应头应被设置
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Expected text/event-stream, got '%s'", ct)
	}
}

// ============================================================
// handleAnthropicMessages: 上游响应体读取失败分支
// ============================================================

// TestHandleAnthropicMessages_UpstreamBodyReadError 上游响应体读取失败 → 500
func TestHandleAnthropicMessages_UpstreamBodyReadError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("server does not support hijacking")
		}
		conn, _, _ := hj.Hijack()
		defer conn.Close()
		// 声明 Content-Length: 1000，但只发送 5 字节
		fmt.Fprint(conn, "HTTP/1.1 200 OK\r\nContent-Length: 1000\r\nContent-Type: application/json\r\n\r\nhello")
	}))
	defer upstream.Close()

	am := makeAnthropicTestAM(t, upstream.URL)
	s := NewServer(am)

	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(makeAnthropicRequest("claude-sonnet-4-6", false)))
	w := httptest.NewRecorder()

	s.handleAnthropicMessages(w, req)

	// io.ReadAll 读取上游 body 失败 → 500
	if w.Result().StatusCode != 500 {
		t.Errorf("Expected 500 on upstream body read error, got %d", w.Result().StatusCode)
	}
}

// ============================================================
// generateTraceID 测试
// ============================================================

// TestGenerateTraceID_Format traceID 格式验证
func TestGenerateTraceID_Format(t *testing.T) {
	id := generateTraceID()
	if len(id) != 24 {
		t.Errorf("Expected length 24, got %d ('%s')", len(id), id)
	}
	if id[:4] != "req-" {
		t.Errorf("Expected prefix 'req-', got '%s'", id[:4])
	}
}

// TestGenerateTraceID_Unique 多次调用生成唯一 ID
func TestGenerateTraceID_Unique(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := generateTraceID()
		if seen[id] {
			t.Errorf("Duplicate traceID: %s", id)
		}
		seen[id] = true
	}
}

// ============================================================
// setCORSHeaders 测试
// ============================================================

// TestSetCORSHeaders 验证 CORS 头设置
func TestSetCORSHeaders(t *testing.T) {
	am := engine.NewAccountManager(&types.Config{})
	s := NewServer(am)

	w := httptest.NewRecorder()
	s.setCORSHeaders(w, "POST, GET, OPTIONS")

	resp := w.Result()
	if resp.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("Expected origin '*', got '%s'", resp.Header.Get("Access-Control-Allow-Origin"))
	}
	if resp.Header.Get("Access-Control-Allow-Methods") != "POST, GET, OPTIONS" {
		t.Errorf("Expected methods 'POST, GET, OPTIONS', got '%s'", resp.Header.Get("Access-Control-Allow-Methods"))
	}
	if resp.Header.Get("Access-Control-Allow-Headers") != "Content-Type, Authorization" {
		t.Errorf("Expected headers 'Content-Type, Authorization', got '%s'", resp.Header.Get("Access-Control-Allow-Headers"))
	}
}

// ============================================================
// handleListModels 边缘测试
// ============================================================

// TestHandleListModels_NoProviders 无 provider 时返回空列表
func TestHandleListModels_NoProviders(t *testing.T) {
	am := engine.NewAccountManager(&types.Config{
		Providers: []types.ProviderConfig{},
	})
	s := NewServer(am)

	req := httptest.NewRequest("GET", "/v1/models", nil)
	w := httptest.NewRecorder()
	s.handleListModels(w, req)

	resp := w.Result()
	if resp.StatusCode != 200 {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}

	var body types.ModelListResponse
	json.NewDecoder(resp.Body).Decode(&body)
	if body.Object != "list" {
		t.Errorf("Expected 'list', got '%s'", body.Object)
	}
	if len(body.Data) != 0 {
		t.Errorf("Expected 0 models, got %d", len(body.Data))
	}
}

// TestHandleListModels_MultipleProviders 多 provider 去重
func TestHandleListModels_MultipleProviders(t *testing.T) {
	am := engine.NewAccountManager(&types.Config{
		Providers: []types.ProviderConfig{
			{Name: "p1", BaseURL: "http://x", APIKeyEntries: []string{"k"}, Models: map[string]string{"gpt-4": "gpt-4-real", "gpt-3.5": "gpt-3.5"}},
			{Name: "p2", BaseURL: "http://x", APIKeyEntries: []string{"k"}, Models: map[string]string{"gpt-4": "gpt-4-v2", "claude": "claude-real"}},
		},
	})
	s := NewServer(am)

	req := httptest.NewRequest("GET", "/v1/models", nil)
	w := httptest.NewRecorder()
	s.handleListModels(w, req)

	resp := w.Result()
	if resp.StatusCode != 200 {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}

	var body types.ModelListResponse
	json.NewDecoder(resp.Body).Decode(&body)
	// gpt-4 在两个 provider 中都有，去重后应为 1 个
	// 加上 gpt-3.5 和 claude，总共 3 个
	if len(body.Data) != 3 {
		t.Errorf("Expected 3 models (gpt-4, gpt-3.5, claude), got %d", len(body.Data))
	}
}

// ============================================================
// NewServer 测试
// ============================================================

// TestNewServer 验证 NewServer 返回非 nil
func TestNewServer(t *testing.T) {
	am := engine.NewAccountManager(&types.Config{})
	s := NewServer(am)
	if s == nil {
		t.Fatal("Expected non-nil Server")
	}
	if s.accountManager == nil {
		t.Error("Expected non-nil accountManager")
	}
}
