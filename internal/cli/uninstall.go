package cli

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/geodro/lerd/internal/certs"
	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/dns"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/services"
	"github.com/spf13/cobra"
)

// NewUninstallCmd returns the uninstall command.
func NewUninstallCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove Lerd and all its components",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runUninstall(force)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation prompts")
	return cmd
}

func runUninstall(force bool) error {
	fmt.Println("==> Uninstalling Lerd")

	if !force {
		fmt.Print("  This will stop all containers and remove Lerd. Continue? [y/N] ")
		if !readYes() {
			fmt.Println("  Aborted.")
			return nil
		}
	}

	// Ask about data removal up front — the StepRunner puts stdin into raw
	// mode and its reader goroutine would consume bytes meant for this prompt.
	removeData := force || confirmRemoveData()
	removeMCP := force || confirmRemoveMCPIntegration()
	removeMkcertCA := force || confirmRemoveMkcertCA()
	purgeImages := force || confirmPurgeLerdImages()

	// DNS teardown runs outside the step runner because it may prompt for sudo.
	fmt.Println("  --> Removing DNS configuration")
	dns.Teardown()

	step("Stopping containers and services")
	{
		// Use the service manager so this works on both Linux (systemd/quadlet)
		// and macOS (launchd plists).
		seen := map[string]bool{}
		for _, unit := range services.Mgr.ListContainerUnits("lerd-*") {
			seen[unit] = true
			status, _ := podman.UnitStatus(unit)
			if status == "active" || status == "activating" {
				_ = podman.StopUnit(unit)
			}
			_ = services.Mgr.Disable(unit)
		}
		for _, unit := range services.Mgr.ListServiceUnits("lerd-*") {
			if seen[unit] {
				continue
			}
			status, _ := podman.UnitStatus(unit)
			if status == "active" || status == "activating" {
				_ = podman.StopUnit(unit)
			}
			_ = services.Mgr.Disable(unit)
		}
		for _, unit := range services.Mgr.ListTimerUnits("lerd-*") {
			_ = podman.StopUnit(unit)
			_ = services.Mgr.Disable(unit)
		}
		// Kill any running tray process. The tray may be running standalone
		// (launched from the desktop file or `lerd tray`) without a unit,
		// in which case the unit teardown above misses it.
		killTray()
	}
	ok()

	step("Removing service units")
	{
		seen := map[string]bool{}
		for _, unit := range services.Mgr.ListContainerUnits("lerd-*") {
			seen[unit] = true
			_ = services.Mgr.RemoveContainerUnit(unit)
		}
		for _, unit := range services.Mgr.ListServiceUnits("lerd-*") {
			if seen[unit] {
				continue
			}
			_ = services.Mgr.RemoveServiceUnit(unit)
		}
		for _, unit := range services.Mgr.ListTimerUnits("lerd-*") {
			_ = services.Mgr.RemoveTimerUnit(strings.TrimSuffix(unit, ".timer"))
		}
	}
	ok()

	step("Reloading service manager")
	_ = podman.DaemonReloadFn()
	ok()

	step("Removing lerd Podman network")
	_ = podman.RemoveNetwork("lerd")
	ok()

	if purgeImages {
		fmt.Println("  --> Purging lerd-built container images")
		removeLerdImages()
	}

	if removeMkcertCA {
		fmt.Println("  --> Uninstalling mkcert CA from system trust stores")
		cmd := exec.Command(certs.MkcertPath(), "-uninstall")
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		_ = cmd.Run()
	}

	if removeMCP {
		fmt.Println("  --> Removing MCP integration (global)")
		if home, err := os.UserHomeDir(); err == nil {
			_ = RemoveGlobalAISkills(home, true)
		}
		fmt.Println("  --> Removing MCP integration (registered sites)")
		if reg, err := config.LoadSites(); err == nil {
			for _, s := range reg.Sites {
				if err := RemoveProjectAISkills(s.Path, false); err == nil {
					fmt.Printf("    cleaned %s\n", s.Path)
				}
			}
		}
	}

	step("Removing shell PATH entry")
	removeShellEntry()
	ok()

	step("Removing lerd binary")
	if self, err := selfPath(); err == nil {
		os.Remove(self) //nolint:errcheck
	}
	ok()

	fmt.Println()
	if removeData {
		fmt.Print("  --> Removing config and data directories ... ")
		os.RemoveAll(config.ConfigDir())
		os.RemoveAll(config.DataDir())
		fmt.Println("OK")
	} else {
		fmt.Printf("  Config kept at %s\n", config.ConfigDir())
		fmt.Printf("  Data kept at   %s\n", config.DataDir())
	}

	fmt.Println("\nLerd uninstalled.")
	return nil
}

func confirmRemoveMCPIntegration() bool {
	fmt.Print("  Remove MCP integration (global skills + per-site .mcp/.claude/.cursor/.junie files)? [y/N] ")
	return readYes()
}

