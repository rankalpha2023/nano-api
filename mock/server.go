package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// MockChatResponse 模拟聊天响应
type MockChatResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
}

// Choice 选择结构
type Choice struct {
	Index        int    `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
	Delta        *Delta  `json:"delta,omitempty"`
}

// Message 消息结构
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Delta 增量结构
type Delta struct {
	Role            string `json:"role,omitempty"`
	Content         string `json:"content,omitempty"`
	ReasoningContent string `json:"reasoning_content,omitempty"`
}

// handleChatCompletions 处理聊天完成请求
func handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	// 解析请求
	var req struct {
		Model   string `json:"model"`
		Stream  bool   `json:"stream"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}
	}
	
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()
	
	// 设置响应头
	w.Header().Set("Content-Type", "application/json")
	
	// 处理流式响应
	if req.Stream {
		w.Header().Set("Transfer-Encoding", "chunked")
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming not supported", http.StatusInternalServerError)
			return
		}
		
		// 发送流式响应
		responses := []MockChatResponse{
			{
				ID:      "chatcmpl-123",
				Object:  "chat.completion.chunk",
				Created: time.Now().Unix(),
				Model:   req.Model,
				Choices: []Choice{{
					Index: 0,
					Delta: &Delta{Role: "assistant"},
				}},
			},
			{
				ID:      "chatcmpl-123",
				Object:  "chat.completion.chunk",
				Created: time.Now().Unix(),
				Model:   req.Model,
				Choices: []Choice{{
					Index: 0,
					Delta: &Delta{ReasoningContent: "I need to think about this..."},
				}},
			},
			{
				ID:      "chatcmpl-123",
				Object:  "chat.completion.chunk",
				Created: time.Now().Unix(),
				Model:   req.Model,
				Choices: []Choice{{
					Index: 0,
					Delta: &Delta{Content: "Hello! I'm an AI assistant. How can I help you today?"},
				}},
			},
			{
				ID:      "chatcmpl-123",
				Object:  "chat.completion.chunk",
				Created: time.Now().Unix(),
				Model:   req.Model,
				Choices: []Choice{{
					Index:        0,
					FinishReason: "stop",
				}},
			},
		}
		
		for _, resp := range responses {
			data, _ := json.Marshal(resp)
			fmt.Fprintf(w, "%s\n", data)
			flusher.Flush()
			time.Sleep(100 * time.Millisecond)
		}
	} else {
		// 发送非流式响应
		resp := MockChatResponse{
			ID:      "chatcmpl-123",
			Object:  "chat.completion",
			Created: time.Now().Unix(),
			Model:   req.Model,
			Choices: []Choice{{
				Index: 0,
				Message: Message{
					Role:    "assistant",
					Content: "Hello! I'm an AI assistant. How can I help you today?",
				},
				FinishReason: "stop",
			}},
		}
		
		json.NewEncoder(w).Encode(resp)
	}
}

func main() {
	// 注册路由
	http.HandleFunc("/v1/chat/completions", handleChatCompletions)
	
	// 启动服务器
	port := 8081
	fmt.Printf("Mock server starting on port %d...\n", port)
	if err := http.ListenAndServe(fmt.Sprintf(":%d", port), nil); err != nil {
		fmt.Printf("Failed to start mock server: %v\n", err)
	}
}
