package types

// ChatRequest OpenAI聊天请求结构
type ChatRequest struct {
	Model            string             `json:"model"`                       // 模型名称
	Messages         []Message          `json:"messages"`                    // 消息列表
	Temperature      float64            `json:"temperature"`                 // 温度参数
	TopP             float64            `json:"top_p"`                       // Top P参数
	MaxTokens        int                `json:"max_tokens"`                  // 最大token数
	Stream           bool               `json:"stream"`                      // 是否流式响应
	Tools            []Tool             `json:"tools,omitempty"`             // 工具列表
	ToolChoice       interface{}        `json:"tool_choice,omitempty"`       // 工具选择
	User             string             `json:"user,omitempty"`              // 用户标识符
	Stop             interface{}        `json:"stop,omitempty"`              // 停止词
	FrequencyPenalty float64            `json:"frequency_penalty,omitempty"` // 频率惩罚
	PresencePenalty  float64            `json:"presence_penalty,omitempty"`  // 存在惩罚
	LogitBias        map[string]float64 `json:"logit_bias,omitempty"`        // 对数偏见
	ResponseFormat   interface{}        `json:"response_format,omitempty"`   // 响应格式
	Seed             *int               `json:"seed,omitempty"`              // 种子值
	ExtraBody        *ExtraBody         `json:"extra_body,omitempty"`        // 额外参数
}

// Message 消息结构
type Message struct {
	Role       string      `json:"role"`                   // 角色
	Content    interface{} `json:"content"`                // 内容（支持字符串或数组）
	ToolCalls  []ToolCall  `json:"tool_calls,omitempty"`   // 工具调用
	ToolCallID string      `json:"tool_call_id,omitempty"` // 工具调用ID
}

// Tool 工具结构
type Tool struct {
	Type     string   `json:"type"`     // 工具类型
	Function Function `json:"function"` // 函数信息
}

// Function 函数结构
type Function struct {
	Name        string                 `json:"name"`        // 函数名称
	Description string                 `json:"description"` // 函数描述
	Parameters  map[string]interface{} `json:"parameters"`  // 参数定义
}

// ToolCall 工具调用结构
type ToolCall struct {
	ID       string       `json:"id"`       // 工具调用ID
	Type     string       `json:"type"`     // 工具调用类型
	Function FunctionCall `json:"function"` // 函数调用信息
}

// FunctionCall 函数调用结构
type FunctionCall struct {
	Name      string `json:"name"`      // 函数名称
	Arguments string `json:"arguments"` // 函数参数
}

// ExtraBody 额外参数结构
type ExtraBody struct {
	ChatTemplateKwargs ChatTemplateKwargs `json:"chat_template_kwargs"` // 聊天模板参数
}

// ChatTemplateKwargs 聊天模板参数
type ChatTemplateKwargs struct {
	EnableThinking bool `json:"enable_thinking"` // 是否启用思考
	ClearThinking  bool `json:"clear_thinking"`  // 是否清除思考
}

// Usage OpenAI token 用量信息
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ChatResponse OpenAI聊天响应结构
type ChatResponse struct {
	ID      string   `json:"id"`      // 响应ID
	Object  string   `json:"object"`  // 对象类型
	Created int64    `json:"created"` // 创建时间
	Model   string   `json:"model"`   // 模型名称
	Choices []Choice `json:"choices"` // 选择列表
	Usage   *Usage   `json:"usage,omitempty"` // token 用量
}

// Choice 选择结构
type Choice struct {
	Index        int     `json:"index"`           // 索引
	Message      Message `json:"message"`         // 消息
	FinishReason string  `json:"finish_reason"`   // 结束原因
	Delta        *Delta  `json:"delta,omitempty"` // 增量(流式响应)
}

// Delta 增量结构
type Delta struct {
	Role             string `json:"role,omitempty"`              // 角色
	Content          string `json:"content,omitempty"`           // 内容
	ReasoningContent string `json:"reasoning_content,omitempty"` // 思考内容
}

// ModelInfo 模型信息（OpenAI+Anthropic /v1/models 格式）
type ModelInfo struct {
	ID          string `json:"id"`
	Object      string `json:"object"`
	Created     int64  `json:"created"`
	OwnedBy     string `json:"owned_by"`
	DisplayName string `json:"display_name,omitempty"` // Anthropic 要求用于 gateway model discovery
}

// ModelListResponse /v1/models 响应
type ModelListResponse struct {
	Object string      `json:"object"`
	Data   []ModelInfo `json:"data"`
}
