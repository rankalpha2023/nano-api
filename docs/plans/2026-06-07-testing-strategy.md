# NanoAPI 端到端测试策略

## 目标

- 整体行覆盖率 ≥ 90%，types/core 包 ≥ 95%
- E2E 全链路测试 + 单元测试 + 集成测试，单命令 `go test ./...` 完成
- 消除 BUG 反复回归（Provider 配置串扰、限流逻辑回归）
- 解决"响应慢但无法归因"问题：日志 + 延迟测试

## 测试架构

```
E2E 测试 (server/e2e_test.go)
  完整链路：HTTP请求 → 路由 → 限流 → 转发 → 错误恢复
  用 httptest.Server 模拟上游

集成测试 (server/*_test.go)
  handler 层：CORS / 错误码 / 流式 / header 转发 / model 列表

功能测试 (engine/core/types 各 *_test.go)
  纯逻辑：路由/轮询/失败恢复/请求体构建/配置加载/限流
```

## 关键技术选择

| 项目 | 选择 |
|------|------|
| Mock 上游 | `httptest.NewServer()` 内联，支持自定义延迟和状态码 |
| NanoAPI 启动 | goroutine + `net.Listen(":0")` 随机端口 |
| 测试框架 | 标准 `testing` + `go test` + `t.Run()` 子测试 |
| 覆盖率 | `-coverprofile -covermode=count` |
| 并发测试 | `sync.WaitGroup` + 计时断言 |

## 覆盖率目标

| 包 | 目标 | 备注 |
|----|------|------|
| types | 95-100% | 纯数据结构 + 配置解析，极其容易 |
| core | 95-100% | 请求构建 + 限流，纯函数为主 |
| engine | ≥ 90% | 难点：路由/轮询/并发/限流/故障恢复 |
| server | ≥ 85% | goroutine 启动路径较难满覆盖 |
| **总计** | **≥ 90%** | |

## 测试文件结构

```
test_NanoAPI/
├── types/
│   └── config_test.go              # LoadConfig / GetDefaultConfig / YAML/JSON
├── core/
│   ├── request_test.go             # SendRequest / header / proxy / timeout
│   ├── request_body_test.go        # buildRequestBody（从 test/ 移入）
│   ├── rate_limit_test.go          # 单 provider 限流
│   └── rate_limit_concurrency_test.go  # 多 provider 独立限流 + 并发
├── engine/
│   ├── account_manager_test.go     # 路由/轮询/故障恢复/模型映射
│   └── provider_isolation_test.go  # provider 配置隔离专项
├── server/
│   ├── server_test.go              # handler: CORS / 错误码 / 响应格式
│   ├── stream_test.go              # 流式 / header转发 / chunk完整性
│   └── e2e_test.go                 # 完整链路: 限流+路由+转发+错误恢复
```

## 测试场景矩阵

### 1. Provider 独立性与配置隔离（核心优先级）

| 场景 | 验证点 |
|------|--------|
| 不同 ModelConfig | A 的 temperature/top_p/max_tokens 不泄漏到 B |
| 不同 Headers | A 的自定义头不出现 B 请求中 |
| 不同 ExtraFields | A 的 `thinking_enabled` 不污染 B |
| 不同 RateLimit | A `minIntervalMs=1000` 和 B `minIntervalMs=100` 独立生效 |
| 不同 Timeout | A `timeout=600` 和 B `timeout=30` 各自使用 |
| 不同 Proxy | 互不干扰 |

测试手段：两个 `httptest.Server` 冒充两个上游，各自记录收到的请求参数并逐一断言。

### 2. 多 Model 限流策略

| 场景 | 行为 | 验证 |
|------|------|------|
| 同一 provider 多个 model | 共享 RateLimiter | model-A 消耗令牌后 model-B 被限流阻塞 |
| 不同 provider 各自 model | 独立限流 | provider-A 耗尽后 provider-B 秒回 |
| 同一 model 配在多个 provider | 轮询 + 各自限流 | 互不阻塞 |

### 3. 并发请求

| 场景 | 预期 |
|------|------|
| 3 请求 → 3 个不同 provider | 并发执行，总耗时 ≈ max(单个) |
| 5 请求 → 同一 provider | 串行化，受 RateLimiter 约束 |
| 混合 2:1 | A 串行、B 并发，总耗时 ≈ max(A耗时, B单次) |

### 4. 错误处理与故障恢复

