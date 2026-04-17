package zhipu_4v

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/openaicompat"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

func zhipuChatToResponsesHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}

	defer service.CloseResponseBodyGracefully(resp)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}

	var chatResp dto.OpenAITextResponse
	if err := common.Unmarshal(body, &chatResp); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	if oaiError := chatResp.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
		return nil, types.WithOpenAIError(*oaiError, resp.StatusCode)
	}

	responsesResp, err := openaicompat.ChatCompletionsResponseToResponsesResponse(&chatResp, info.UpstreamModelName)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	responseBody, err := common.Marshal(responsesResp)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeJsonMarshalFailed, http.StatusInternalServerError)
	}

	service.IOCopyBytesGracefully(c, resp, responseBody)

	usage := &dto.Usage{}
	if responsesResp.Usage != nil {
		usage = responsesResp.Usage
	}
	if usage.TotalTokens == 0 {
		text := openaicompat.ExtractOutputTextFromResponses(responsesResp)
		usage = service.ResponseText2Usage(c, text, info.UpstreamModelName, info.GetEstimatePromptTokens())
	}

	return usage, nil
}

func zhipuChatStreamToResponsesStream(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse)
	}

	defer service.CloseResponseBodyGracefully(resp)

	helper.SetEventStreamHeaders(c)

	responseID := fmt.Sprintf("resp_%d", time.Now().UnixNano())
	model := info.UpstreamModelName
	createdAt := int(time.Now().Unix())

	var (
		usage         = &dto.Usage{}
		outputText    strings.Builder
		usageText     strings.Builder
		streamErr     *types.NewAPIError
		sentCreated   bool
		sentMsgAdded  bool
		sentPartAdded bool

		toolCallsByIndex = make(map[int]*dto.ToolCallResponse)
		toolCallOrder    []int
	)

	sendEvent := func(eventType string, data any) bool {
		// Marshal data, then inject "type" field into the JSON
		jsonData, err := common.Marshal(data)
		if err != nil {
			streamErr = types.NewOpenAIError(err, types.ErrorCodeJsonMarshalFailed, http.StatusInternalServerError)
			return false
		}
		// Unmarshal to map to set the type field
		var m map[string]any
		if err := common.Unmarshal(jsonData, &m); err == nil {
			m["type"] = eventType
			jsonData, err = common.Marshal(m)
			if err != nil {
				streamErr = types.NewOpenAIError(err, types.ErrorCodeJsonMarshalFailed, http.StatusInternalServerError)
				return false
			}
		}
		helper.ResponseChunkData(c, dto.ResponsesStreamResponse{Type: eventType}, string(jsonData))
		return true
	}

	makeResponseObj := func(status string) *dto.OpenAIResponsesResponse {
		return &dto.OpenAIResponsesResponse{
			ID:        responseID,
			Object:    "response",
			CreatedAt: createdAt,
			Model:     model,
			Status:    json.RawMessage(fmt.Sprintf(`"%s"`, status)),
			Output:    []dto.ResponsesOutput{},
		}
	}

	sendCreatedIfNeeded := func() bool {
		if sentCreated {
			return true
		}
		if !sendEvent("response.created", makeResponseObj("in_progress")) {
			return false
		}
		if !sendEvent("response.in_progress", makeResponseObj("in_progress")) {
			return false
		}
		sentCreated = true
		return true
	}

	sendMessageAddedIfNeeded := func() bool {
		if sentMsgAdded {
			return true
		}
		if !sendCreatedIfNeeded() {
			return false
		}
		msgItem := dto.ResponsesOutput{
			Type:   "message",
			ID:     fmt.Sprintf("msg_%d", time.Now().UnixNano()),
			Status: "in_progress",
			Role:   "assistant",
			Content: []dto.ResponsesOutputContent{
				{Type: "output_text", Text: ""},
			},
		}
		if !sendEvent("response.output_item.added", dto.ResponsesStreamResponse{
			Item: &msgItem,
		}) {
			return false
		}
		if !sendEvent("response.content_part.added", dto.ResponsesStreamResponse{
			Part: &dto.ResponsesReasoningSummaryPart{Type: "output_text", Text: ""},
		}) {
			return false
		}
		sentMsgAdded = true
		sentPartAdded = true
		return true
	}

	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
		if streamErr != nil {
			sr.Stop(streamErr)
			return
		}

		if data == "[DONE]" {
			return
		}

		var chatChunk dto.ChatCompletionsStreamResponse
		if err := common.UnmarshalJsonStr(data, &chatChunk); err != nil {
			sr.Error(err)
			return
		}

		if chatChunk.Model != "" {
			model = chatChunk.Model
		}

		if len(chatChunk.Choices) == 0 {
			return
		}

		choice := chatChunk.Choices[0]
		delta := choice.Delta

		if delta.Role == "assistant" && !sentMsgAdded {
			if !sendMessageAddedIfNeeded() {
				sr.Stop(streamErr)
				return
			}
		}

		// Text content delta
		if delta.Content != nil && *delta.Content != "" {
			if !sendMessageAddedIfNeeded() {
				sr.Stop(streamErr)
				return
			}
			text := *delta.Content
			outputText.WriteString(text)
			usageText.WriteString(text)
			if !sendEvent("response.output_text.delta", dto.ResponsesStreamResponse{
				Delta: text,
			}) {
				sr.Stop(streamErr)
				return
			}
		}

		// Tool calls delta
		for _, tc := range delta.ToolCalls {
			if !sendCreatedIfNeeded() {
				sr.Stop(streamErr)
				return
			}

			idx := 0
			if tc.Index != nil {
				idx = *tc.Index
			}

			existing, exists := toolCallsByIndex[idx]
			if !exists {
				callID := tc.ID
				if callID == "" {
					callID = fmt.Sprintf("call_%d", idx)
				}
				newTC := &dto.ToolCallResponse{
					ID:       callID,
					Type:     "function",
					Function: dto.FunctionResponse{Name: tc.Function.Name, Arguments: tc.Function.Arguments},
				}
				toolCallsByIndex[idx] = newTC
				toolCallOrder = append(toolCallOrder, idx)

				fcItem := dto.ResponsesOutput{
					Type:   "function_call",
					ID:     fmt.Sprintf("fc_%s", callID),
					CallId: callID,
					Name:   tc.Function.Name,
				}
				if !sendEvent("response.output_item.added", dto.ResponsesStreamResponse{
					Item: &fcItem,
				}) {
					sr.Stop(streamErr)
					return
				}

				if tc.Function.Arguments != "" {
					usageText.WriteString(tc.Function.Arguments)
					if !sendEvent("response.function_call_arguments.delta", dto.ResponsesStreamResponse{
						Delta: tc.Function.Arguments,
					}) {
						sr.Stop(streamErr)
						return
					}
				}
			} else {
				if tc.Function.Name != "" && existing.Function.Name == "" {
					existing.Function.Name = tc.Function.Name
				}
				if tc.Function.Arguments != "" {
					existing.Function.Arguments += tc.Function.Arguments
					usageText.WriteString(tc.Function.Arguments)
					if !sendEvent("response.function_call_arguments.delta", dto.ResponsesStreamResponse{
						Delta: tc.Function.Arguments,
					}) {
						sr.Stop(streamErr)
						return
					}
				}
			}
		}

		// Finish
		if choice.FinishReason != nil {
			for _, idx := range toolCallOrder {
				tc := toolCallsByIndex[idx]
				if !sendEvent("response.function_call_arguments.done", map[string]any{
					"type":      "response.function_call_arguments.done",
					"item_id":   fmt.Sprintf("fc_%s", tc.ID),
					"call_id":   tc.ID,
					"arguments": tc.Function.Arguments,
				}) {
					sr.Stop(streamErr)
					return
				}
			}

			if sentPartAdded {
				text := outputText.String()
				if !sendEvent("response.output_text.done", dto.ResponsesStreamResponse{
					Delta: text,
				}) {
					sr.Stop(streamErr)
					return
				}
				if !sendEvent("response.content_part.done", dto.ResponsesStreamResponse{
					Part: &dto.ResponsesReasoningSummaryPart{Type: "output_text", Text: text},
				}) {
					sr.Stop(streamErr)
					return
				}
			}

			if sentMsgAdded {
				msgItem := dto.ResponsesOutput{
					Type:   "message",
					ID:     fmt.Sprintf("msg_%d", time.Now().UnixNano()),
					Status: "completed",
					Role:   "assistant",
					Content: []dto.ResponsesOutputContent{
						{Type: "output_text", Text: outputText.String()},
					},
				}
				if !sendEvent("response.output_item.done", dto.ResponsesStreamResponse{
					Item: &msgItem,
				}) {
					sr.Stop(streamErr)
					return
				}
			}

			if usage.TotalTokens == 0 {
				usage.PromptTokens = info.GetEstimatePromptTokens()
				usage.CompletionTokens = service.CountTextToken(usageText.String(), info.UpstreamModelName)
				usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
			}

			completedResp := makeResponseObj("completed")
			completedResp.Usage = usage

			if sentMsgAdded {
				completedResp.Output = append(completedResp.Output, dto.ResponsesOutput{
					Type:   "message",
					ID:     fmt.Sprintf("msg_%d", time.Now().UnixNano()),
					Status: "completed",
					Role:   "assistant",
					Content: []dto.ResponsesOutputContent{
						{Type: "output_text", Text: outputText.String()},
					},
				})
			}
			for _, idx := range toolCallOrder {
				tc := toolCallsByIndex[idx]
				completedResp.Output = append(completedResp.Output, dto.ResponsesOutput{
					Type:      "function_call",
					ID:        fmt.Sprintf("fc_%s", tc.ID),
					CallId:    tc.ID,
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
					Status:    "completed",
				})
			}

			if !sendEvent("response.completed", dto.ResponsesStreamResponse{
				Response: completedResp,
			}) {
				sr.Stop(streamErr)
				return
			}
		}
	})

	if streamErr != nil {
		return nil, streamErr
	}

	if !sentCreated {
		sendCreatedIfNeeded()
	}

	return usage, nil
}
