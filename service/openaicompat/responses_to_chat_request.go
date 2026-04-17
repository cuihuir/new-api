package openaicompat

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

func ResponsesRequestToChatCompletionsRequest(req *dto.OpenAIResponsesRequest) (*dto.GeneralOpenAIRequest, error) {
	if req == nil {
		return nil, errors.New("request is nil")
	}
	if req.Model == "" {
		return nil, errors.New("model is required")
	}

	var messages []dto.Message

	// Convert instructions → system message
	if len(req.Instructions) > 0 {
		var instrStr string
		if err := common.Unmarshal(req.Instructions, &instrStr); err == nil {
			instrStr = strings.TrimSpace(instrStr)
			if instrStr != "" {
				messages = append(messages, dto.Message{
					Role:    "system",
					Content: instrStr,
				})
			}
		}
	}

	// Convert input → messages
	inputMessages, err := convertResponsesInputToMessages(req.Input)
	if err != nil {
		return nil, err
	}
	messages = append(messages, inputMessages...)

	// Convert tools
	var tools []dto.ToolCallRequest
	if len(req.Tools) > 0 {
		var rawTools []map[string]any
		if err := common.Unmarshal(req.Tools, &rawTools); err == nil {
			for _, t := range rawTools {
				ttype, _ := t["type"].(string)
				if ttype == "function" {
					fn, _ := t["function"].(map[string]any)
					if fn == nil {
						// Responses API format: {type: "function", name: "...", parameters: ...}
						tool := dto.ToolCallRequest{Type: "function", Function: dto.FunctionRequest{}}
						if name, ok := t["name"].(string); ok {
							tool.Function.Name = name
						}
						if desc, ok := t["description"].(string); ok {
							tool.Function.Description = desc
						}
						if params, ok := t["parameters"]; ok {
							tool.Function.Parameters = params
						}
						tools = append(tools, tool)
					} else {
						tool := dto.ToolCallRequest{Type: "function", Function: dto.FunctionRequest{}}
						if name, ok := fn["name"].(string); ok {
							tool.Function.Name = name
						}
						if desc, ok := fn["description"].(string); ok {
							tool.Function.Description = desc
						}
						if params, ok := fn["parameters"]; ok {
							tool.Function.Parameters = params
						}
						tools = append(tools, tool)
					}
				}
			}
		}
	}

	// Convert tool_choice
	var toolChoice any
	if len(req.ToolChoice) > 0 {
		var tcRaw any
		if err := common.Unmarshal(req.ToolChoice, &tcRaw); err == nil {
			switch v := tcRaw.(type) {
			case string:
				toolChoice = v
			case map[string]any:
				if ttype, _ := v["type"].(string); ttype == "function" {
					if name, _ := v["name"].(string); name != "" {
						toolChoice = map[string]any{
							"type": "function",
							"function": map[string]any{
								"name": name,
							},
						}
					} else {
						toolChoice = v
					}
				} else {
					toolChoice = v
				}
			default:
				toolChoice = v
			}
		}
	}

	out := &dto.GeneralOpenAIRequest{
		Model:       req.Model,
		Messages:    messages,
		Stream:      req.Stream,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		User:        req.User,
		ToolChoice:  toolChoice,
	}
	if len(tools) > 0 {
		out.Tools = tools
	}
	if req.MaxOutputTokens != nil {
		maxTokens := *req.MaxOutputTokens
		out.MaxTokens = &maxTokens
	}
	if req.Reasoning != nil && req.Reasoning.Effort != "" {
		out.ReasoningEffort = req.Reasoning.Effort
	}
	if len(req.Store) > 0 {
		out.Store = req.Store
	}
	if len(req.Metadata) > 0 {
		out.Metadata = req.Metadata
	}

	return out, nil
}

