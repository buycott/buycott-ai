package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Project   ProjectConfig          `yaml:"project"`
	Roles     map[string]RoleConfig  `yaml:"roles"`
	Execution ExecutionConfig        `yaml:"execution"`
	API       APIConfig              `yaml:"api"`
	Dashboard DashboardConfig        `yaml:"dashboard"`
	APIKeys   APIKeysConfig          `yaml:"api_keys"`
	Webhooks  []WebhookConfig        `yaml:"webhooks"`
}

type DashboardConfig struct {
	Port int `yaml:"port"`
}

type APIConfig struct {
	Port int `yaml:"port"`
}

type ProjectConfig struct {
	Name          string `yaml:"name"`
	ArtifactsPath string `yaml:"artifacts_path"`
}

// ScanCommand describes one static-analysis or CVE-scan tool to run before the
// security reviewer LLM is invoked. The command runs inside the specified Docker
// image with the artifacts volume mounted at /artifacts.
type ScanCommand struct {
	Image string `yaml:"image"`
	Cmd   string `yaml:"cmd"`
}

type RoleConfig struct {
	Provider         string        `yaml:"provider"`
	Model            string        `yaml:"model"`
	SystemPrompt     string        `yaml:"system_prompt"`
	SystemPromptFile string        `yaml:"system_prompt_file"`
	ScanCommands     []ScanCommand `yaml:"scan_commands,omitempty"`
}

type ExecutionConfig struct {
	MaxRetries           int           `yaml:"max_retries"`
	TaskTimeout          time.Duration `yaml:"task_timeout"`
	DockerSocket         string        `yaml:"docker_socket"`
	PromptsDir           string        `yaml:"prompts_dir"`
	ReleaseCheckInterval int           `yaml:"release_check_interval"`
	MinQueueDepth        int           `yaml:"min_queue_depth"`
	MaxHistoryMessages   int           `yaml:"max_history_messages"`
}

// WebhookConfig fires an HTTP POST to URL for each matching event type.
// Events is a list of event type patterns; use "*" to match all events.
type WebhookConfig struct {
	URL    string   `yaml:"url"`
	Events []string `yaml:"events"`
}

type APIKeysConfig struct {
	Anthropic string `yaml:"anthropic"`
	OpenAI    string `yaml:"openai"`
	Gemini    string `yaml:"gemini"`
	// ClaudeCode is an optional long-lived OAuth token (from `claude setup-token`)
	// used by the "claude-code" provider to drive the CLI on a Claude
	// subscription. Optional: if empty, the CLI falls back to an existing login.
	ClaudeCode string `yaml:"claude_code"`
}

var envVarRe = regexp.MustCompile(`\$\{([^}]+)\}`)

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	expanded := expandEnvVars(string(data))

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	cfg.applyDefaults()
	return &cfg, nil
}

// expandEnvVars replaces ${VAR} references with the corresponding environment
// variable. An unset (or empty) variable expands to an empty string rather than
// being left as the literal "${VAR}" — otherwise the placeholder would leak
// through as a value (e.g. an API key literally equal to "${ANTHROPIC_API_KEY}"),
// passing non-empty checks and producing a confusing auth failure downstream
// instead of a clear "not set" error.
func expandEnvVars(s string) string {
	return envVarRe.ReplaceAllStringFunc(s, func(match string) string {
		key := strings.TrimSuffix(strings.TrimPrefix(match, "${"), "}")
		return os.Getenv(key)
	})
}

func (c *Config) validate() error {
	if c.Project.ArtifactsPath == "" {
		return fmt.Errorf("project.artifacts_path is required")
	}
	if _, ok := c.Roles["pm"]; !ok {
		return fmt.Errorf("roles.pm is required")
	}
	for name, role := range c.Roles {
		if role.Provider == "" {
			return fmt.Errorf("roles.%s.provider is required", name)
		}
		if role.Model == "" {
			return fmt.Errorf("roles.%s.model is required", name)
		}
		switch role.Provider {
		case "anthropic", "openai", "gemini":
		default:
			return fmt.Errorf("roles.%s.provider %q is not supported (use anthropic, openai, or gemini)", name, role.Provider)
		}
	}
	return nil
}

func (c *Config) applyDefaults() {
	if c.Execution.MaxRetries == 0 {
		c.Execution.MaxRetries = 10
	}
	if c.Execution.TaskTimeout == 0 {
		c.Execution.TaskTimeout = 5 * time.Minute
	}
	if c.Execution.DockerSocket == "" {
		c.Execution.DockerSocket = "/var/run/docker.sock"
	}
	if c.Execution.PromptsDir == "" {
		c.Execution.PromptsDir = "/etc/buycott/prompts"
	}
	if c.Execution.ReleaseCheckInterval == 0 {
		c.Execution.ReleaseCheckInterval = 10
	}
	if c.Execution.MinQueueDepth == 0 {
		c.Execution.MinQueueDepth = 5
	}
	if c.Execution.MaxHistoryMessages == 0 {
		c.Execution.MaxHistoryMessages = 20
	}
	if c.API.Port == 0 {
		c.API.Port = 8080
	}
	if c.Dashboard.Port == 0 {
		c.Dashboard.Port = 8000
	}
}

func Default() *Config {
	return &Config{
		Project: ProjectConfig{
			ArtifactsPath: "/artifacts",
		},
		Roles: map[string]RoleConfig{
			"pm": {
				Provider: "anthropic",
				Model:    "claude-opus-4-8",
			},
			"backend": {
				Provider: "anthropic",
				Model:    "claude-sonnet-4-6",
			},
			"frontend": {
				Provider: "anthropic",
				Model:    "claude-sonnet-4-6",
			},
			"copywriter": {
				Provider: "anthropic",
				Model:    "claude-haiku-4-5",
			},
		},
		Execution: ExecutionConfig{
			MaxRetries:   10,
			TaskTimeout:  5 * time.Minute,
			DockerSocket: "/var/run/docker.sock",
		},
	}
}
