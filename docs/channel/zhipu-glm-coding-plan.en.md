# ZhiPu GLM Coding Plan with Codex CLI

This guide explains how to connect ZhiPu GLM Coding Plan to Codex CLI through New API.

## How It Works

Codex CLI communicates using the OpenAI Responses API (`/v1/responses`) format, while ZhiPu GLM Coding Plan's OpenAI-compatible endpoint only supports Chat Completions. New API handles the conversion automatically:

```
Codex CLI → /v1/responses → New API (format conversion) → /chat/completions → ZhiPu GLM
```

Both streaming and non-streaming modes are supported, including bidirectional function call / tool use mapping.

## Channel Configuration

1. Go to the New API admin dashboard and click "Add Channel"
2. Fill in the following:

| Field | Value |
|-------|-------|
| Type | **ZhiPu GLM-4V** |
| Name | Custom, e.g. `ZhiPu Coding Plan` |
| Key | ZhiPu Open Platform API Key ([Get it here](https://open.bigmodel.cn/)) |
| Base URL | **`glm-coding-plan`** |
| Models | `glm-4.7`, `glm-5`, etc. |

> **Important**: Use `glm-coding-plan` (a built-in identifier) as the Base URL, not an actual URL. The system maps it to the correct endpoints automatically.

### Model Redirect

Codex CLI uses `gpt-5.1-codex` as the default model name. You need to configure model redirect in the channel:

1. Find "**Model Redirect**" in the channel edit page
2. Add a mapping:

| Key | Value |
|-----|-------|
| `gpt-5.1-codex` | `glm-4.7` |

### Token Configuration

1. Create a token on the "Tokens" page
2. **Do not enable model restrictions** (or ensure the list includes the actual model name being requested)

## Codex CLI Configuration

Use [cc-switch](https://github.com/anthropics/codex) or manually edit Codex config:

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
  "OPENAI_API_KEY": "sk-your-new-api-token"
}
```

## Special Base URLs

The following ZhiPu Coding Plan identifiers are built in:

| Identifier | OpenAI Compatible Endpoint | Claude Compatible Endpoint |
|------------|---------------------------|--------------------------|
| `glm-coding-plan` | `https://open.bigmodel.cn/api/coding/paas/v4` | `https://open.bigmodel.cn/api/anthropic` |
| `glm-coding-plan-international` | `https://api.z.ai/api/coding/paas/v4` | `https://api.z.ai/api/anthropic` |

For the international version, use `glm-coding-plan-international` as the Base URL.

## Notes

- This feature works via Responses API → Chat Completions format conversion, supporting both streaming and non-streaming
- Tool calling (function call) bidirectional mapping is supported
- If you encounter `stream disconnected before completion`, ensure you are using a version that includes this feature
- Under China mainland network, ZhiPu API responses may be slow — consider increasing Codex streaming timeout

## TODO

- [ ] Extract a generic Responses ↔ Chat Completions conversion layer to support more Chat-only channels with Codex
