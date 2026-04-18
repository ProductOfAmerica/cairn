package cli

import (
	"os"
	"path/filepath"
	"runtime"
)

// ResolveStateRoot returns the cairn state-root directory per § 3a of the
// design spec. Precedence: explicit override > CAIRN_HOME env > platform
// default.
func ResolveStateRoot(override string) string {
	if override != "" {
		return override
	}
	if v := os.Getenv("CAIRN_HOME"); v != "" {
		return v
	}
	switch runtime.GOOS {
	case "linux":
		if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
			return filepath.Join(xdg, "cairn")
		}
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".local", "share", "cairn")
	case "darwin":
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".cairn")
	case "windows":
		up := os.Getenv("USERPROFILE")
		if up == "" {
			up, _ = os.UserHomeDir()
		}
		return filepath.Join(up, ".cairn")
	default:
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".cairn")
	}
}
