//go:build darwin

package cli

import (
	"fmt"
	"os"
	"path/filepath"
)

// unitLogHint returns the shell command a user can run to view recent unit logs.
// On macOS, launchd services log to ~/Library/Logs/lerd/<unit>.log.
func unitLogHint(unitName string) string {
	home, _ := os.UserHomeDir()
	logPath := filepath.Join(home, "Library", "Logs", "lerd", unitName+".log")
	return fmt.Sprintf("tail -n 20 %s", logPath)
}
