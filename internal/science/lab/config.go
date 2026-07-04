package lab

import (
	"encoding/json"
	"os"
	"path/filepath"

	sciconfig "lumen/internal/science/config"
)

const defaultToolProfile = "full_science"

// LocalConfig holds lab-specific settings under ~/.lumen/science/lab/.
type LocalConfig struct {
	DefaultPort int    `json:"default_port,omitempty"`
	ToolProfile string `json:"tool_profile,omitempty"`
	DefaultMode string `json:"default_mode,omitempty"` // plan | agent
}

func labRoot(sciDir string) string {
	return filepath.Join(sciDir, "lab")
}

func localConfigPath(sciDir string) string {
	return filepath.Join(labRoot(sciDir), "lab.json")
}

func loadLocalConfig(sciDir string) LocalConfig {
	cfg := LocalConfig{DefaultPort: DefaultPort, ToolProfile: defaultToolProfile, DefaultMode: "plan"}
	data, err := os.ReadFile(localConfigPath(sciDir))
	if err != nil {
		return cfg
	}
	_ = json.Unmarshal(data, &cfg)
	if cfg.DefaultPort == 0 {
		cfg.DefaultPort = DefaultPort
	}
	if cfg.ToolProfile == "" {
		cfg.ToolProfile = defaultToolProfile
	}
	if cfg.DefaultMode == "" {
		cfg.DefaultMode = "plan"
	}
	return cfg
}

func scienceConfig(sciDir string) (sciconfig.File, error) {
	return sciconfig.Load(sciDir)
}
