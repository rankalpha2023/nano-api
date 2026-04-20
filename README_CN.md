# Nano API

一个轻量级、多提供商的 API 代理服务器，可将 OpenAI 兼容的请求转发到各种 LLM 提供商，具有内置的负载均衡、速率限制和自动故障转移功能。

## 特性

- **多提供商支持**：同时连接到多个 LLM 提供商（NVIDIA、OpenCode 等）
- **自动负载均衡**：在每个提供商的多个 API 密钥之间进行轮询分发
- **速率限制**：按提供商进行速率限制，以尊重 API 配额
- **模型映射**：使用友好的别名（如 "high"、"medium"），这些别名会映射到每个提供商的实际模型名称
- **代理支持**：当直接访问不可用时，通过代理路由请求
- **请求重试**：失败时自动重试，可配置重试次数
- **流式支持**：完全支持流式响应
- **CORS 支持**：与基于浏览器的客户端兼容

## 快速开始

### 1. 克隆仓库

```bash
git clone https://github.com/rankalpha2023/nano-api.git
cd nano-api
```

### 2. 配置

复制示例配置并使用您的 API 密钥进行编辑：

```bash
cp config.example.yaml config.yaml
```

编辑 `config.yaml`，添加您的提供商和 API 密钥。

### 3. 构建和运行

```bash
go build -o nano-api main.go
./nano-api
```

服务器默认会在端口 8080 上启动。

## 配置

### config.yaml 结构

```yaml
providers:
  - name: opencode                    # 提供商名称
    base-url: https://opencode.ai/zen/v1
    api-key-entries:                 # 多个 API 密钥用于负载均衡
      - your-api-key-1
      - your-api-key-2
    models:                          # 此提供商的模型别名
      high: minimax-m2.5-free
      medium: nemotron-3-super-free
    rate-limit:                      # 速率限制设置
      maxRequestsPerMinute: 40
      minIntervalMs: 100
      retryIntervalMs: 5000
    model-config:                    # 默认模型参数
      defaultTemperature: 0.1
      defaultTopP: 1.0
      defaultMaxTokens: 64000
    timeout: 30                      # 请求超时（秒）
    proxy: ""                        # 可选代理 URL

  - name: nvidia
    base-url: https://integrate.api.nvidia.com/v1
    api-key-entries:
      - your-nvidia-api-key
    models:
      high: z-ai/glm-5.1
    rate-limit:
      maxRequestsPerMinute: 40
    timeout: 120
    proxy: "http://localhost:16808"  # 代理示例

port: 8080                           # 服务器端口
request-retry: 3                     # 失败时的重试次数
rate-limit:                          # 全局速率限制回退
  maxRequestsPerMinute: 40
  minIntervalMs: 100
  retryIntervalMs: 5000
```

### 配置优先级

1. **提供商特定设置**优先
2. **全局设置**作为回退
3. 如果两者都未配置，使用**硬编码默认值**

## API 使用

### OpenAI 兼容端点

```
POST http://localhost:8080/v1/chat/completions
```

### 请求示例

```json
{
  "model": "high",
  "messages": [
    {
      "role": "user",
      "content": "你好，你怎么样？"
    }
  ],
  "stream": false
}
```

### 响应

服务器以与上游提供商相同的格式返回响应。

## 架构

```
┌─────────────┐     ┌──────────────────┐     ┌─────────────────┐
│   客户端    │────▶│   Nano API       │────▶│   提供商 1     │
│             │     │   (负载均衡)     │     │   (轮询)       │
└─────────────┘     │                  │     └─────────────────┘
                    │   速率限制器     │     ┌─────────────────┐
                    │   按提供商       │────▶│   提供商 2     │
                    │                  │     └─────────────────┘
                    └──────────────────┘
```

### 四层架构

- **Types**：数据模式和请求/响应结构
- **Core**：纯函数（速率限制、请求处理）
- **Engine**：状态管理和账号池
- **Server**：HTTP 路由和响应处理

## 开发

### 项目结构

```
.
├── config/          # 配置加载
├── core/            # 核心工具（速率限制器、请求处理程序）
├── engine/          # 账号管理器和负载均衡
├── server/          # HTTP 服务器和路由
├── types/           # 类型定义
├── config.yaml      # 本地配置（不提交）
├── config.example.yaml
└── main.go
```

### 运行测试

```bash
# 非流式测试
python test.py

# 流式测试
python test_stream.py
```

## 许可证

MIT
