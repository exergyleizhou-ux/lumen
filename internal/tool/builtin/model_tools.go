// model_tools.go — Model listing and switching tools for the agent.
package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"lumen/internal/config"
	"lumen/internal/tool"
)

func init() {
	tool.RegisterBuiltin(&ModelListTool{})
	tool.RegisterBuiltin(&ModelPresetTool{})
}

type ModelListTool struct{}

func (t *ModelListTool) Name() string   { return "model_list" }
func (t *ModelListTool) ReadOnly() bool { return true }
func (t *ModelListTool) Description() string {
	return "List all supported AI models across all providers (OpenAI, Anthropic, DeepSeek, Grok, Kimi, Qwen, GLM, Mimo, Gemini). Shows model name, provider, and API endpoint."
}
func (t *ModelListTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"filter":{"type":"string","description":"Optional: filter by provider name (e.g. 'openai', 'anthropic', 'deepseek')"}}}`)
}
func (t *ModelListTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct{ Filter string }
	json.Unmarshal(args, &p)

	presets := config.ModelPresets()
	var sb strings.Builder

	count := 0
	providers := map[string]int{}
	for _, pr := range presets {
		if p.Filter != "" && pr.Provider != p.Filter {
			continue
		}
		providers[pr.Provider]++
		count++
	}

	if p.Filter == "" {
		fmt.Fprintf(&sb, "Supported models across %d providers:\n\n", len(providers))
	} else {
		fmt.Fprintf(&sb, "%d model(s) for provider %q:\n\n", count, p.Filter)
	}

	lastProvider := ""
	for _, pr := range presets {
		if p.Filter != "" && pr.Provider != p.Filter {
			continue
		}
		if pr.Provider != lastProvider {
			if lastProvider != "" {
				sb.WriteByte('\n')
			}
			fmt.Fprintf(&sb, "  [%s]\n", pr.Provider)
			lastProvider = pr.Provider
		}
		fmt.Fprintf(&sb, "    %-30s → %s\n", pr.Name, pr.Model)
	}

	sb.WriteString("\nUse /model <name> to switch models at runtime.\n")
	return sb.String(), nil
}

type ModelPresetTool struct{}

func (t *ModelPresetTool) Name() string   { return "model_preset" }
func (t *ModelPresetTool) ReadOnly() bool { return true }
func (t *ModelPresetTool) Description() string {
	return "Show how to configure a specific model in lumen.toml. Shows the provider kind, base URL, and example TOML config."
}
func (t *ModelPresetTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","description":"Model preset name (e.g. 'gpt-4o', 'claude-sonnet-4-20250514', 'deepseek-chat')"}},"required":["name"]}`)
}
func (t *ModelPresetTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct{ Name string }
	if err := json.Unmarshal(args, &p); err != nil {
		return "", err
	}

	preset := config.FindPreset(p.Name)
	if preset == nil {
		return fmt.Sprintf("Model %q not found. Use model_list to see available models.", p.Name), nil
	}

	return fmt.Sprintf(`Model: %s
Provider: %s
Kind: %s
API Model: %s
Base URL: %s

Add to lumen.toml:

[[providers]]
name = "%s"
kind = "%s"
base_url = "%s"
model = "%s"
api_key = "your-key-here"
`,
		preset.Name, preset.Provider, preset.Kind, preset.Model, preset.BaseURL,
		preset.Name, preset.Kind, preset.BaseURL, preset.Model,
	), nil
}
