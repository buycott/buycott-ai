package llm

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

type GeminiProvider struct {
	apiKey string
	model  string
}

func NewGeminiProvider(apiKey, model string) *GeminiProvider {
	return &GeminiProvider{apiKey: apiKey, model: model}
}

func (p *GeminiProvider) Name() string { return "gemini/" + p.model }

// buildSession constructs a chat session and returns the model, session, and
// the last user message to be sent (not yet in history).
func (p *GeminiProvider) buildSession(client *genai.Client, req CompletionRequest) (*genai.GenerativeModel, *genai.ChatSession, string) {
	model := client.GenerativeModel(p.model)
	if req.MaxTokens > 0 {
		max := int32(req.MaxTokens)
		model.MaxOutputTokens = &max
	}

	if len(req.Tools) > 0 {
		var decls []*genai.FunctionDeclaration
		for _, t := range req.Tools {
			decls = append(decls, &genai.FunctionDeclaration{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  jsonSchemaToGemini(t.Parameters),
			})
		}
		model.Tools = []*genai.Tool{{FunctionDeclarations: decls}}
		model.ToolConfig = &genai.ToolConfig{
			FunctionCallingConfig: &genai.FunctionCallingConfig{
				Mode: genai.FunctionCallingAny,
			},
		}
	}

	var lastUser string
	var chatHistory []*genai.Content

	for i, m := range req.Messages {
		switch m.Role {
		case "system":
			if model.SystemInstruction == nil {
				model.SystemInstruction = &genai.Content{}
			}
			model.SystemInstruction.Parts = append(model.SystemInstruction.Parts, genai.Text(m.Content))
		case "user":
			isLast := true
			for _, later := range req.Messages[i+1:] {
				if later.Role == "user" {
					isLast = false
					break
				}
			}
			if isLast {
				lastUser = m.Content
			} else {
				chatHistory = append(chatHistory, &genai.Content{
					Role:  "user",
					Parts: []genai.Part{genai.Text(m.Content)},
				})
			}
		case "assistant":
			chatHistory = append(chatHistory, &genai.Content{
				Role:  "model",
				Parts: []genai.Part{genai.Text(m.Content)},
			})
		}
	}

	session := model.StartChat()
	session.History = chatHistory
	return model, session, lastUser
}

func (p *GeminiProvider) Stream(ctx context.Context, req CompletionRequest, ch chan<- string) error {
	client, err := genai.NewClient(ctx, option.WithAPIKey(p.apiKey))
	if err != nil {
		return fmt.Errorf("gemini client: %w", err)
	}
	defer client.Close()

	_, session, lastUser := p.buildSession(client, req)

	iter := session.SendMessageStream(ctx, genai.Text(lastUser))
	for {
		resp, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return fmt.Errorf("gemini stream: %w", err)
		}
		for _, cand := range resp.Candidates {
			if cand.Content == nil {
				continue
			}
			for _, part := range cand.Content.Parts {
				if t, ok := part.(genai.Text); ok {
					select {
					case ch <- string(t):
					case <-ctx.Done():
						return ctx.Err()
					}
				}
			}
		}
	}
	return nil
}

func (p *GeminiProvider) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	client, err := genai.NewClient(ctx, option.WithAPIKey(p.apiKey))
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("gemini client: %w", err)
	}
	defer client.Close()

	_, session, lastUser := p.buildSession(client, req)

	resp, err := session.SendMessage(ctx, genai.Text(lastUser))
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("gemini send: %w", err)
	}
	if len(resp.Candidates) == 0 {
		return CompletionResponse{}, fmt.Errorf("gemini: no candidates")
	}

	var content string
	var toolName string
	var toolInput json.RawMessage

	for _, part := range resp.Candidates[0].Content.Parts {
		switch v := part.(type) {
		case genai.Text:
			content += string(v)
		case genai.FunctionCall:
			toolName = v.Name
			if b, err := json.Marshal(v.Args); err == nil {
				toolInput = b
			}
		}
	}

	var inputTokens, outputTokens int
	if resp.UsageMetadata != nil {
		inputTokens = int(resp.UsageMetadata.PromptTokenCount)
		outputTokens = int(resp.UsageMetadata.CandidatesTokenCount)
	}

	return CompletionResponse{
		Content:      content,
		ToolName:     toolName,
		ToolInput:    toolInput,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
	}, nil
}

// jsonSchemaToGemini converts a JSON Schema map to a genai.Schema.
func jsonSchemaToGemini(schema map[string]any) *genai.Schema {
	if schema == nil {
		return nil
	}
	s := &genai.Schema{}
	if t, ok := schema["type"].(string); ok {
		switch t {
		case "string":
			s.Type = genai.TypeString
		case "integer":
			s.Type = genai.TypeInteger
		case "number":
			s.Type = genai.TypeNumber
		case "boolean":
			s.Type = genai.TypeBoolean
		case "array":
			s.Type = genai.TypeArray
		case "object":
			s.Type = genai.TypeObject
		}
	}
	if desc, ok := schema["description"].(string); ok {
		s.Description = desc
	}
	if items, ok := schema["items"].(map[string]any); ok {
		s.Items = jsonSchemaToGemini(items)
	}
	if props, ok := schema["properties"].(map[string]any); ok {
		s.Properties = make(map[string]*genai.Schema)
		for k, v := range props {
			if pm, ok := v.(map[string]any); ok {
				s.Properties[k] = jsonSchemaToGemini(pm)
			}
		}
	}
	if req, ok := schema["required"].([]any); ok {
		for _, r := range req {
			if rs, ok := r.(string); ok {
				s.Required = append(s.Required, rs)
			}
		}
	}
	return s
}
