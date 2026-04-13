package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/geodro/lerd/internal/config"
	phpPkg "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/serviceops"
	"github.com/spf13/cobra"
)

var knownServices = []string{"mysql", "redis", "postgres", "meilisearch", "rustfs", "mailpit"}

// serviceInfo holds the quadlet name and Laravel .env hints for a service.
type serviceInfo struct {
	envVars []string
}

var serviceEnvVars = map[string]serviceInfo{
	// sqlite is not a podman service — it's a per-project file. Listed here so
	// the env writer can apply Laravel's standard sqlite settings when the user
	// explicitly chooses sqlite in the wizard or runtime prompt.
	"sqlite": {envVars: []string{
		"DB_CONNECTION=sqlite",
		"DB_DATABASE=database/database.sqlite",
	}},
	"mysql": {envVars: []string{
		"DB_CONNECTION=mysql",
		"DB_HOST=lerd-mysql",
		"DB_PORT=3306",
		"DB_DATABASE=lerd",
		"DB_USERNAME=root",
		"DB_PASSWORD=lerd",
	}},
	"postgres": {envVars: []string{
		"DB_CONNECTION=pgsql",
		"DB_HOST=lerd-postgres",
		"DB_PORT=5432",
		"DB_DATABASE=lerd",
		"DB_USERNAME=postgres",
		"DB_PASSWORD=lerd",
	}},
	"redis": {envVars: []string{
		"REDIS_HOST=lerd-redis",
		"REDIS_PORT=6379",
		"REDIS_PASSWORD=null",
		"CACHE_STORE=redis",
		"SESSION_DRIVER=redis",
		"QUEUE_CONNECTION=redis",
	}},
	"meilisearch": {envVars: []string{
		"SCOUT_DRIVER=meilisearch",
		"MEILISEARCH_HOST=http://lerd-meilisearch:7700",
	}},
	"rustfs": {envVars: []string{
		"FILESYSTEM_DISK=s3",
		"AWS_ACCESS_KEY_ID=lerd",
		"AWS_SECRET_ACCESS_KEY=lerdpassword",
		"AWS_DEFAULT_REGION=us-east-1",
		"AWS_BUCKET=lerd",
		"AWS_URL=http://localhost:9000",
		"AWS_ENDPOINT=http://lerd-rustfs:9000",
		"AWS_USE_PATH_STYLE_ENDPOINT=true",
	}},
	"mailpit": {envVars: []string{
		"MAIL_MAILER=smtp",
		"MAIL_HOST=lerd-mailpit",
		"MAIL_PORT=1025",
		"MAIL_USERNAME=null",
		"MAIL_PASSWORD=null",
		"MAIL_ENCRYPTION=null",
	}},
}

// isKnownService returns true if name is a built-in service.
func isKnownService(name string) bool {
	for _, s := range knownServices {
		if s == name {
			return true
		}
	}
	return false
}

// NewServiceCmd returns the service command with subcommands.
func NewServiceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service",
		Short: "Manage Lerd services (mysql, redis, postgres, meilisearch, rustfs, mailpit)",
	}

	cmd.AddCommand(newServiceStartCmd())
	cmd.AddCommand(newServiceStopCmd())
	cmd.AddCommand(newServiceRestartCmd())
	cmd.AddCommand(newServiceStatusCmd())
	cmd.AddCommand(newServiceListCmd())
	cmd.AddCommand(newServiceAddCmd())
	cmd.AddCommand(newServicePresetCmd())
	cmd.AddCommand(newServiceRemoveCmd())
	cmd.AddCommand(newServiceExposeCmd())
	cmd.AddCommand(newServicePinCmd())
	cmd.AddCommand(newServiceUnpinCmd())

	return cmd
}

func newServiceStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start <service>",
		Short: "Start a service",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			unit := "lerd-" + name

			var image string
			if isKnownService(name) {
				if err := ensureServiceQuadlet(name); err != nil {
					return err
				}
				image = podman.ServiceImage("lerd-" + name)
			} else {
				svc, loadErr := config.LoadCustomService(name)
				if loadErr != nil {
					return fmt.Errorf("unknown service %q", name)
				}
				if err := ensureCustomServiceQuadlet(svc); err != nil {
					return err
				}
				// Make sure every declared dependency is up first. Without
				// this, starting e.g. mongo-express by itself would leave
				// mongo stopped and the container would fail to connect.
				if err := StartServiceDependencies(svc); err != nil {
					return err
				}
				image = svc.Image
			}

			if image != "" && !podman.ImageExists(image) {
				jobs := []BuildJob{{
					Label: "Pulling " + name,
					Run:   func(w io.Writer) error { return podman.PullImageTo(image, w) },
				}}
				if err := RunParallel(jobs); err != nil {
					return fmt.Errorf("pulling image: %w", err)
				}
			}

			fmt.Printf("Starting %s...\n", unit)
			if err := podman.StartUnit(unit); err != nil {
				return err
			}
			_ = config.SetServicePaused(name, false)
			_ = config.SetServiceManuallyStarted(name, true)

			// Start any custom services that depend on this one.
			for _, dep := range config.CustomServicesDependingOn(name) {
				if err := ensureServiceRunning(dep); err != nil {
					fmt.Printf("  [WARN] could not start dependent service %s: %v\n", dep, err)
				}
			}

			// Restart family consumers (e.g. phpMyAdmin) so they pick up
			// the freshly-started member without DNS / connection caching.
			if fam := serviceops.ServiceFamily(name); fam != "" {
				serviceops.RegenerateFamilyConsumers(fam)
			}

			printEnvVars(name)
			return nil
		},
	}
}

func newServiceStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop <service>",
		Short: "Stop a service",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			StopServiceAndDependents(name)
			_ = config.SetServicePaused(name, true)
			_ = config.SetServiceManuallyStarted(name, false)
			if fam := serviceops.ServiceFamily(name); fam != "" {
				serviceops.RegenerateFamilyConsumers(fam)
			}
			return nil
		},
	}
}

// RegenerateFamilyConsumersForService is the public entry the Web UI uses
// after a start/stop. Forwards to serviceops.
func RegenerateFamilyConsumersForService(name string) {
	serviceops.RegenerateFamilyConsumersForService(name)
}

func newServiceRestartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restart <service>",
		Short: "Restart a service",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			unit := "lerd-" + name
			fmt.Printf("Restarting %s...\n", unit)
			if err := podman.RestartUnit(unit); err != nil {
				return err
			}
			printEnvVars(name)
			return nil
		},
	}
}

func newServiceStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status <service>",
		Short: "Show the status of a service",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			unit := "lerd-" + args[0]
			status, err := podman.UnitStatus(unit)
			if err != nil {
				return err
			}
			note := ""
			if status == "inactive" {
				note = serviceInactiveReason(args[0])
			}
			fmt.Printf("%s: %s%s\n", unit, colorStatus(status), note)
			return nil
		},
	}
}

func newServiceListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all services and their status",
		RunE: func(_ *cobra.Command, _ []string) error {
			fmt.Printf("%-20s %s\n", "Service", "Status")
			fmt.Printf("%s\n", strings.Repeat("─", 32))
			for _, svc := range knownServices {
				unit := "lerd-" + svc
				status, err := podman.UnitStatus(unit)
				if err != nil {
					status = "unknown"
				}
				fmt.Printf("%-20s %s\n", svc, colorStatus(status))
				if status == "inactive" {
					if reason := serviceInactiveReason(svc); reason != "" {
						fmt.Printf("  %s\n", strings.TrimSpace(reason))
					}
				}
			}
			customs, _ := config.ListCustomServices()
			for _, svc := range customs {
				unit := "lerd-" + svc.Name
				status, err := podman.UnitStatus(unit)
				if err != nil {
					status = "unknown"
				}
				fmt.Printf("%-20s %s  [custom]\n", svc.Name, colorStatus(status))
				if status == "inactive" {
					if reason := serviceInactiveReason(svc.Name); reason != "" {
						fmt.Printf("  %s\n", strings.TrimSpace(reason))
					}
				}
				if len(svc.DependsOn) > 0 {
					fmt.Printf("  depends on: %s\n", strings.Join(svc.DependsOn, ", "))
				}
			}
			return nil
		},
	}
}

