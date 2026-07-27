package buildcheck

import (
	"fmt"
	"os"
	"regexp"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
)

const Version = "0.1.0"

type Info struct {
	Version       string `json:"version"`
	ModuleVersion string `json:"module_version,omitempty"`
	Revision      string `json:"revision,omitempty"`
	Modified      bool   `json:"modified"`
	Go            string `json:"go"`
}

func Current() Info {
	info := Info{Version: Version, Go: runtime.Version()}
	if build, ok := debug.ReadBuildInfo(); ok {
		info.ModuleVersion = build.Main.Version
		for _, setting := range build.Settings {
			switch setting.Key {
			case "vcs.revision":
				info.Revision = setting.Value
			case "vcs.modified":
				info.Modified = setting.Value == "true"
			}
		}
	}
	return info
}

// CheckKernelPin enforces the immutable source pin when configured. Bootstrap
// is a deliberate, replayable exception and is illegal once enforcement is
// active.
func CheckKernelPin(pin, enforcement string) error {
	if pin == "hctl@bootstrap" {
		if enforcement == "active" {
			return fmt.Errorf("STALE_BINARY: active enforcement cannot use hctl@bootstrap")
		}
		return nil
	}
	value := strings.TrimPrefix(pin, "hctl@")
	info := Current()
	if regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`).MatchString(value) {
		if info.Revision == "" {
			return fmt.Errorf("STALE_BINARY: binary has no vcs.revision; expected %s", value)
		}
		if info.Revision != value {
			return fmt.Errorf("STALE_BINARY: binary revision %s does not match kernel pin %s", info.Revision, value)
		}
		if info.Modified {
			return fmt.Errorf("STALE_BINARY: binary was built from a modified worktree")
		}
		return nil
	}
	if value == "v"+Version || value == Version {
		want := value
		if !strings.HasPrefix(want, "v") {
			want = "v" + want
		}
		if info.ModuleVersion != want {
			return fmt.Errorf(
				"STALE_BINARY: module build version %q does not match semver kernel pin %s",
				info.ModuleVersion, want,
			)
		}
		if info.Modified {
			return fmt.Errorf("STALE_BINARY: semver-pinned binary was built from a modified worktree")
		}
		return nil
	}
	return fmt.Errorf("STALE_BINARY: unsupported kernel pin %q", pin)
}

func CheckGoDirective(goModPath string) error {
	raw, err := os.ReadFile(goModPath)
	if err != nil {
		return err
	}
	var required string
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "go" {
			required = fields[1]
			break
		}
	}
	if required == "" {
		return fmt.Errorf("go.mod has no go directive")
	}
	have := strings.TrimPrefix(runtime.Version(), "go")
	if compareVersion(have, required) < 0 {
		return fmt.Errorf("Go %s is older than go.mod requirement %s", have, required)
	}
	return nil
}

func compareVersion(a, b string) int {
	parse := func(value string) [3]int {
		var result [3]int
		parts := strings.SplitN(value, ".", 4)
		for i := 0; i < len(parts) && i < 3; i++ {
			digits := strings.TrimRightFunc(parts[i], func(r rune) bool { return r < '0' || r > '9' })
			result[i], _ = strconv.Atoi(digits)
		}
		return result
	}
	aa, bb := parse(a), parse(b)
	for i := range aa {
		if aa[i] < bb[i] {
			return -1
		}
		if aa[i] > bb[i] {
			return 1
		}
	}
	return 0
}
