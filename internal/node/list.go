package node

import (
	"os/exec"
	"strings"

	"github.com/geodro/lerd/internal/config"
)

// ListInstalled returns every Node major version installed under fnm, in the
// order fnm reports them. Empty when fnm isn't on disk or the user has no
// versions installed. Centralises the parsing previously duplicated in
// internal/ui/server.go and internal/mcp/server.go so every surface (web UI,
// MCP, TUI) sees the same list with the same dedupe rules.
func ListInstalled() []string {
	out, err := exec.Command(config.BinDir()+"/fnm", "list").Output()
	if err != nil {
		return nil
	}
	return parseFnmList(string(out))
}

// parseFnmList extracts the major-version numbers from `fnm list` output.
// Each line looks like "* v20.0.0 default" or "  v18.0.0"; we strip the
// leader, the leading "v", split on ".", and keep only numeric majors.
func parseFnmList(raw string) []string {
	seen := map[string]bool{}
	var versions []string
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "* ")
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		v := strings.TrimPrefix(fields[0], "v")
		if v == "" {
			continue
		}
		major := strings.SplitN(v, ".", 2)[0]
		if strings.Trim(major, "0123456789") != "" {
			continue
		}
		if !seen[major] {
			seen[major] = true
			versions = append(versions, major)
		}
	}
	return versions
}