// newServiceAddCmd returns the `service add` command.
func newServiceAddCmd() *cobra.Command {
	var (
		name          string
		image         string
		ports         []string
		envVars       []string
		containerEnv  []string
		dataDir       string
		detectKey     string
		detectPrefix  string
		description   string
		initExec      string
		initContainer string
		dashboard     string
		dependsOn     []string
	)

	cmd := &cobra.Command{
		Use:   "add [file.yaml]",
		Short: "Define a new custom service (from a YAML file or flags)",
		Long: `Define a new custom service and write its systemd quadlet.

Load from a YAML file:
  lerd service add mongodb.yaml

Or specify inline with flags (--name and --image are required):
  lerd service add --name mongodb --image docker.io/library/mongo:7 --port 27017:27017`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var svc *config.CustomService

			if len(args) == 1 {
				// YAML file mode — load and validate from disk
				loaded, err := config.LoadCustomServiceFromFile(args[0])
				if err != nil {
					return fmt.Errorf("loading %s: %w", args[0], err)
				}
				svc = loaded
			} else {
				// Flag mode — --name and --image are required
				if name == "" {
					return fmt.Errorf("required flag \"name\" not set")
				}
				if image == "" {
					return fmt.Errorf("required flag \"image\" not set")
				}
				svc = &config.CustomService{
					Name:        name,
					Image:       image,
					Ports:       ports,
					DataDir:     dataDir,
					EnvVars:     envVars,
					Dashboard:   dashboard,
					Description: description,
					DependsOn:   dependsOn,
				}
				if len(containerEnv) > 0 {
					svc.Environment = make(map[string]string, len(containerEnv))
					for _, kv := range containerEnv {
						k, v, _ := strings.Cut(kv, "=")
						svc.Environment[k] = v
					}
				}
				if detectKey != "" {
					svc.EnvDetect = &config.EnvDetect{
						Key:         detectKey,
						ValuePrefix: detectPrefix,
					}
				}
				if initExec != "" {
					svc.SiteInit = &config.SiteInit{
						Container: initContainer,
						Exec:      initExec,
					}
				}
			}

			if isKnownService(svc.Name) {
				return fmt.Errorf("%q is a built-in service and cannot be redefined", svc.Name)
			}
			if _, err := config.LoadCustomService(svc.Name); err == nil {
				return fmt.Errorf("custom service %q already exists; remove it first with: lerd service remove %s", svc.Name, svc.Name)
			}

			if err := config.SaveCustomService(svc); err != nil {
				return fmt.Errorf("saving service config: %w", err)
			}
			if err := ensureCustomServiceQuadlet(svc); err != nil {
				return fmt.Errorf("writing quadlet: %w", err)
			}
			fmt.Printf("Custom service %q added. Start it with: lerd service start %s\n", svc.Name, svc.Name)
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Service name (slug: [a-z0-9-])")
	cmd.Flags().StringVar(&image, "image", "", "OCI image (e.g. docker.io/library/mongo:7)")
	cmd.Flags().StringArrayVar(&ports, "port", nil, "Port mapping host:container (repeatable)")
	cmd.Flags().StringArrayVar(&containerEnv, "env", nil, "Container environment variable KEY=VALUE (repeatable)")
	cmd.Flags().StringArrayVar(&envVars, "env-var", nil, ".env variable KEY=VALUE injected by `lerd env` (repeatable)")
	cmd.Flags().StringVar(&dataDir, "data-dir", "", "Mount path inside container for persistent data (host dir auto-created)")
	cmd.Flags().StringVar(&detectKey, "detect-key", "", "Env key for auto-detection in `lerd env`")
	cmd.Flags().StringVar(&detectPrefix, "detect-prefix", "", "Value prefix filter for auto-detection (optional)")
	cmd.Flags().StringVar(&description, "description", "", "Human-readable description")
	cmd.Flags().StringVar(&dashboard, "dashboard", "", "URL to open when clicking the dashboard button in the web UI")
	cmd.Flags().StringVar(&initExec, "init-exec", "", "Shell command to run inside the container once per site (supports {{site}} and {{site_testing}})")
	cmd.Flags().StringVar(&initContainer, "init-container", "", "Container to run --init-exec in (default: lerd-<name>)")
	cmd.Flags().StringArrayVar(&dependsOn, "depends-on", nil, "Service name that must be running before this service (repeatable)")

	return cmd
}

// newServicePresetCmd returns the `service preset` command.
func newServicePresetCmd() *cobra.Command {
	var version string
	cmd := &cobra.Command{
		Use:   "preset [name]",
		Short: "Install a bundled service preset (e.g. phpmyadmin, pgadmin)",
		Long: `Install a bundled, opt-in service preset.

Run with no arguments to list the available presets:
  lerd service preset

Install a preset by name:
  lerd service preset phpmyadmin

Pick a specific version on multi-version presets like mysql or postgres.
When --version is omitted on a multi-version preset and the terminal is
interactive, lerd prompts for the version:
  lerd service preset mysql --version 5.7
  lerd service preset mysql           # interactive picker

Presets are installed as ordinary custom services. They can then be started,
stopped, removed, exposed, or pinned with the usual service subcommands.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 {
				return printPresetList()
			}
			name := args[0]
			pickedVersion := version
			if pickedVersion == "" {
				if loaded, err := config.LoadPreset(name); err == nil && len(loaded.Versions) > 0 {
					if isInteractive() {
						pickedVersion, err = promptPresetVersion(loaded)
						if err != nil {
							return err
						}
					}
				}
			}
			svc, err := InstallPresetByName(name, pickedVersion)
			if err != nil {
				return err
			}
			fmt.Printf("Installed preset %q. Start it with: lerd service start %s\n", svc.Name, svc.Name)
			if svc.Dashboard != "" {
				fmt.Printf("Dashboard: %s\n", svc.Dashboard)
			}
			if len(svc.DependsOn) > 0 {
				fmt.Printf("Depends on: %s (will be auto-started)\n", strings.Join(svc.DependsOn, ", "))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&version, "version", "", "Pick a specific version for multi-version presets (e.g. 5.7)")
	return cmd
}

// promptPresetVersion shows an interactive picker for the versions a
// multi-version preset offers, defaulting to the preset's DefaultVersion and
// excluding any version tag that's already installed locally.
func promptPresetVersion(p *config.Preset) (string, error) {
	options := make([]huh.Option[string], 0, len(p.Versions))
	for _, v := range p.Versions {
		svcName := p.Name + "-" + config.SanitizeImageTag(v.Tag)
		label := v.Label
		if label == "" {
			label = v.Tag
		}
		if _, err := config.LoadCustomService(svcName); err == nil {
			label += " (already installed)"
		}
		options = append(options, huh.NewOption(label, v.Tag))
	}
	if len(options) == 0 {
		return "", fmt.Errorf("preset %s has no versions", p.Name)
	}
	picked := p.DefaultVersion
	if picked == "" {
		picked = p.Versions[0].Tag
	}
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title(fmt.Sprintf("Which %s version do you want to install?", p.Name)).
			Options(options...).
			Value(&picked),
	)).WithTheme(huh.ThemeCatppuccin())
	if err := form.Run(); err != nil {
		return "", err
	}
	return picked, nil
}

// printPresetList prints the bundled presets in a simple table.
func printPresetList() error {
	presets, err := config.ListPresets()
	if err != nil {
		return err
	}
	if len(presets) == 0 {
		fmt.Println("No presets bundled with this build.")
		return nil
	}
	fmt.Printf("%-14s %-10s %s\n", "Preset", "Status", "Description")
	fmt.Printf("%s\n", strings.Repeat("─", 60))
	for _, p := range presets {
		status := "available"
		if len(p.Versions) == 0 {
			if _, err := config.LoadCustomService(p.Name); err == nil {
				status = "installed"
			}
		} else {
			anyInstalled := false
			for _, v := range p.Versions {
				if _, err := config.LoadCustomService(p.Name + "-" + config.SanitizeImageTag(v.Tag)); err == nil {
					anyInstalled = true
					break
				}
			}
			if anyInstalled {
				status = "installed"
			}
		}
		fmt.Printf("%-14s %-10s %s\n", p.Name, status, p.Description)
		if len(p.DependsOn) > 0 {
			fmt.Printf("%-14s %-10s depends on: %s\n", "", "", strings.Join(p.DependsOn, ", "))
		}
		if p.Dashboard != "" {
			fmt.Printf("%-14s %-10s dashboard:  %s\n", "", "", p.Dashboard)
		}
		for _, v := range p.Versions {
			versionStatus := "available"
			label := v.Tag
			if v.Label != "" {
				label = v.Label
			}
			if _, err := config.LoadCustomService(p.Name + "-" + config.SanitizeImageTag(v.Tag)); err == nil {
				versionStatus = "installed"
			}
			marker := " "
			if v.Tag == p.DefaultVersion {
				marker = "*"
			}
			fmt.Printf("%-14s %-10s %s %-9s %-13s %s\n", "", "", marker, versionStatus, v.Tag, label)
		}
	}
	fmt.Println("\n* = default version")
	fmt.Println("Install with: lerd service preset <name> [--version <tag>]")
	return nil
}

// InstallPresetByName is a thin wrapper around serviceops.InstallPresetByName
// kept for the existing call sites in cli (init wizard, link, web UI handler).
func InstallPresetByName(name, version string) (*config.CustomService, error) {
	return serviceops.InstallPresetByName(name, version)
}

// MissingPresetDependencies is a thin wrapper around the serviceops helper.
func MissingPresetDependencies(svc *config.CustomService) []string {
	return serviceops.MissingPresetDependencies(svc)
}

// newServiceRemoveCmd returns the `service remove` command.
func newServiceRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <service>",
		Short: "Stop and remove a custom service",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]

			if isKnownService(name) {
				return fmt.Errorf("%q is a built-in service and cannot be removed", name)
			}

			// Capture the family before deletion so consumers can be regenerated.
			var family string
			if existing, err := config.LoadCustomService(name); err == nil {
				family = existing.Family
			}

			unit := "lerd-" + name

			// Stop the unit if it is running.
			status, _ := podman.UnitStatus(unit)
			if status == "active" || status == "activating" {
				fmt.Printf("Stopping %s...\n", unit)
				if err := podman.StopUnit(unit); err != nil {
					return fmt.Errorf("could not stop %s: %w\nRemove aborted — the service is still running", unit, err)
				}
			}
			podman.RemoveContainer(unit)

			if err := podman.RemoveQuadlet(unit); err != nil {
				fmt.Printf("  WARN: could not remove quadlet: %v\n", err)
			}
			if err := podman.DaemonReloadFn(); err != nil {
				fmt.Printf("  WARN: daemon-reload failed: %v\n", err)
			}

			if err := config.RemoveCustomService(name); err != nil {
				return fmt.Errorf("removing service config: %w", err)
			}

			if family != "" {
				serviceops.RegenerateFamilyConsumers(family)
			}

			dataPath := config.DataSubDir(name)
			fmt.Printf("Removed service %q.\n", name)
			fmt.Printf("Data at %s was NOT removed. Delete it manually if no longer needed.\n", dataPath)
			return nil
		},
	}
}

// migrateServiceUnits rewrites unit files for all globally configured services.
// This ensures BindForLAN and other install-time settings are applied even for
// services that already have a unit file on disk.
func migrateServiceUnits() {
	for _, svc := range knownServices {
		ensureServiceQuadlet(svc) //nolint:errcheck
	}
	customs, _ := config.ListCustomServices()
	for _, svc := range customs {
		ensureCustomServiceQuadlet(svc) //nolint:errcheck
	}
}

// ensureServiceQuadlet writes the unit file for a known service and reloads the service manager.
func ensureServiceQuadlet(name string) error {
	quadletName := "lerd-" + name
	content, err := podman.GetQuadletTemplate(quadletName + ".container")
	if err != nil {
		return fmt.Errorf("unknown service %q", name)
	}
	if cfg, loadErr := config.LoadGlobal(); loadErr == nil {
		if svcCfg, ok := cfg.Services[name]; ok {
			content = podman.ApplyImage(content, svcCfg.Image)
			if len(svcCfg.ExtraPorts) > 0 {
				content = podman.ApplyExtraPorts(content, svcCfg.ExtraPorts)
			}
		}
	}
	// Platform override applied last so it wins over the global config image.
	// The override only fires when the resolved image is a known-bad one for
	// this platform (e.g. postgis/postgis alpine has no ARM64 manifest on macOS).
	if currentImage := podman.CurrentImage(content); currentImage != "" {
		if override := platformImageOverride(name, currentImage); override != "" {
			content = podman.ApplyImage(content, override)
		}
	}
	if err := podman.WriteContainerUnitFn(quadletName, content); err != nil {
		return fmt.Errorf("writing unit for %s: %w", name, err)
	}
	return podman.DaemonReloadFn()
}

// ensureCustomServiceQuadlet defers to serviceops so the CLI and the MCP
// tools generate identical quadlets.
func ensureCustomServiceQuadlet(svc *config.CustomService) error {
	return serviceops.EnsureCustomServiceQuadlet(svc)
}

// newServiceExposeCmd returns the `service expose` command.
func newServiceExposeCmd() *cobra.Command {
	var remove bool
	cmd := &cobra.Command{
		Use:   "expose <service> <host:container>",
		Short: "Add (or remove) an extra published port on a built-in service",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			name, port := args[0], args[1]
			if !isKnownService(name) {
				return fmt.Errorf("%q is not a built-in service", name)
			}
			cfg, err := config.LoadGlobal()
			if err != nil {
				return err
			}
			svcCfg := cfg.Services[name]
			if remove {
				svcCfg.ExtraPorts = removePort(svcCfg.ExtraPorts, port)
			} else {
				if !containsPort(svcCfg.ExtraPorts, port) {
					svcCfg.ExtraPorts = append(svcCfg.ExtraPorts, port)
				}
			}
			cfg.Services[name] = svcCfg
			if err := config.SaveGlobal(cfg); err != nil {
				return err
			}
			if err := ensureServiceQuadlet(name); err != nil {
				return err
			}
			status, _ := podman.UnitStatus("lerd-" + name)
			if status == "active" {
				fmt.Printf("Restarting lerd-%s to apply port changes...\n", name)
				_ = podman.RestartUnit("lerd-" + name)
			}
			if remove {
				fmt.Printf("Removed extra port %s from %s.\n", port, name)
			} else {
				fmt.Printf("Added extra port %s to %s.\n", port, name)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&remove, "remove", false, "Remove the port mapping instead of adding it")
	return cmd
}

func containsPort(ports []string, port string) bool {
	for _, p := range ports {
		if p == port {
			return true
		}
	}
	return false
}

func removePort(ports []string, port string) []string {
	out := ports[:0]
	for _, p := range ports {
		if p != port {
			out = append(out, p)
		}
	}
	return out
}

// printEnvVars prints the recommended .env variables for a service.
func printEnvVars(name string) {
	info, ok := serviceEnvVars[name]
	if ok && len(info.envVars) > 0 {
		fmt.Println("\nAdd to your .env:")
		for _, v := range info.envVars {
			fmt.Println(v)
		}
		fmt.Println()
		return
	}
	// Fall back to custom service env_vars
	svc, err := config.LoadCustomService(name)
	if err != nil || len(svc.EnvVars) == 0 {
		return
	}
	fmt.Println("\nAdd to your .env:")
	for _, v := range svc.EnvVars {
		fmt.Println(v)
	}
	fmt.Println()
}

func newServicePinCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pin <service>",
		Short: "Pin a service so it is never auto-stopped",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			if err := config.SetServicePinned(name, true); err != nil {
				return err
			}
			fmt.Printf("Pinned %s — it will not be auto-stopped when no sites use it.\n", name)
			if err := ensureServiceRunning(name); err != nil {
				fmt.Printf("  [WARN] could not start %s: %v\n", name, err)
			}
			return nil
		},
	}
}

func newServiceUnpinCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unpin <service>",
		Short: "Unpin a service so it can be auto-stopped when unused",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			if err := config.SetServicePinned(name, false); err != nil {
				return err
			}
			fmt.Printf("Unpinned %s — it will be auto-stopped when no sites use it.\n", name)
			return nil
		},
	}
}

// StartServiceDependencies and StopServiceAndDependents are thin wrappers so
// the Web UI can share the same semantics as the CLI.
func StartServiceDependencies(svc *config.CustomService) error {
	return serviceops.StartDependencies(svc)
}

func StopServiceAndDependents(name string) {
	serviceops.StopWithDependents(name)
}

// autoStopUnusedServices stops any running service that has no active sites
// referencing it and was not manually started by the user.
func autoStopUnusedServices() {
	candidates := make([]string, len(knownServices))
	copy(candidates, knownServices)
	if customs, err := config.ListCustomServices(); err == nil {
		for _, c := range customs {
			candidates = append(candidates, c.Name)
		}
	}
	for _, name := range candidates {
		if config.CountSitesUsingService(name) == 0 && !config.ServiceIsManuallyStarted(name) && !config.ServiceIsPinned(name) {
			unit := "lerd-" + name
			status, _ := podman.UnitStatus(unit)
			if status == "active" || status == "activating" {
				StopServiceAndDependents(name)
			}
		}
	}
}

// activePHPVersions returns the set of PHP versions actually in use by
// non-ignored, non-paused sites, using live disk detection (.php-version file)
// with the stored registry value as fallback.
func activePHPVersions() map[string]bool {
	reg, err := config.LoadSites()
	if err != nil {
		return nil
	}
	active := make(map[string]bool)
	for _, s := range reg.Sites {
		if s.Ignored {
			continue
		}
		phpMin, phpMax := "", ""
		if s.Framework != "" {
			if fw, fwOk := config.GetFrameworkForDir(s.Framework, s.Path); fwOk {
				phpMin, phpMax = fw.PHP.Min, fw.PHP.Max
			}
		}
		v := phpPkg.DetectVersionClamped(s.Path, phpMin, phpMax, s.PHPVersion)
		if v != "" {
			active[v] = true
		}
	}
	return active
}

// autoStopUnusedFPMs stops any PHP-FPM container whose PHP version is no longer
// referenced by any active (non-ignored, non-paused) site.
func autoStopUnusedFPMs() {
	versions, err := phpPkg.ListInstalled()
	if err != nil {
		return
	}
	active := activePHPVersions()
	for _, v := range versions {
		if active[v] {
			continue
		}
		unit := "lerd-php" + strings.ReplaceAll(v, ".", "") + "-fpm"
		status, _ := podman.UnitStatus(unit)
		if status == "active" || status == "activating" {
			if err := podman.StopUnit(unit); err != nil {
				fmt.Printf("[WARN] stopping %s: %v\n", unit, err)
			}
		}
	}
}

// serviceInactiveReason returns an extra note for an inactive service explaining
// why it is stopped, if the reason is that no sites are using it.
func serviceInactiveReason(name string) string {
	if config.CountSitesUsingService(name) == 0 {
		return " (no sites using this service)"
	}
	return ""
}

// colorStatus returns an ANSI-colored status string.
func colorStatus(status string) string {
	switch status {
	case "active":
		return "\033[32m" + status + "\033[0m"
	case "inactive":
		return "\033[33m" + status + "\033[0m"
	case "failed":
		return "\033[31m" + status + "\033[0m"
	default:
		return status
	}
}
