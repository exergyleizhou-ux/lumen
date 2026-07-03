package doctor

import (
	"lumen/internal/config"
	sciconfig "lumen/internal/science/config"
	"lumen/internal/science/runtime"
)

func defaultScienceDir() (string, error) {
	return sciconfig.Dir()
}

func runScienceDoctor(sciDir string, cfg *config.File) ([]runtime.DoctorResult, int, int) {
	if cfg == nil {
		cfg = &config.File{}
	}
	return runtime.RunDoctor(sciDir, cfg)
}