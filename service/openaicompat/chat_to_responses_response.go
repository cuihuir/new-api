package openaicompat

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

func ChatCompletionsResponseToResponsesResponse(chatResp *dto.OpenAITextResponse, requestModel string) (*dto.OpenAIResponsesResponse, error) {
	if chatResp == nil {
		return nil, fmt.Errorf("response is nil")
	}

	resp := &dto.OpenAIResponsesResponse{
		ID:        fmt.Sprintf("resp_%s", extractID(chatResp.Id)),
		Object:    "response",
		CreatedAt: int(time.Now().Unix()),
		Model:     chatResp.Model,
		Status:    json.RawMessage(`"completed"`),
		Output:    []dto.ResponsesOutput{},
	}

	if resp.Model == "" {
		resp.Model = requestModel
	}

	// Convert usage
	resp.Usage = convertChatUsageToResponsesUsage(&chatResp.Usage)

	// Convert choices
	if len(chatResp.Choices) > 0 {
		choice := chatResp.Choices[0]
		msg := choice.Message

		// Build message output
		var textContent string
		if msg.IsStringContent() {
			textContent = msg.StringContent()
		} else if msg.Content != nil {
			if b, err := common.Marshal(msg.Content); err == nil {
				textContent = string(b)
			}
		}

		if textContent != "" || len(msg.ParseToolCalls()) == 0 {
			output := dto.ResponsesOutput{
				Type:   "message",
				ID:     fmt.Sprintf("msg_%s", extractID(chatResp.Id)),
				Status: "completed",
				Role:   "assistant",
				Content: []dto.ResponsesOutputContent{
					{
						Type: "output_text",
						Text: textContent,
					},
				},
			}
			resp.Output = append(resp.Output, output)
		}

		// Convert tool calls
		for _, tc := range msg.ParseToolCalls() {
			resp.Output = append(resp.Output, dto.ResponsesOutput{
				Type:      "function_call",
				ID:        fmt.Sprintf("fc_%s", tc.ID),
				CallId:    tc.ID,
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
				Status:    "completed",
			})
		}
	}

	return resp, nil
}

func convertChatUsageToResponsesUsage(u *dto.Usage) *dto.Usage {
	if u == nil {
		return &dto.Usage{}
	}
	out := &dto.Usage{
		PromptTokens:         u.PromptTokens,
		CompletionTokens:     u.CompletionTokens,
		TotalTokens:          u.TotalTokens,
		InputTokens:          u.InputTokens,
		OutputTokens:         u.OutputTokens,
		PromptTokensDetails:  u.PromptTokensDetails,
		InputTokensDetails:   u.InputTokensDetails,
	}
	if out.InputTokens == 0 && u.PromptTokens != 0 {
		out.InputTokens = u.PromptTokens
	}
	if out.OutputTokens == 0 && u.CompletionTokens != 0 {
		out.OutputTokens = u.CompletionTokens
	}
	if out.TotalTokens == 0 {
		out.TotalTokens = out.PromptTokens + out.CompletionTokens
	}
	return out
}

func extractID(id string) string {
	// Remove common prefixes like "chatcmpl-" to get a clean ID
	if len(id) > 10 {
		return id[len(id)-10:]
	}
	return id
}