func convertResponsesInputToMessages(input json.RawMessage) ([]dto.Message, error) {
	if len(input) == 0 {
		return nil, nil
	}

	jsonType := common.GetJsonType(input)

	// Simple string input → single user message
	if jsonType == "string" {
		var str string
		_ = common.Unmarshal(input, &str)
		return []dto.Message{{Role: "user", Content: str}}, nil
	}

	// Must be array
	if jsonType != "array" {
		return nil, fmt.Errorf("unsupported input type: %s", jsonType)
	}

	var items []json.RawMessage
	if err := common.Unmarshal(input, &items); err != nil {
		return nil, fmt.Errorf("failed to parse input array: %w", err)
	}

	var messages []dto.Message
	var pendingToolCalls []dto.ToolCallResponse

	flushToolCalls := func() {
		if len(pendingToolCalls) == 0 {
			return
		}
		tcJSON, _ := common.Marshal(pendingToolCalls)
		messages = append(messages, dto.Message{
			Role:      "assistant",
			Content:   "",
			ToolCalls: tcJSON,
		})
		pendingToolCalls = nil
	}

	for _, item := range items {
		var itemMap map[string]json.RawMessage
		if err := common.Unmarshal(item, &itemMap); err != nil {
			continue
		}

		itemType := getJsonStringField(itemMap, "type")
		role := getJsonStringField(itemMap, "role")

		switch {
		case itemType == "function_call":
			callID := getJsonStringField(itemMap, "call_id")
			name := getJsonStringField(itemMap, "name")
			arguments := getJsonStringField(itemMap, "arguments")
			pendingToolCalls = append(pendingToolCalls, dto.ToolCallResponse{
				ID:   callID,
				Type: "function",
				Function: dto.FunctionResponse{
					Name:      name,
					Arguments: arguments,
				},
			})

		case itemType == "function_call_output":
			// Flush any pending tool calls into an assistant message first
			flushToolCalls()
			callID := getJsonStringField(itemMap, "call_id")
			output := getJsonStringField(itemMap, "output")
			messages = append(messages, dto.Message{
				Role:       "tool",
				Content:    output,
				ToolCallId: callID,
			})

		case role == "user" || role == "assistant":
			// Flush any pending tool calls into the previous assistant message
			flushToolCalls()

			contentRaw, hasContent := itemMap["content"]
			if !hasContent {
				messages = append(messages, dto.Message{Role: role, Content: ""})
				continue
			}

			// String content
			if common.GetJsonType(contentRaw) == "string" {
				var str string
				_ = common.Unmarshal(contentRaw, &str)
				messages = append(messages, dto.Message{Role: role, Content: str})
				continue
			}

			// Array content — map input_text/output_text → text parts, input_image → image_url
			if common.GetJsonType(contentRaw) == "array" {
				var parts []map[string]any
				if err := common.Unmarshal(contentRaw, &parts); err != nil {
					messages = append(messages, dto.Message{Role: role, Content: ""})
					continue
				}
				mediaParts := make([]dto.MediaContent, 0, len(parts))
				for _, part := range parts {
					ptype, _ := part["type"].(string)
					switch ptype {
					case "input_text", "output_text":
						text, _ := part["text"].(string)
						mediaParts = append(mediaParts, dto.MediaContent{
							Type: dto.ContentTypeText,
							Text: text,
						})
					case "input_image":
						imageUrl, _ := part["image_url"].(string)
						mediaParts = append(mediaParts, dto.MediaContent{
							Type: dto.ContentTypeImageURL,
							ImageUrl: dto.MessageImageUrl{
								Url: imageUrl,
							},
						})
					default:
						// Skip unknown types
					}
				}
				if len(mediaParts) > 0 {
					msg := dto.Message{Role: role}
					msg.SetMediaContent(mediaParts)
					messages = append(messages, msg)
				} else {
					messages = append(messages, dto.Message{Role: role, Content: ""})
				}
			}

		default:
			// Skip unknown item types
		}
	}

	// Flush remaining tool calls
	flushToolCalls()

	return messages, nil
}

func getJsonStringField(m map[string]json.RawMessage, key string) string {
	raw, ok := m[key]
	if !ok {
		return ""
	}
	var s string
	_ = common.Unmarshal(raw, &s)
	return s
}
