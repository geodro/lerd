package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/nginx"
	"github.com/spf13/cobra"
)

// NewUnparkCmd returns the unpark command.
func NewUnparkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unpark [directory]",
		Short: "Remove a parked directory and unlink all its sites",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runUnpark,
	}
}

func runUnpark(_ *cobra.Command, args []string) error {
	dir := ""
	if len(args) > 0 {
		dir = args[0]
	} else {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return err
		}
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}

	cfg, err := config.LoadGlobal()
	if err != nil {
		return err
	}

	// Remove from parked directories list
	found := false
	filtered := cfg.ParkedDirectories[:0]
	for _, pd := range cfg.ParkedDirectories {
		if pd == absDir {
			found = true
		} else {
			filtered = append(filtered, pd)
		}
	}
	if !found {
		return fmt.Errorf("%s is not a parked directory", absDir)
	}
	cfg.ParkedDirectories = filtered
	if err := config.SaveGlobal(cfg); err != nil {
		return err
	}

	// Remove all sites whose path is under this directory
	reg, err := config.LoadSites()
	if err != nil {
		return err
	}

	removed := 0
	for _, site := range reg.Sites {
		if !strings.HasPrefix(site.Path, absDir+string(filepath.Separator)) {
			continue
		}
		if err := nginx.RemoveVhost(site.PrimaryDomain()); err != nil {
			fmt.Printf("  [WARN] removing vhost for %s: %v\n", site.Name, err)
		}
		if err := config.RemoveSite(site.Name); err != nil {
			fmt.Printf("  [WARN] removing site %s: %v\n", site.Name, err)
			continue
		}
		fmt.Printf("  - %s (%s)\n", site.Name, site.PrimaryDomain())
		removed++
	}

	fmt.Printf("Unparked: %s (%d site(s) removed)\n", absDir, removed)

	if removed > 0 {
		if err := nginx.Reload(); err != nil {
			fmt.Printf("  [WARN] nginx reload: %v\n", err)
		}
	}

	return nil
}
