package roles

import (
	"fmt"
	"os"
	"path/filepath"

	"buycott/internal/config"
)

// LoadPrompt resolves the system prompt for a role using this precedence:
//  1. Inline system_prompt in role config
//  2. system_prompt_file path in role config
//  3. {prompts_dir}/{role_name}.md  (default convention)
func LoadPrompt(roleName string, roleCfg config.RoleConfig, promptsDir string) (string, error) {
	if roleCfg.SystemPrompt != "" {
		return roleCfg.SystemPrompt, nil
	}

	path := roleCfg.SystemPromptFile
	if path == "" {
		path = filepath.Join(promptsDir, roleName+".md")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf(
				"no prompt found for role %q: file %q does not exist "+
					"(set system_prompt or system_prompt_file in config, or add the file at that path)",
				roleName, path,
			)
		}
		return "", fmt.Errorf("reading prompt for role %q from %q: %w", roleName, path, err)
	}

	return string(data), nil
}
