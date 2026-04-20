package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"testing"
	"time"

	"nvidia-api-proxy/config"
	"nvidia-api-proxy/engine"
	"nvidia-api-proxy/server"
	"nvidia-api-proxy/types"
)

// 启动MOCK服务器
func startMockServer() error {
	// 创建MOCK服务器进程
	cmd := exec.Command("go", "run", "mock/server.go")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	
	// 启动MOCK服务器
	if err := cmd.Start(); err != nil {
		return err
	}
	
	// 等待MOCK服务器启动
	time.Sleep(1 * time.Second)
	
	return nil
}

// 测试非流式响应
func TestNonStreamResponse(t *testing.T) {
	// 启动MOCK服务器
	if err := startMockServer(); err != nil {
		t.Fatalf("Failed to start mock server: %v", err)
	}
	
	// 创建测试配置，使用MOCK服务器地址
	testConfig := &types.Config{
		Servers: []types.ServerConfig{
			{
				Name:    "mock",
				APIKey:  "test-key",
				BaseURL: "http://localhost:8081",
			},
		},
		RateLimit: types.RateLimitConfig{
			MaxRequestsPerMinute: 60,
			MinIntervalMs:        10,
			RetryIntervalMs:      1000,
		},
	}
	
	// 初始化帐号管理器
	accountManager := engine.NewAccountManager(testConfig)
	accountManager.Start()
	
	// 初始化服务器
	s := server.NewServer(accountManager)
	
	// 启动服务器
	go func() {
		if err := s.Start(8080); err != nil {
			t.Logf("Failed to start server: %v", err)
		}
	}()
	
	// 等待服务器启动
	time.Sleep(1 * time.Second)
	
	// 构建测试请求
	reqBody := types.ChatRequest{
		Model:       "z-ai/glm-5.1",
		Messages:    []types.Message{{Role: "user", Content: "Hello"}},
		Temperature: 1,
		TopP:        1,
		MaxTokens:   1000,
		Stream:      false,
	}
	
	// 发送请求到我们的服务器
	data, _ := json.Marshal(reqBody)
	httpReq, _ := http.NewRequest("POST", "http://localhost:8080/v1/chat/completions", bytes.NewBuffer(data))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer test-key")
	
	client := &http.Client{}
	resp, err := client.Do(httpReq)
	if err != nil {
		t.Fatalf("Failed to send request: %v", err)
	}
	defer resp.Body.Close()
	
	// 验证响应状态码
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status code 200, got %d", resp.StatusCode)
	}
	
	// 验证响应内容
	var response types.ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	
	if response.Choices[0].Message.Content != "Hello! I'm an AI assistant. How can I help you today?" {
		t.Errorf("Expected response content, got %s", response.Choices[0].Message.Content)
	}
}

// 测试流式响应
func TestStreamResponse(t *testing.T) {
	// 启动MOCK服务器
	if err := startMockServer(); err != nil {
		t.Fatalf("Failed to start mock server: %v", err)
	}
	
	// 创建测试配置，使用MOCK服务器地址
	testConfig := &types.Config{
		Servers: []types.ServerConfig{
			{
				Name:    "mock",
				APIKey:  "test-key",
				BaseURL: "http://localhost:8081",
			},
		},
		RateLimit: types.RateLimitConfig{
			MaxRequestsPerMinute: 60,
			MinIntervalMs:        10,
			RetryIntervalMs:      1000,
		},
	}
	
	// 初始化帐号管理器
	accountManager := engine.NewAccountManager(testConfig)
	accountManager.Start()
	
	// 初始化服务器
	s := server.NewServer(accountManager)
	
	// 启动服务器
	go func() {
		if err := s.Start(8080); err != nil {
			t.Logf("Failed to start server: %v", err)
		}
	}()
	
	// 等待服务器启动
	time.Sleep(1 * time.Second)
	
	// 构建测试请求
	reqBody := types.ChatRequest{
		Model:       "z-ai/glm-5.1",
		Messages:    []types.Message{{Role: "user", Content: "Hello"}},
		Temperature: 1,
		TopP:        1,
		MaxTokens:   1000,
		Stream:      true,
	}
	
	// 发送请求到我们的服务器
	data, _ := json.Marshal(reqBody)
	httpReq, _ := http.NewRequest("POST", "http://localhost:8080/v1/chat/completions", bytes.NewBuffer(data))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer test-key")
	
	client := &http.Client{}
	resp, err := client.Do(httpReq)
	if err != nil {
		t.Fatalf("Failed to send request: %v", err)
	}
	defer resp.Body.Close()
	
	// 验证响应状态码
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status code 200, got %d", resp.StatusCode)
	}
	
	// 验证流式响应
	decoder := json.NewDecoder(resp.Body)
	var chunks []map[string]interface{}
	for {
		var chunk map[string]interface{}
		if err := decoder.Decode(&chunk); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("Failed to decode chunk: %v", err)
		}
		chunks = append(chunks, chunk)
	}
	
	if len(chunks) == 0 {
		t.Error("Expected at least one chunk in streaming response")
	}
}

func main() {
	// 运行测试
	t := &testing.T{}
	TestNonStreamResponse(t)
	TestStreamResponse(t)
	
	if !t.Failed() {
		fmt.Println("All tests passed!")
	}
}