func confirmRemoveMkcertCA() bool {
	fmt.Print("  Uninstall mkcert CA from system trust stores? [y/N] ")
	return readYes()
}

func confirmPurgeLerdImages() bool {
	fmt.Print("  Purge lerd-built container images (lerd-php*-fpm, lerd-custom-*, lerd-dnsmasq)? Databases and app files are unaffected. [y/N] ")
	return readYes()
}

// removeLerdImages removes locally-built lerd images. Upstream pulls
// (mysql/redis/postgres/etc.) are left alone since they're expensive to
// re-pull and not lerd-owned.
func removeLerdImages() {
	out, err := exec.Command(podman.PodmanBin(), "images", "--format", "{{.Repository}}:{{.Tag}}").Output()
	if err != nil {
		fmt.Printf("    WARN: listing images: %v\n", err)
		return
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !isLerdBuiltImage(line) {
			continue
		}
		if err := exec.Command(podman.PodmanBin(), "image", "rm", "-f", line).Run(); err != nil {
			fmt.Printf("    WARN: removing %s: %v\n", line, err)
			continue
		}
		fmt.Printf("    removed %s\n", line)
	}
}

// isLerdBuiltImage matches the locally-built tags lerd owns.
func isLerdBuiltImage(ref string) bool {
	switch {
	case strings.HasPrefix(ref, "lerd-php") && strings.HasSuffix(ref, "-fpm:local"):
		return true
	case strings.HasPrefix(ref, "lerd-custom-") && strings.HasSuffix(ref, ":local"):
		return true
	case ref == "lerd-dnsmasq:local":
		return true
	}
	return false
}

func confirmRemoveData() bool {
	fmt.Print("  Remove all config and data (~/.config/lerd, ~/.local/share/lerd)? [y/N] ")
	return readYes()
}

func readYes() bool {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	ans := strings.TrimSpace(scanner.Text())
	return strings.EqualFold(ans, "y") || strings.EqualFold(ans, "yes")
}

// shellRCMarkers lists every marker comment lerd's install pipelines
// ever wrote into a user shell rc, paired with the number of follow-up
// lines belonging to that block. Each entry is a separate writer:
//
//   - "# Added by Lerd installer" → install.sh; 1 trailing line
//     (`export PATH=...` or fish `fish_add_path …`)
//   - "# Lerd"                    → install.go appendShellRC; 1 trailing
//     line (`export PATH=...`)
//   - "# Lerd completions"        → install.go ensureZshFpath; 2 trailing
//     lines (`fpath=(...)` + `autoload -Uz compinit && compinit`)
//
// uninstall used to match only the first marker, which left the other
// two blocks behind on every install path that went through `lerd
// install` (which is most of them). User-visible: `# Lerd … export
// PATH …` lingering in `~/.zshrc` after a clean uninstall.
var shellRCMarkers = []struct {
	marker     string
	skipAfter  int
}{
	{"# Added by Lerd installer", 1},
	{"# Lerd completions", 2}, // must be before "# Lerd" — longer prefix wins
	{"# Lerd", 1},
}

func removeShellEntry() {
	home, _ := os.UserHomeDir()

	candidates := []string{
		filepath.Join(home, ".bashrc"),
		filepath.Join(home, ".zshrc"),
		filepath.Join(home, ".config", "fish", "conf.d", "lerd.fish"),
	}

	for _, rc := range candidates {
		for _, m := range shellRCMarkers {
			removeMarkedBlock(rc, m.marker, m.skipAfter)
		}
		// The fish path is lerd-dedicated — if our removals left only
		// whitespace behind, delete the file rather than leave an empty
		// conf.d entry that fish still sources on every shell start.
		if strings.HasSuffix(rc, "lerd.fish") {
			if data, err := os.ReadFile(rc); err == nil && strings.TrimSpace(string(data)) == "" {
				os.Remove(rc) //nolint:errcheck
			}
		}
	}
}

// removeMarkedBlock removes the marker line plus skipAfter follow-up
// lines. The marker match is exact (after TrimSpace) so a comment that
// happens to contain "# Lerd" as a substring (e.g. "# Lerd-related
// notes") isn't touched.
func removeMarkedBlock(path, marker string, skipAfter int) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}

	lines := strings.Split(string(data), "\n")
	out := make([]string, 0, len(lines))
	skip := 0
	for _, line := range lines {
		if skip > 0 {
			skip--
			continue
		}
		if strings.TrimSpace(line) == marker {
			skip = skipAfter
			continue
		}
		out = append(out, line)
	}

	// Only rewrite if something changed
	result := strings.Join(out, "\n")
	if result != string(data) {
		os.WriteFile(path, []byte(result), 0644) //nolint:errcheck
	}
}