| 场景 | 预期 |
|------|------|
| 上游 401 | 账号标记失败，5s 内不再分配 |
| retryIntervalMs 到期 | 自动从 failed 回到 available |
| 全部账号失败 | HTTP 503 + 区分"无provider" vs "无账号" |
| 上游超时 | timeout 触发，账号标记失败 |
| 上游 500 | 非 2xx → 标记失败 |
| 上游 200 | MarkAccountAvailable，requestCount++ |

### 5. 流式响应

| 场景 | 验证 |
|------|------|
| SSE 格式 | `Content-Type: text/event-stream` |
| chunk 完整 | 4 chunk 全部到达，内容一致 |
| reasoning_content | 原样转发 |
| finish_reason | 最后一个 chunk 含 `finish_reason: "stop"` |
| header 转发 | `X-RateLimit-Remaining: 5` 到达客户端 |
| 大响应 | 100KB 不截断 |

### 6. 日志诊断与延迟归因

| 日志点 | 内容 |
|--------|------|
| 限流等待 | "rate_limiter waited XXms" |
| 上游响应 | "upstream response 200 in XXms" |
| 请求完成 | "request completed in XXms" |

测试断言：mock 注入 3s 延迟，验证日志显示 3s 花在上游而非限流。

## 慢响应归因对照表

| 日志特征 | 根因 | 解决方案 |
|----------|------|----------|
| `rate_limiter waited 5000ms` | 限流 | 调高 maxRequestsPerMinute |
| `upstream in 15000ms` | 远端慢 | 换 provider / 加超时 |
| `rate_limiter waited 100ms` + `upstream in 200ms` + `completed in 8000ms` | BUG | 追踪中间 7.7s 在哪 |

### 7. 配置继承与覆盖（全局 → Provider）

配置继承链：`config.yaml` 全局设置 → Provider 级覆盖。当前代码中存在的继承节点：

```
全局 Config
  ├── RateLimit (maxRequestsPerMinute / minIntervalMs / retryIntervalMs)
  ├── Port
  └── RequestRetry

Provider Config
  ├── RateLimit   ← 从全局继承（当 MaxRequestsPerMinute==0 && MinIntervalMs==0）
  ├── ModelConfig ← 无全局 fallback（全零即零值）
  ├── Timeout     ← 无全局 fallback（默认硬编码 30）
  └── Headers / ExtraFields / Proxy / Models  ← provider 专属，无继承
```

**⚠️ 发现代码 BUG**：`engine/account_manager.go:41`
```go
if rateLimit.MaxRequestsPerMinute == 0 && rateLimit.MinIntervalMs == 0 {
    rateLimit = defaultRateLimit
}
```
条件要求**两者都为 0**才继承全局。若 provider 只设了 `MaxRequestsPerMinute=60` 而不设 `MinIntervalMs`，则 `MinIntervalMs=0`（无限流间隔）——这是一个潜在问题。

#### 测试矩阵

| 场景 | 全局 | Provider | 预期生效 | 
|------|------|----------|----------|
| RateLimit: 两字段都为 0 | 40/100 | 0/0 | **全局 40/100** |
| RateLimit: 只有 MaxRequests ≠ 0 | 40/100 | 60/0 | **60/0 → BUG: MinIntervalMs=0** |
| RateLimit: 只有 MinIntervalMs ≠ 0 | 40/100 | 0/500 | **0/500 → BUG: 无限流上限** |
| RateLimit: 两者都非 0 | 40/100 | 60/500 | Provider 60/500 |
| RateLimit: RetryIntervalMs | 5000 | 2000 | Provider 2000（一直用 provider 值，无继承） |
| RateLimit: RetryIntervalMs 为 0 | 5000 | 0 | Provider 0（ticker 立即触发，无继承） |
| ModelConfig: 全默认 | - | 0/0/0 | **零值 → BUG: temperature/top_p/max_tokens 均为 0** |
| ModelConfig: 自定义 | - | 0.5/0.9/4096 | Provider 自定义值 |
| Timeout: 未设置 | - | 0 | 硬编码 30 |
| Timeout: 自定义 | - | 600 | Provider 600 |

#### 建议修复（与测试同步进行）

1. **RateLimit 继承逻辑**：改为逐字段继承——若 provider 字段为 0，使用全局对应字段
2. **ModelConfig 全局 fallback**：在 Config 根下添加全局 ModelConfig
3. **Timeout 全局 fallback**：在 Config 根下添加全局 Timeout

测试同时覆盖"修复前 BUG 行为"和"修复后正确行为"。

```bash
# 全部测试 + 覆盖率
go test -coverprofile=coverage.out -covermode=count ./...

# 可视化
go tool cover -html=coverage.out -o coverage.html

# CI 门禁
go test -race -coverprofile=coverage.out ./...
```
