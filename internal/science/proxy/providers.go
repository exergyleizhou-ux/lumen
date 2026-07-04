package proxy

import "strings"

// Mode is how the proxy talks to the upstream API.
type Mode string

const (
	ModeAnthropic Mode = "anthropic" // native Anthropic wire — passthrough + remap
	ModeOpenAI    Mode = "openai"    // OpenAI-compatible — translate Anthropic↔OpenAI
)

// ModelEntry is one model exposed to Claude Science's selector.
type ModelEntry struct {
	ID          string
	DisplayName string
}

// ProviderSpec is a built-in upstream preset for the Science proxy.
type ProviderSpec struct {
	Name         string
	Mode         Mode
	URL          string
	KeyEnv       string
	Models       []ModelEntry
	ModelMap     map[string]string
	ModelCaps    map[string]int
	DefaultCap   int
	DefaultModel string
	DsmlCapable  bool // DeepSeek may leak DSML tool calls as plain text
	DualAuth     bool // relay: send x-api-key + Authorization Bearer
}

// BuiltInProviders maps CLI/config provider names to proxy presets.
var BuiltInProviders = map[string]ProviderSpec{
	"deepseek": {
		Name:        "deepseek",
		Mode:        ModeAnthropic,
		URL:         "https://api.deepseek.com/anthropic/v1/messages",
		KeyEnv:      "DEEPSEEK_API_KEY",
		DsmlCapable: true,
		Models: []ModelEntry{
			{ID: "claude-opus-4-8", DisplayName: "DeepSeek V4 Pro"},
			{ID: "claude-haiku-4-5", DisplayName: "DeepSeek V4 Flash"},
		},
		ModelMap: map[string]string{
			"claude-opus-4-8":   "deepseek-v4-pro",
			"claude-sonnet-5":   "deepseek-v4-flash",
			"claude-sonnet-4-6": "deepseek-v4-flash",
			"claude-haiku-4-5":  "deepseek-v4-flash",
		},
		ModelCaps: map[string]int{
			"deepseek-v4-pro":   65536,
			"deepseek-v4-flash": 32768,
		},
		DefaultCap:   8192,
		DefaultModel: "deepseek-v4-flash",
	},
	"moonshot": {
		Name:   "moonshot",
		Mode:   ModeOpenAI,
		URL:    "https://api.moonshot.cn/v1/chat/completions",
		KeyEnv: "MOONSHOT_API_KEY",
		Models: []ModelEntry{
			{ID: "kimi-k2", DisplayName: "Kimi K2"},
			{ID: "moonshot-v1", DisplayName: "Moonshot V1"},
		},
		ModelMap: map[string]string{
			"claude-opus-4-8":   "kimi-k2-0711-preview",
			"claude-sonnet-5":   "moonshot-v1-8k",
			"claude-sonnet-4-6": "moonshot-v1-8k",
			"claude-haiku-4-5":  "moonshot-v1-8k",
		},
		DefaultCap:   8192,
		DefaultModel: "moonshot-v1-8k",
	},
	"zhipu": {
		Name:   "zhipu",
		Mode:   ModeOpenAI,
		URL:    "https://open.bigmodel.cn/api/paas/v4/chat/completions",
		KeyEnv: "ZHIPU_API_KEY",
		Models: []ModelEntry{
			{ID: "glm-4", DisplayName: "GLM-4"},
			{ID: "glm-4-flash", DisplayName: "GLM-4 Flash"},
		},
		ModelMap: map[string]string{
			"claude-opus-4-8":   "glm-4-plus",
			"claude-sonnet-5":   "glm-4",
			"claude-sonnet-4-6": "glm-4",
			"claude-haiku-4-5":  "glm-4-flash",
		},
		DefaultCap:   8192,
		DefaultModel: "glm-4",
	},
	"qwen": {
		Name:   "qwen",
		Mode:   ModeOpenAI,
		URL:    "https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions",
		KeyEnv: "DASHSCOPE_API_KEY",
		Models: []ModelEntry{
			{ID: "qwen-max", DisplayName: "Qwen Max"},
			{ID: "qwen-plus", DisplayName: "Qwen Plus"},
			{ID: "qwen-turbo", DisplayName: "Qwen Turbo"},
		},
		ModelMap: map[string]string{
			"claude-opus-4-8":   "qwen-max",
			"claude-sonnet-5":   "qwen-plus",
			"claude-sonnet-4-6": "qwen-plus",
			"claude-haiku-4-5":  "qwen-turbo",
		},
		ModelCaps: map[string]int{
			"qwen-max":   8192,
			"qwen-plus":  8192,
			"qwen-turbo": 8192,
		},
		DefaultCap:   8192,
		DefaultModel: "qwen-plus",
	},
}

// LookupProvider returns a provider spec by name (case-insensitive).
func LookupProvider(name string) (ProviderSpec, bool) {
	p, ok := BuiltInProviders[strings.ToLower(strings.TrimSpace(name))]
	return p, ok
}
