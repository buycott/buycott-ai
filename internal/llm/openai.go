package llm

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

type OpenAIProvider struct {
	client *openai.Client
	model  string
}

func NewOpenAIProvider(apiKey, model string) *OpenAIProvider {
	return &OpenAIProvider{
		client: openai.NewClient(option.WithAPIKey(apiKey)),
		model:  model,
	}
}

func (p *OpenAIProvider) Name() string {
	return "openai/" + p.model
}

func (p *OpenAIProvider) buildParams(req CompletionRequest) openai.ChatCompletionNewParams {
	maxTokens := int64(req.MaxTokens)
	if maxTokens == 0 {
		maxTokens = 8096
	}
	var messages []openai.ChatCompletionMessageParamUnion
	for _, m := range req.Messages {
		switch m.Role {
		case "system":
			messages = append(messages, openai.SystemMessage(m.Content))
		case "user":
			messages = append(messages, openai.UserMessage(m.Content))
		case "assistant":
			messages = append(messages, openai.AssistantMessage(m.Content))
		}
	}
	params := openai.ChatCompletionNewParams{
		Model:     openai.F(openai.ChatModel(p.model)),
		Messages:  openai.F(messages),
		MaxTokens: openai.F(maxTokens),
	}

	if len(req.Tools) > 0 {
		var tools []openai.ChatCompletionToolParam
		for _, t := range req.Tools {
			tools = append(tools, openai.ChatCompletionToolParam{
				Type: openai.F(openai.ChatCompletionToolTypeFunction),
				Function: openai.F(openai.FunctionDefinitionParam{
					Name:        openai.F(t.Name),
					Description: openai.F(t.Description),
					Parameters:  openai.F(openai.FunctionParameters(t.Parameters)),
				}),
			})
		}
		params.Tools = openai.F(tools)
		if req.ToolName != "" {
			params.ToolChoice = openai.F[openai.ChatCompletionToolChoiceOptionUnionParam](
				openai.ChatCompletionNamedToolChoiceParam{
					Type: openai.F(openai.ChatCompletionNamedToolChoiceTypeFunction),
					Function: openai.F(openai.ChatCompletionNamedToolChoiceFunctionParam{
						Name: openai.F(req.ToolName),
					}),
				},
			)
		} else {
			params.ToolChoice = openai.F[openai.ChatCompletionToolChoiceOptionUnionParam](
				openai.ChatCompletionToolChoiceOptionBehaviorRequired,
			)
		}
	}

	return params
}

func (p *OpenAIProvider) Stream(ctx context.Context, req CompletionRequest, ch chan<- string) error {
	stream := p.client.Chat.Completions.NewStreaming(ctx, p.buildParams(req))
	defer stream.Close()

	for stream.Next() {
		chunk := stream.Current()
		if len(chunk.Choices) > 0 {
			if text := chunk.Choices[0].Delta.Content; text != "" {
				select {
				case ch <- text:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}
	}
	return stream.Err()
}

func (p *OpenAIProvider) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	resp, err := p.client.Chat.Completions.New(ctx, p.buildParams(req))
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("openai completion: %w", err)
	}

	if len(resp.Choices) == 0 {
		return CompletionResponse{}, fmt.Errorf("openai: no choices in response")
	}

	msg := resp.Choices[0].Message

	// Tool call response.
	if len(msg.ToolCalls) > 0 {
		tc := msg.ToolCalls[0]
		return CompletionResponse{
			ToolName:     tc.Function.Name,
			ToolInput:    json.RawMessage(tc.Function.Arguments),
			InputTokens:  int(resp.Usage.PromptTokens),
			OutputTokens: int(resp.Usage.CompletionTokens),
		}, nil
	}

	return CompletionResponse{
		Content:      msg.Content,
		InputTokens:  int(resp.Usage.PromptTokens),
		OutputTokens: int(resp.Usage.CompletionTokens),
	}, nil
}
