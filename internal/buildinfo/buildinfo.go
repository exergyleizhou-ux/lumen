package buildinfo

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
)

var (
	Version   = "0.0.0-dev"
	Commit    = "unknown"
	BuildDate = "unknown"
	BuiltBy   = "unknown"
)

func Init(v, c, d, b string) {
	if v != "" {
		Version = v
	}
	if c != "" {
		Commit = c
	}
	if d != "" {
		BuildDate = d
	}
	if b != "" {
		BuiltBy = b
	}
}

type Info struct {
	Version, Commit, BuildDate, GoVersion, OS, Arch, BuiltBy string
	Dirty                                                    bool
}

func Get() *Info {
	info := &Info{Version: Version, Commit: Commit, BuildDate: BuildDate, GoVersion: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH, BuiltBy: BuiltBy}
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				if Commit == "unknown" {
					info.Commit = s.Value
				}
			case "vcs.modified":
				info.Dirty = s.Value == "true"
			case "vcs.time":
				if BuildDate == "unknown" {
					info.BuildDate = s.Value
				}
			}
		}
	}
	return info
}
func (i *Info) Short() string {
	if i.Dirty {
		return fmt.Sprintf("%s-%s (dirty)", i.Version, i.Commit[:8])
	}
	return fmt.Sprintf("%s-%s", i.Version, i.Commit[:8])
}
func (i *Info) String() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Lumen %s\ncommit: %s\nbuilt: %s\ngo: %s\nos/arch: %s/%s\n", i.Version, i.Commit, i.BuildDate, i.GoVersion, i.OS, i.Arch)
	return sb.String()
}
