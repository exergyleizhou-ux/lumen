// Package config provides model presets and a registry. This file
// declares every supported model/provider combination so the agent
// can list, switch, and auto-configure.
package config

// ModelPreset describes one pre-configured model.
type ModelPreset struct {
	Name     string `json:"name"`
	Provider string `json:"provider"` // "openai", "anthropic", "gemini"
	Kind     string `json:"kind"`     // "openai", "anthropic", "gemini" (wire kind)
	BaseURL  string `json:"base_url"`
	Model    string `json:"model"` // API model string
}

// ModelPresets returns all supported models.
func ModelPresets() []ModelPreset {
	return []ModelPreset{
		// ── OpenAI ──
		{Name: "gpt-4o", Provider: "openai", Kind: "openai", BaseURL: "https://api.openai.com/v1", Model: "gpt-4o"},
		{Name: "gpt-4o-mini", Provider: "openai", Kind: "openai", BaseURL: "https://api.openai.com/v1", Model: "gpt-4o-mini"},
		{Name: "gpt-4.1", Provider: "openai", Kind: "openai", BaseURL: "https://api.openai.com/v1", Model: "gpt-4.1"},
		{Name: "o3-mini", Provider: "openai", Kind: "openai", BaseURL: "https://api.openai.com/v1", Model: "o3-mini"},
		{Name: "o4-mini", Provider: "openai", Kind: "openai", BaseURL: "https://api.openai.com/v1", Model: "o4-mini"},

		// ── Anthropic / Claude ──
		{Name: "claude-sonnet-4-20250514", Provider: "anthropic", Kind: "anthropic", BaseURL: "https://api.anthropic.com", Model: "claude-sonnet-4-20250514"},
		{Name: "claude-opus-4-20250514", Provider: "anthropic", Kind: "anthropic", BaseURL: "https://api.anthropic.com", Model: "claude-opus-4-20250514"},
		{Name: "claude-3.5-sonnet", Provider: "anthropic", Kind: "anthropic", BaseURL: "https://api.anthropic.com", Model: "claude-3-5-sonnet-20241022"},
		{Name: "claude-3.5-haiku", Provider: "anthropic", Kind: "anthropic", BaseURL: "https://api.anthropic.com", Model: "claude-3-5-haiku-20241022"},

		// ── DeepSeek ──
		{Name: "deepseek-chat", Provider: "deepseek", Kind: "openai", BaseURL: "https://api.deepseek.com/v1", Model: "deepseek-chat"},
		{Name: "deepseek-reasoner", Provider: "deepseek", Kind: "openai", BaseURL: "https://api.deepseek.com/v1", Model: "deepseek-reasoner"},

		// ── Grok / xAI ──
		{Name: "grok-3", Provider: "xai", Kind: "openai", BaseURL: "https://api.x.ai/v1", Model: "grok-3"},
		{Name: "grok-3-mini", Provider: "xai", Kind: "openai", BaseURL: "https://api.x.ai/v1", Model: "grok-3-mini"},

		// ── Kimi / Moonshot ──
		{Name: "kimi-k2", Provider: "moonshot", Kind: "openai", BaseURL: "https://api.moonshot.cn/v1", Model: "kimi-k2-0711-preview"},
		{Name: "moonshot-v1", Provider: "moonshot", Kind: "openai", BaseURL: "https://api.moonshot.cn/v1", Model: "moonshot-v1-8k"},

		// ── Qwen / Tongyi (Alibaba) ──
		{Name: "qwen-max", Provider: "qwen", Kind: "openai", BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Model: "qwen-max"},
		{Name: "qwen-plus", Provider: "qwen", Kind: "openai", BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Model: "qwen-plus"},
		{Name: "qwen-turbo", Provider: "qwen", Kind: "openai", BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Model: "qwen-turbo"},
		{Name: "qwen-coder", Provider: "qwen", Kind: "openai", BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Model: "qwen-coder-plus"},

		// ── Zhipu GLM ──
		{Name: "glm-4", Provider: "zhipu", Kind: "openai", BaseURL: "https://open.bigmodel.cn/api/paas/v4", Model: "glm-4"},
		{Name: "glm-4-flash", Provider: "zhipu", Kind: "openai", BaseURL: "https://open.bigmodel.cn/api/paas/v4", Model: "glm-4-flash"},
		{Name: "glm-4-plus", Provider: "zhipu", Kind: "openai", BaseURL: "https://open.bigmodel.cn/api/paas/v4", Model: "glm-4-plus"},

		// ── Mimo (China) ──
		{Name: "mimo-chat", Provider: "mimo", Kind: "openai", BaseURL: "https://api.mimo.run/v1", Model: "mimo-chat"},

		// ── Google Gemini ──
		{Name: "gemini-2.5-pro", Provider: "google", Kind: "gemini", BaseURL: "https://generativelanguage.googleapis.com", Model: "gemini-2.5-pro-exp-03-25"},
		{Name: "gemini-2.5-flash", Provider: "google", Kind: "gemini", BaseURL: "https://generativelanguage.googleapis.com", Model: "gemini-2.5-flash"},
		{Name: "gemini-2.0-flash", Provider: "google", Kind: "gemini", BaseURL: "https://generativelanguage.googleapis.com", Model: "gemini-2.0-flash"},
	}
}

// FindPreset locates a preset by name.
func FindPreset(name string) *ModelPreset {
	for _, p := range ModelPresets() {
		if p.Name == name {
			return &p
		}
	}
	return nil
}
