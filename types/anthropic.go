package types

// AnthropicMessagesRequest Anthropic Messages API 请求结构
type AnthropicMessagesRequest struct {
	Model         string                    `json:"model"`
	Messages      []AnthropicMessage        `json:"messages"`
	System        interface{}               `json:"system,omitempty"` // string 或 []AnthropicTextBlock
	MaxTokens     int                       `json:"max_tokens"`
	Temperature   *float64                  `json:"temperature,omitempty"`
	TopP          *float64                  `json:"top_p,omitempty"`
	TopK          *int                      `json:"top_k,omitempty"`
	StopSequences []string                  `json:"stop_sequences,omitempty"`
	Stream        bool                      `json:"stream,omitempty"`
	Tools         []AnthropicTool           `json:"tools,omitempty"`
	ToolChoice    interface{}               `json:"tool_choice,omitempty"`
	Metadata      map[string]interface{}    `json:"metadata,omitempty"`
}

// AnthropicMessage Anthropic 消息结构
type AnthropicMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // string 或 []AnthropicContentBlock
}

// AnthropicContentBlock Anthropic 内容块
type AnthropicContentBlock struct {
	Type       string            `json:"type"`
	Text       string            `json:"text,omitempty"`
	Source     *AnthropicSource  `json:"source,omitempty"`    // for image type
	ID         string            `json:"id,omitempty"`        // for tool_use
	Name       string            `json:"name,omitempty"`      // for tool_use
	Input      interface{}       `json:"input,omitempty"`     // for tool_use
	ToolUseID  string            `json:"tool_use_id,omitempty"` // for tool_result
	Content    interface{}       `json:"content,omitempty"`   // for tool_result
}

// AnthropicSource Anthropic 图像源
type AnthropicSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

// AnthropicTextBlock Anthropic 文本块
type AnthropicTextBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// AnthropicTool Anthropic 工具定义
type AnthropicTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

// AnthropicMessagesResponse Anthropic Messages API 响应
type AnthropicMessagesResponse struct {
	ID           string                  `json:"id"`
	Type         string                  `json:"type"`
	Role         string                  `json:"role"`
	Content      []AnthropicContentBlock `json:"content"`
	Model        string                  `json:"model"`
	StopReason   string                  `json:"stop_reason"`
	StopSequence *string                 `json:"stop_sequence,omitempty"`
	Usage        AnthropicUsage          `json:"usage"`
}

// AnthropicUsage Anthropic token 用量
type AnthropicUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
}
