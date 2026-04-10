package store

import (
	"fmt"
	"os"

	"github.com/geodro/lerd/internal/config"
	"golang.org/x/term"
)

// DetectFrameworkWithStore wraps config.DetectFramework with a store fallback.
// When no local framework matches and stdin is a terminal, it checks the store
// index and prompts the user to install a matching definition.
// Returns the framework name and true if resolved, ("", false) otherwise.
func DetectFrameworkWithStore(dir string) (string, bool) {
	// Try local detection first
	if name, ok := config.DetectFramework(dir); ok {
		return name, true
	}

	// Only prompt in interactive mode
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", false
	}

	client := NewClient()
	entry, version, ok := client.DetectFromStore(dir)
	if !ok {
		return "", false
	}

	// Prompt user
	fmt.Printf("Detected %s project. Install framework definition from the store? [Y/n] ", entry.Label)
	var answer string
	fmt.Scanln(&answer) //nolint:errcheck
	if answer != "" && answer[0] != 'Y' && answer[0] != 'y' {
		return "", false
	}

	fw, err := client.FetchFramework(entry.Name, version)
	if err != nil {
		fmt.Printf("  [WARN] Failed to fetch %s: %v\n", entry.Name, err)
		return "", false
	}

	if err := config.SaveStoreFramework(fw); err != nil {
		fmt.Printf("  [WARN] Failed to save %s: %v\n", entry.Name, err)
		return "", false
	}

	versionStr := fw.Version
	if versionStr == "" {
		versionStr = "latest"
	}
	fmt.Printf("  Installed %s@%s\n", fw.Name, versionStr)
	return fw.Name, true
}
