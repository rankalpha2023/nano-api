package core

import (
	"encoding/json"
	"fmt"
	"strings"

	"nano-api/types"
)

// AnthropicToOpenAI 把 Anthropic Messages 请求转换成 nano-api 内部的 ChatRequest。
// 纯转换函数（无副作用），从 server 层迁入 core 层。
// UI 层应仅负责渲染与转发，不应承载格式转换逻辑。
func AnthropicToOpenAI(ar *types.AnthropicMessagesRequest) *types.ChatRequest {
	req := &types.ChatRequest{
		Model:     ar.Model,
		MaxTokens: ar.MaxTokens,
		Stream:    ar.Stream,
	}

	if ar.Temperature != nil {
		req.Temperature = *ar.Temperature
	}
	if ar.TopP != nil {
		req.TopP = *ar.TopP
	}

	// 把 Anthropic Messages 转为 OpenAI Messages
	var openaiMessages []types.Message

	// 处理 system prompt
	if ar.System != nil {
		var systemContent string
		switch s := ar.System.(type) {
		case string:
			systemContent = s
		case []interface{}:
			// []AnthropicTextBlock
			var parts []string
			for _, block := range s {
				if m, ok := block.(map[string]interface{}); ok {
					if t, _ := m["type"].(string); t == "text" {
						if text, _ := m["text"].(string); text != "" {
							parts = append(parts, text)
						}
					}
				}
			}
			systemContent = strings.Join(parts, "\n")
		}
		if systemContent != "" {
			openaiMessages = append(openaiMessages, types.Message{
				Role:    "system",
				Content: systemContent,
			})
		}
	}

	// 转换每条消息
	for _, am := range ar.Messages {
		om := AnthropicMessageToOpenAI(am)
		openaiMessages = append(openaiMessages, om)
	}

	req.Messages = openaiMessages

	// 转换 tools
	if len(ar.Tools) > 0 {
		var openaiTools []types.Tool
		for _, at := range ar.Tools {
			openaiTools = append(openaiTools, types.Tool{
				Type: "function",
				Function: types.Function{
					Name:        at.Name,
					Description: at.Description,
					Parameters:  at.InputSchema,
				},
			})
		}
		req.Tools = openaiTools
	}

	if ar.ToolChoice != nil {
		req.ToolChoice = ar.ToolChoice
	}

	return req
}

// AnthropicMessageToOpenAI 将单条 Anthropic 消息转为 OpenAI 消息格式（纯函数）。
func AnthropicMessageToOpenAI(am types.AnthropicMessage) types.Message {
	om := types.Message{
		Role: am.Role,
	}

	switch c := am.Content.(type) {
	case string:
		om.Content = c
	case []interface{}:
		var textParts []string
		var toolCalls []types.ToolCall
		var toolCallID string
		var toolResultContent string

		for _, block := range c {
			m, ok := block.(map[string]interface{})
			if !ok {
				continue
			}
			blockType, _ := m["type"].(string)

			switch blockType {
			case "text":
				if text, _ := m["text"].(string); text != "" {
					textParts = append(textParts, text)
				}
			case "tool_use":
				id, _ := m["id"].(string)
				name, _ := m["name"].(string)
				input, _ := m["input"]
				inputJSON, _ := json.Marshal(input)
				toolCalls = append(toolCalls, types.ToolCall{
					ID:   id,
					Type: "function",
					Function: types.FunctionCall{
						Name:      name,
						Arguments: string(inputJSON),
					},
				})
			case "tool_result":
				toolCallID, _ = m["tool_use_id"].(string)
				if content, ok := m["content"]; ok {
					switch cc := content.(type) {
					case string:
						toolResultContent = cc
					case []interface{}:
						for _, cblock := range cc {
							if cm, ok := cblock.(map[string]interface{}); ok {
								if ct, _ := cm["type"].(string); ct == "text" {
									if txt, _ := cm["text"].(string); txt != "" {
										toolResultContent += txt
									}
								}
							}
						}
					default:
						b, _ := json.Marshal(content)
						toolResultContent = string(b)
					}
				}
			}
		}

		if len(textParts) > 0 && len(toolCalls) == 0 {
			om.Content = strings.Join(textParts, "\n")
		} else if len(toolCalls) > 0 {
			om.Content = nil
			om.ToolCalls = toolCalls
			if len(textParts) > 0 {
				om.Content = strings.Join(textParts, "\n")
			}
		}

		if toolCallID != "" {
			om.ToolCallID = toolCallID
			om.Role = "tool"
			om.Content = toolResultContent
		}
	}

	return om
}

// OpenAIToAnthropicResponse 把 OpenAI ChatResponse 转成 Anthropic MessagesResponse（纯函数）。
func OpenAIToAnthropicResponse(respBody []byte, modelOverride string) (*types.AnthropicMessagesResponse, error) {
	var oaiResp types.ChatResponse
	if err := json.Unmarshal(respBody, &oaiResp); err != nil {
		return nil, fmt.Errorf("unmarshal OpenAI response: %w", err)
	}

	if len(oaiResp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	choice := oaiResp.Choices[0]
	msg := choice.Message

	// 终止原因映射
	stopReason := "end_turn"
	switch choice.FinishReason {
	case "stop":
		stopReason = "end_turn"
	case "length":
		stopReason = "max_tokens"
	case "tool_calls":
		stopReason = "tool_use"
	case "content_filter":
		stopReason = "end_turn"
	}

	modelName := oaiResp.Model
	if modelOverride != "" {
		modelName = modelOverride
	}

	resp := &types.AnthropicMessagesResponse{
		ID:         oaiResp.ID,
		Type:       "message",
		Role:       "assistant",
		Model:      modelName,
		StopReason: stopReason,
		Content:    []types.AnthropicContentBlock{},
		Usage:      types.AnthropicUsage{},
	}

	if oaiResp.Usage != nil {
		resp.Usage.InputTokens = oaiResp.Usage.PromptTokens
		resp.Usage.OutputTokens = oaiResp.Usage.CompletionTokens
	}

	// 文本内容
	if textContent, ok := msg.Content.(string); ok && textContent != "" {
		resp.Content = append(resp.Content, types.AnthropicContentBlock{
			Type: "text",
			Text: textContent,
		})
	}

	// 工具调用转换
	for _, tc := range msg.ToolCalls {
		var input interface{}
		if tc.Function.Arguments != "" {
			json.Unmarshal([]byte(tc.Function.Arguments), &input)
		}
		resp.Content = append(resp.Content, types.AnthropicContentBlock{
			Type:  "tool_use",
			ID:    tc.ID,
			Name:  tc.Function.Name,
			Input: input,
		})
	}

	if len(resp.Content) == 0 {
		resp.Content = append(resp.Content, types.AnthropicContentBlock{
			Type: "text",
			Text: "",
		})
	}

	return resp, nil
}
