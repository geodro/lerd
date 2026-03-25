package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/geodro/lerd/internal/config"
	gitpkg "github.com/geodro/lerd/internal/git"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// NewSitesCmd returns the sites command.
func NewSitesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sites",
		Short: "List all registered sites",
		RunE:  runSites,
	}
}

func termWidth() int {
	w, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || w <= 0 {
		return 120 // assume wide if not a tty
	}
	return w
}

func runSites(_ *cobra.Command, _ []string) error {
	reg, err := config.LoadSites()
	if err != nil {
		return err
	}

	if len(reg.Sites) == 0 {
		fmt.Println("No sites registered. Use 'lerd park' or 'lerd link' to add sites.")
		return nil
	}

	width := termWidth()

	for _, s := range reg.Sites {
		fwName := s.Framework
		if fwName == "" {
			fwName, _ = config.DetectFramework(s.Path)
		}
		fwLabel := ""
		if fw, ok := config.GetFramework(fwName); ok {
			fwLabel = fw.Label
		}

		var worktrees []gitpkg.Worktree
		if gitpkg.IsMainRepo(s.Path) {
			worktrees, _ = gitpkg.DetectWorktrees(s.Path, s.Domain)
		}

		switch {
		case width >= 120:
			printSiteWide(s, fwLabel)
			for _, wt := range worktrees {
				printWorktreeWide(wt, s)
			}
		case width >= 80:
			printSiteMedium(s, fwLabel)
			for _, wt := range worktrees {
				printWorktreeMedium(wt, s)
			}
		default:
			printSiteCompact(s, fwLabel)
			for _, wt := range worktrees {
				printWorktreeCompact(wt, s)
			}
		}
	}

	return nil
}

func pausedTag() string { return "\033[33mpaused\033[0m" }

// Wide layout: Name Domain PHP Node TLS Framework Status Path  (≥120 cols)
func printSiteWide(s config.Site, fwLabel string) {
	if !siteWideHeaderPrinted {
		fmt.Printf("%-22s %-32s %-6s %-6s %-4s %-10s %-8s %s\n",
			"Name", "Domain", "PHP", "Node", "TLS", "Framework", "Status", "Path")
		fmt.Printf("%s %s %s %s %s %s %s %s\n",
			strings.Repeat("─", 22), strings.Repeat("─", 32),
			strings.Repeat("─", 6), strings.Repeat("─", 6),
			strings.Repeat("─", 4), strings.Repeat("─", 10),
			strings.Repeat("─", 8), strings.Repeat("─", 28))
		siteWideHeaderPrinted = true
	}
	tls := "No"
	if s.Secured {
		tls = "Yes"
	}
	statusCol := fmt.Sprintf("%-8s", "")
	if s.Paused {
		statusCol = pausedTag() + "  "
	}
	fmt.Printf("%-22s %-32s %-6s %-6s %-4s %-10s %s %s\n",
		truncate(s.Name, 22), truncate(s.Domain, 32),
		s.PHPVersion, s.NodeVersion, tls, fwLabel, statusCol, s.Path)
}

func printWorktreeWide(wt gitpkg.Worktree, s config.Site) {
	fmt.Printf("  %-20s %-32s %-6s %-6s %-4s %-10s %-8s %s\n",
		"↳ "+truncate(wt.Branch, 18), truncate(wt.Domain, 32),
		s.PHPVersion, s.NodeVersion, "—", "", "", wt.Path)
}

// Medium layout: Domain PHP TLS Framework Status Path  (80–119 cols, no Node, shorter Name)
func printSiteMedium(s config.Site, fwLabel string) {
	if !siteMediumHeaderPrinted {
		fmt.Printf("%-28s %-6s %-4s %-10s %-8s %s\n",
			"Domain", "PHP", "TLS", "Framework", "Status", "Path")
		fmt.Printf("%s %s %s %s %s %s\n",
			strings.Repeat("─", 28), strings.Repeat("─", 6),
			strings.Repeat("─", 4), strings.Repeat("─", 10),
			strings.Repeat("─", 8), strings.Repeat("─", 22))
		siteMediumHeaderPrinted = true
	}
	tls := "No"
	if s.Secured {
		tls = "Yes"
	}
	statusCol := fmt.Sprintf("%-8s", "")
	if s.Paused {
		statusCol = pausedTag() + "  "
	}
	fmt.Printf("%-28s %-6s %-4s %-10s %s %s\n",
		truncate(s.Domain, 28), s.PHPVersion, tls, fwLabel, statusCol, s.Path)
}

func printWorktreeMedium(wt gitpkg.Worktree, s config.Site) {
	fmt.Printf("  %-26s %-6s %-4s %-10s %-8s %s\n",
		"↳ "+truncate(wt.Domain, 24), s.PHPVersion, "—", "", "", wt.Path)
}

// Compact layout: two lines per site  (<80 cols)
func printSiteCompact(s config.Site, fwLabel string) {
	status := ""
	if s.Paused {
		status = " [" + pausedTag() + "]"
	}
	tls := ""
	if s.Secured {
		tls = " 🔒"
	}
	meta := s.PHPVersion
	if fwLabel != "" {
		meta += " · " + fwLabel
	}
	fmt.Printf("%s%s%s\n", s.Domain, tls, status)
	fmt.Printf("  %s\n", truncate(s.Path, 76))
	if meta != "" {
		fmt.Printf("  \033[2m%s\033[0m\n", meta)
	}
}

func printWorktreeCompact(wt gitpkg.Worktree, s config.Site) {
	fmt.Printf("  ↳ %s\n", wt.Domain)
	fmt.Printf("    %s\n", truncate(wt.Path, 74))
}

// package-level header guards so headers print once per run
var (
	siteWideHeaderPrinted   bool
	siteMediumHeaderPrinted bool
)

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
