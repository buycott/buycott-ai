package llm

import (
	"fmt"

	"buycott/internal/config"
)

func NewProvider(roleCfg config.RoleConfig, apiKeys config.APIKeysConfig) (Provider, error) {
	switch roleCfg.Provider {
	case "anthropic":
		if apiKeys.Anthropic == "" {
			return nil, fmt.Errorf("ANTHROPIC_API_KEY not set")
		}
		return NewAnthropicProvider(apiKeys.Anthropic, roleCfg.Model), nil
	case "openai":
		if apiKeys.OpenAI == "" {
			return nil, fmt.Errorf("OPENAI_API_KEY not set")
		}
		return NewOpenAIProvider(apiKeys.OpenAI, roleCfg.Model), nil
	case "gemini":
		if apiKeys.Gemini == "" {
			return nil, fmt.Errorf("GEMINI_API_KEY not set")
		}
		return NewGeminiProvider(apiKeys.Gemini, roleCfg.Model), nil
	case "claude-code":
		// No API key required: the `claude` CLI authenticates via the Claude
		// subscription (OAuth). apiKeys.ClaudeCode is an optional setup-token.
		return NewClaudeCodeProvider(apiKeys.ClaudeCode, roleCfg.Model), nil
	case "codex":
		// No API key required: the `codex` CLI authenticates via a ChatGPT
		// subscription (`codex login`).
		return NewCodexProvider(roleCfg.Model), nil
	case "gemini-cli":
		// No API key required: the `gemini` CLI authenticates via a Google
		// account login.
		return NewGeminiCLIProvider(roleCfg.Model), nil
	default:
		return nil, fmt.Errorf("unsupported provider: %s", roleCfg.Provider)
	}
}
