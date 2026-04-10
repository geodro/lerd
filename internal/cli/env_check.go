package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/geodro/lerd/internal/envfile"
	"github.com/spf13/cobra"
)

// NewEnvCheckCmd returns the env:check command.
func NewEnvCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "env:check",
		Short:        "Compare all .env files against .env.example and flag missing keys",
		SilenceUsage: true,
		RunE:         runEnvCheck,
	}
}

// diffEnvKeys compares envFile against exampleFile and returns
// keys missing from envFile and keys extra in envFile.
func diffEnvKeys(exampleFile, envFile string) (missing, extra []string) {
	exampleKeys, err := envfile.ReadKeys(exampleFile)
	if err != nil {
		return nil, nil
	}
	envKeys, err := envfile.ReadKeys(envFile)
	if err != nil {
		return nil, nil
	}

	envSet := make(map[string]bool, len(envKeys))
	for _, k := range envKeys {
		envSet[k] = true
	}
	exampleSet := make(map[string]bool, len(exampleKeys))
	for _, k := range exampleKeys {
		exampleSet[k] = true
	}

	for _, k := range exampleKeys {
		if !envSet[k] {
			missing = append(missing, k)
		}
	}
	for _, k := range envKeys {
		if !exampleSet[k] {
			extra = append(extra, k)
		}
	}
	return missing, extra
}

// findEnvFiles returns all .env* files in dir except .env.example, sorted.
func findEnvFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var files []string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, ".env") || e.IsDir() {
			continue
		}
		if name == ".env.example" {
			continue
		}
		files = append(files, filepath.Join(dir, name))
	}
	sort.Strings(files)
	return files
}

func runEnvCheck(_ *cobra.Command, _ []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	examplePath := filepath.Join(cwd, ".env.example")
	if _, err := os.Stat(examplePath); os.IsNotExist(err) {
		return fmt.Errorf("no .env.example found in %s", cwd)
	}

	envFiles := findEnvFiles(cwd)
	if len(envFiles) == 0 {
		return fmt.Errorf("no .env files found in %s — run lerd env to create one", cwd)
	}

	exampleKeys, err := envfile.ReadKeys(examplePath)
	if err != nil {
		return fmt.Errorf("reading .env.example: %w", err)
	}

	// Build per-file key sets.
	type fileInfo struct {
		name   string
		keySet map[string]bool
		keys   []string
	}
	var files []fileInfo
	for _, f := range envFiles {
		keys, err := envfile.ReadKeys(f)
		if err != nil {
			continue
		}
		set := make(map[string]bool, len(keys))
		for _, k := range keys {
			set[k] = true
		}
		files = append(files, fileInfo{
			name:   filepath.Base(f),
			keySet: set,
			keys:   keys,
		})
	}

	// Collect all keys that have at least one mismatch.
	allKeys := make(map[string]bool)
	exampleSet := make(map[string]bool, len(exampleKeys))
	for _, k := range exampleKeys {
		exampleSet[k] = true
	}

	// Check for keys missing from any env file or extra in any env file.
	for _, k := range exampleKeys {
		for _, f := range files {
			if !f.keySet[k] {
				allKeys[k] = true
				break
			}
		}
	}
	for _, f := range files {
		for _, k := range f.keys {
			if !exampleSet[k] {
				allKeys[k] = true
			}
		}
	}

	if len(allKeys) == 0 {
		if len(files) == 1 {
			fmt.Printf("  %s is in sync with .env.example\n", files[0].name)
		} else {
			fmt.Println("  all .env files are in sync with .env.example")
		}
		return nil
	}

	// Sort keys for stable output.
	var sortedKeys []string
	for k := range allKeys {
		sortedKeys = append(sortedKeys, k)
	}
	sort.Strings(sortedKeys)

	// Compute column widths.
	keyWidth := len("KEY")
	for _, k := range sortedKeys {
		if len(k) > keyWidth {
			keyWidth = len(k)
		}
	}

	colWidths := make([]int, len(files)+1)
	colWidths[0] = len(".env.example")
	for i, f := range files {
		colWidths[i+1] = len(f.name)
		if colWidths[i+1] < 4 {
			colWidths[i+1] = 4
		}
	}

	// Print header.
	fmt.Printf("  %-*s", keyWidth, "KEY")
	fmt.Printf("  %-*s", colWidths[0], ".env.example")
	for i, f := range files {
		fmt.Printf("  %-*s", colWidths[i+1], f.name)
	}
	fmt.Println()

	// Print separator.
	fmt.Printf("  %s", dashes(keyWidth))
	fmt.Printf("  %s", dashes(colWidths[0]))
	for i := range files {
		fmt.Printf("  %s", dashes(colWidths[i+1]))
	}
	fmt.Println()

	// Print rows.
	totalMissing := 0
	for _, k := range sortedKeys {
		fmt.Printf("  %-*s", keyWidth, k)
		if exampleSet[k] {
			fmt.Printf("  %s", center("✓", colWidths[0]))
		} else {
			fmt.Printf("  %s", center("✗", colWidths[0]))
		}
		for i, f := range files {
			if f.keySet[k] {
				fmt.Printf("  %s", center("✓", colWidths[i+1]))
			} else {
				fmt.Printf("  %s", center("✗", colWidths[i+1]))
				if exampleSet[k] {
					totalMissing++
				}
			}
		}
		fmt.Println()
	}

	fmt.Println()
	fmt.Printf("  %d key(s) out of sync\n", len(sortedKeys))

	return nil
}

func dashes(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = '-'
	}
	return string(b)
}

func center(s string, width int) string {
	pad := width - 1
	left := pad / 2
	right := pad - left
	return fmt.Sprintf("%*s%s%*s", left, "", s, right, "")
}
