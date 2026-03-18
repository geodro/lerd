// Package envfile provides helpers for reading and updating .env files
// while preserving comments, blank lines, and line order.
package envfile

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// ApplyUpdates rewrites the .env at path, replacing values for any key in updates.
// Keys not already present are appended at the end. Comments and blank lines are preserved.
func ApplyUpdates(path string, updates map[string]string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}

	var lines []string
	applied := map[string]bool{}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "#") && strings.Contains(line, "=") {
			k, _, _ := strings.Cut(line, "=")
			k = strings.TrimSpace(k)
			if newVal, ok := updates[k]; ok {
				line = k + "=" + newVal
				applied[k] = true
			}
		}
		lines = append(lines, line)
	}
	f.Close()
	if err := scanner.Err(); err != nil {
		return err
	}

	for k, v := range updates {
		if !applied[k] {
			lines = append(lines, k+"="+v)
		}
	}

	out := strings.Join(lines, "\n")
	if len(lines) > 0 {
		out += "\n"
	}
	return os.WriteFile(path, []byte(out), 0644)
}

// UpdateAppURL sets APP_URL in the project's .env to scheme://domain.
// Silently does nothing if no .env exists.
func UpdateAppURL(projectPath, scheme, domain string) error {
	envPath := filepath.Join(projectPath, ".env")
	if _, err := os.Stat(envPath); os.IsNotExist(err) {
		return nil
	}
	return ApplyUpdates(envPath, map[string]string{
		"APP_URL": scheme + "://" + domain,
	})
}
