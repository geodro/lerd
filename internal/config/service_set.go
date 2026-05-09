package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// loadServiceNameSet reads a yaml list of service names from path and returns
// it as a set. A missing file is treated as an empty set, not an error.
func loadServiceNameSet(path string) (map[string]bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	if err := yaml.Unmarshal(data, &names); err != nil {
		return nil, err
	}
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m, nil
}

// saveServiceNameSet writes the set back to path as a yaml list, creating the
// data directory if needed.
func saveServiceNameSet(path string, m map[string]bool) error {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := yaml.Marshal(names)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// serviceSetContains reports whether name is in the set stored at path.
// Any read or parse error is treated as "not a member" so callers don't have
// to distinguish missing-file from on-disk corruption.
func serviceSetContains(path, name string) bool {
	m, err := loadServiceNameSet(path)
	if err != nil {
		return false
	}
	return m[name]
}

// serviceSetUpdate flips name's membership in the set stored at path.
func serviceSetUpdate(path, name string, want bool) error {
	m, err := loadServiceNameSet(path)
	if err != nil {
		m = map[string]bool{}
	}
	if want {
		m[name] = true
	} else {
		delete(m, name)
	}
	return saveServiceNameSet(path, m)
}
