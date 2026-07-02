package engine

import (
	"log"
	"net/http"
	"time"

	"nano-api/types"
)

// ProcessChatCompletionResult 一次聊天完成请求的处理结果。
//
// 由 Engine 层填充，UI 层仅据此转发响应。
// Response.Body 的关闭与转发由 UI 层负责（因 body 读取时机由流式/非流式决定）。
type ProcessChatCompletionResult struct {
	Response      *http.Response   // 上游响应
	Metrics       *RequestMetrics  // 性能指标
	Account       *types.Account   // 使用的账户（用于成功后 MarkAccountAvailable）
	RateLimitWait time.Duration    // 限流等待耗时
	AuthFailed    bool             // 401/403：账户认证失败（UI 层据此跳过 MarkAccountAvailable）
}

// ProcessChatCompletion 编排完整的聊天完成请求处理流程。
//
// 承担的 Engine 层职责（UI 层不应承载）：
//  1. 通过 SendRequest 选择 provider/account、应用限流、返回 realModel
//  2. 应用 provider 的模型参数默认值（temperature/top_p/max_tokens）
//  3. 用 realModel 更新请求的 Model 字段
//  4. 调用 RequestHandler.SendRequest 发送上游请求
//  5. SendRequest 失败时标记账户失败（MarkAccountFailed）
//  6. 上游返回 401/403 时标记账户失败（MarkAccountFailed）
//
// 不包含的职责（由 UI 层负责）：
//   - HTTP 请求体读取/解析
//   - 响应头复制、响应体转发（流式/非流式分支）
//   - 成功后 MarkAccountAvailable（必须在 body 读取完成后调用）
//
// 返回值：
//   - result == nil 且 err == nil：无可用 provider/account（UI 层应返回 503）
//   - err != nil：发送失败（账户已标记失败，UI 层应返回 500）
//   - result != nil：成功拿到上游响应（UI 层转发响应体，并在非 AuthFailed 时调用 MarkAccountAvailable）
func (am *AccountManager) ProcessChatCompletion(req *types.ChatRequest) (*ProcessChatCompletionResult, error) {
	requestHandler, account, realModel, rateLimitWait, err := am.SendRequest(req)
	if err != nil {
		return nil, err
	}
	if account == nil {
		return nil, nil
	}

	// 应用 provider 的模型参数默认值（仅当请求未显式指定时）
	am.applyModelDefaults(req, account)

	// 模型名映射：若 provider 配置了别名映射，更新为真实模型名
	if realModel != "" && realModel != req.Model {
		log.Printf("Updating request model: %s -> %s", req.Model, realModel)
		req.Model = realModel
	}

	// 默认超时
	timeout := account.Timeout
	if timeout == 0 {
		timeout = 30
	}

	resp, metrics, err := requestHandler.SendRequest(
		account.APIKey, account.BaseURL, req, timeout,
		account.Proxy, account.Headers, account.ExtraFields,
	)
	if err != nil {
		log.Printf("Error sending request to %s: %v", account.ProviderName, err)
		am.MarkAccountFailed(account)
		return nil, err
	}

	// F3: 401/403 表示认证问题（API Key 失效），标记账户失败。
	// 仍把响应返回给 UI 层转发，让客户端知晓错误。
	authFailed := resp.StatusCode == 401 || resp.StatusCode == 403
	if authFailed {
		log.Printf("Upstream returned %d, marking account failed (auth issue)", resp.StatusCode)
		am.MarkAccountFailed(account)
	}

	return &ProcessChatCompletionResult{
		Response:      resp,
		Metrics:       metrics,
		Account:       account,
		RateLimitWait: rateLimitWait,
		AuthFailed:    authFailed,
	}, nil
}

// applyModelDefaults 应用 provider 的模型参数默认值到请求。
// 仅当请求未显式设置（值为 0）时才填充默认值。
func (am *AccountManager) applyModelDefaults(req *types.ChatRequest, account *types.Account) {
	modelConfig := am.GetModelConfig(account.ProviderName)
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
}
