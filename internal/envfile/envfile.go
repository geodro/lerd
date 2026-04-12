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
		if applied[k] {
			continue
		}
		// Look for a commented-out version to uncomment in place.
		found := false
		for i, line := range lines {
			if !strings.HasPrefix(line, "#") {
				continue
			}
			trimmed := strings.TrimLeft(line, "# ")
			if !strings.Contains(trimmed, "=") {
				continue
			}
			ck, _, _ := strings.Cut(trimmed, "=")
			if strings.TrimSpace(ck) == k {
				lines[i] = k + "=" + v
				found = true
				break
			}
		}
		if !found {
			lines = append(lines, k+"="+v)
		}
	}

	out := strings.Join(lines, "\n")
	if len(lines) > 0 {
		out += "\n"
	}
	return os.WriteFile(path, []byte(out), 0600)
}

// ReadKey returns the value of a single key from the .env file at path,
// or an empty string if the key is absent or the file cannot be read.
func ReadKey(path, key string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if ok && strings.TrimSpace(k) == key {
			return strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	return ""
}

// ReadKeys returns all non-comment key names from the .env file at path,
// in the order they appear.
func ReadKeys(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var keys []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		k, _, _ := strings.Cut(line, "=")
		k = strings.TrimSpace(k)
		if k != "" {
			keys = append(keys, k)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return keys, nil
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

// SyncPrimaryDomain updates APP_URL and VITE_REVERB_HOST/SCHEME/PORT in the
// project's .env to reflect the current primary domain and TLS state.
// Only keys that already exist in the .env are touched.
// Silently does nothing if no .env exists.
func SyncPrimaryDomain(projectPath, domain string, secured bool) error {
	envPath := filepath.Join(projectPath, ".env")
	if _, err := os.Stat(envPath); os.IsNotExist(err) {
		return nil
	}

	keys, err := ReadKeys(envPath)
	if err != nil {
		return err
	}
	present := make(map[string]bool, len(keys))
	for _, k := range keys {
		present[k] = true
	}

	scheme := "http"
	port := "80"
	if secured {
		scheme = "https"
		port = "443"
	}

	updates := map[string]string{}
	if present["APP_URL"] {
		updates["APP_URL"] = scheme + "://" + domain
	}
	if present["VITE_REVERB_HOST"] {
		updates["VITE_REVERB_HOST"] = domain
	}
	if present["VITE_REVERB_SCHEME"] {
		updates["VITE_REVERB_SCHEME"] = scheme
	}
	if present["VITE_REVERB_PORT"] {
		updates["VITE_REVERB_PORT"] = port
	}

	if len(updates) == 0 {
		return nil
	}
	return ApplyUpdates(envPath, updates)
}
