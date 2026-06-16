package editverify

import (
	"github.com/BurntSushi/toml"
)

// ConfigFromTOML parses the [verify] section from raw TOML bytes.
//
//   - Missing [verify] section → returns DefaultConfig().
//   - Missing fields → keep the default value for that field.
//   - Invalid scope → falls back to "changed-pkg".
//   - max_repair_cycles <= 0 → falls back to 3.
func ConfigFromTOML(raw []byte) (Config, error) {
	c := DefaultConfig()

	// Wrap the [verify] section in a toml struct
	var file struct {
		Verify struct {
			Enabled         *bool   `toml:"enabled"`
			Command         *string `toml:"command"`
			Scope           *string `toml:"scope"`
			RunTests        *bool   `toml:"run_tests"`
			MaxRepairCycles *int    `toml:"max_repair_cycles"`
		} `toml:"verify"`
	}

	if err := toml.Unmarshal(raw, &file); err != nil {
		return c, err
	}

	sec := file.Verify
	if sec.Enabled != nil {
		c.Enabled = *sec.Enabled
	}
	if sec.Command != nil {
		c.Command = *sec.Command
	}
	if sec.Scope != nil {
		switch *sec.Scope {
		case "changed-pkg", "all":
			c.Scope = *sec.Scope
		default:
			c.Scope = "changed-pkg"
		}
	}
	if sec.RunTests != nil {
		c.RunTests = *sec.RunTests
	}
	if sec.MaxRepairCycles != nil {
		if *sec.MaxRepairCycles > 0 {
			c.MaxRepairCycles = *sec.MaxRepairCycles
		} else {
			c.MaxRepairCycles = 3
		}
	}

	return c, nil
}
