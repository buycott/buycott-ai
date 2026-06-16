package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

type AnthropicProvider struct {
	client *anthropic.Client
	model  string
}

func NewAnthropicProvider(apiKey, model string) *AnthropicProvider {
	return &AnthropicProvider{
		client: anthropic.NewClient(option.WithAPIKey(apiKey)),
		model:  model,
	}
}

func (p *AnthropicProvider) Name() string {
	return "anthropic/" + p.model
}

func (p *AnthropicProvider) Stream(ctx context.Context, req CompletionRequest, ch chan<- string) error {
	params := p.buildParams(req)
	stream := p.client.Messages.NewStreaming(ctx, params)
	defer stream.Close()

	for stream.Next() {
		event := stream.Current()
		if delta, ok := event.AsUnion().(anthropic.ContentBlockDeltaEvent); ok {
			if text, ok := delta.Delta.AsUnion().(anthropic.TextDelta); ok {
				select {
				case ch <- text.Text:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}
	}
	return stream.Err()
}

// nonEmptyText guards against the Anthropic API rejecting requests with
// "text content blocks must be non-empty". A blank message (e.g. an agent turn
// that produced only files and no narrative) is replaced with a placeholder.
func nonEmptyText(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(no content)"
	}
	return s
}

func (p *AnthropicProvider) buildParams(req CompletionRequest) anthropic.MessageNewParams {
	maxTokens := int64(req.MaxTokens)
	if maxTokens == 0 {
		maxTokens = 8096
	}
	params := anthropic.MessageNewParams{
		Model:     anthropic.F(anthropic.Model(p.model)),
		MaxTokens: anthropic.F(maxTokens),
	}
	var messages []anthropic.MessageParam
	for _, m := range req.Messages {
		switch m.Role {
		case "system":
			params.System = anthropic.F([]anthropic.TextBlockParam{
				anthropic.NewTextBlock(nonEmptyText(m.Content)),
			})
		case "user":
			messages = append(messages, anthropic.NewUserMessage(anthropic.NewTextBlock(nonEmptyText(m.Content))))
		case "assistant":
			messages = append(messages, anthropic.NewAssistantMessage(anthropic.NewTextBlock(nonEmptyText(m.Content))))
		}
	}
	params.Messages = anthropic.F(messages)

	if len(req.Tools) > 0 {
		var tools []anthropic.ToolParam
		for _, t := range req.Tools {
			tools = append(tools, anthropic.ToolParam{
				Name:        anthropic.F(t.Name),
				Description: anthropic.F(t.Description),
				InputSchema: anthropic.F[interface{}](t.Parameters),
			})
		}
		params.Tools = anthropic.F(tools)
		// Force use of the named tool (or any provided tool if no name given).
		if req.ToolName != "" {
			params.ToolChoice = anthropic.F[anthropic.ToolChoiceUnionParam](anthropic.ToolChoiceToolParam{
				Type: anthropic.F(anthropic.ToolChoiceToolTypeTool),
				Name: anthropic.F(req.ToolName),
			})
		} else {
			params.ToolChoice = anthropic.F[anthropic.ToolChoiceUnionParam](anthropic.ToolChoiceAnyParam{
				Type: anthropic.F(anthropic.ToolChoiceAnyTypeAny),
			})
		}
	}

	return params
}

func (p *AnthropicProvider) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	params := p.buildParams(req)

	resp, err := p.client.Messages.New(ctx, params)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("anthropic completion: %w", err)
	}

	var content string
	var toolName string
	var toolInput json.RawMessage

	for _, block := range resp.Content {
		switch block.Type {
		case anthropic.ContentBlockTypeText:
			content += block.Text
		case anthropic.ContentBlockTypeToolUse:
			toolName = block.Name
			toolInput = block.Input
		}
	}

	return CompletionResponse{
		Content:      content,
		ToolName:     toolName,
		ToolInput:    toolInput,
		InputTokens:  int(resp.Usage.InputTokens),
		OutputTokens: int(resp.Usage.OutputTokens),
	}, nil
}
