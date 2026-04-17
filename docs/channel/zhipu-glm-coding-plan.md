# 智谱 GLM Coding Plan 接入 Codex CLI

本文档说明如何通过 New API 将智谱 GLM Coding Plan 接入 Codex CLI。

## 原理

Codex CLI 使用 OpenAI Responses API（`/v1/responses`）格式通信，而智谱 GLM Coding Plan 的 OpenAI 兼容端点仅支持 Chat Completions 格式。New API 在中间自动完成：

```
Codex CLI → /v1/responses → New API（格式转换）→ /chat/completions → 智谱 GLM
```

支持非流式和流式两种模式，包括 function call / tool use 的双向映射。

## 渠道配置

1. 进入 New API 管理后台，点击「添加渠道」
2. 填写以下配置：

| 字段 | 值 |
|------|-----|
| 类型 | **智谱 GLM-4V** |
| 名称 | 自定义，如 `智谱 Coding Plan` |
| 密钥 | 智谱开放平台 API Key（[获取地址](https://open.bigmodel.cn/)） |
| API 地址 | **`glm-coding-plan`** |
| 模型 | `glm-4.7`、`glm-5` 等 |

> **重要**：API 地址填写 `glm-coding-plan`（内置标识），不要填写实际 URL。系统会自动映射到正确的端点。

### 模型重定向

Codex CLI 默认使用 `gpt-5.1-codex` 作为模型名。需要在渠道中配置模型重定向：

1. 在渠道编辑页面找到「**模型重定向**」
2. 添加映射：

| 键 | 值 |
|----|-----|
| `gpt-5.1-codex` | `glm-4.7` |

### 令牌配置

1. 在「令牌」页面创建令牌
2. **不要启用模型限制**（或确保列表中包含实际请求的模型名）

## Codex CLI 配置

使用 [cc-switch](https://github.com/anthropics/codex) 或手动编辑 Codex 配置：

### config.toml

```toml
model = "gpt-5.1-codex"

[model_providers.newapi]
name = "NewAPI"
base_url = "http://<your-new-api-host>:3000/v1"
wire_api = "responses"
requires_openai_auth = true
```

### auth.json

```json
{
  "OPENAI_API_KEY": "sk-你的NewAPI令牌"
}
```

## 特殊 Base URL 说明

系统内置了以下智谱 Coding Plan 标识：

| 标识 | OpenAI 兼容端点 | Claude 兼容端点 |
|------|----------------|----------------|
| `glm-coding-plan` | `https://open.bigmodel.cn/api/coding/paas/v4` | `https://open.bigmodel.cn/api/anthropic` |
| `glm-coding-plan-international` | `https://api.z.ai/api/coding/paas/v4` | `https://api.z.ai/api/anthropic` |

国际版使用 `glm-coding-plan-international` 作为 API 地址即可。

## 注意事项

- 该功能通过 Responses API → Chat Completions 的格式转换实现，支持流式和非流式
- Tool calling（function call）已支持双向映射
- 如遇到 `stream disconnected before completion` 错误，请确认使用的是包含此功能的版本
- 国内网络环境下，智谱 API 响应可能较慢，Codex 的流式超时可能需要适当调大

## TODO

- [ ] 提取通用的 Responses ↔ Chat Completions 转换层，支持更多仅支持 Chat Completions 的渠道接入 Codex
