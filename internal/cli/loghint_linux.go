//go:build linux

package cli

import "fmt"

// unitLogHint returns the shell command a user can run to view recent unit logs.
func unitLogHint(unitName string) string {
	return fmt.Sprintf("journalctl --user -u %s -n 20", unitName)
}
