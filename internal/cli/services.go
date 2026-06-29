package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/feedback"
	phpPkg "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/serviceops"
	"github.com/geodro/lerd/internal/services"
	"github.com/spf13/cobra"
)

// knownServices returns the default-preset service names (mysql, redis, etc.).
// Backed by the preset YAMLs so adding a default preset surfaces here automatically.
func knownServices() []string { return config.DefaultPresetNames() }

// sqliteEnvVars are the Laravel-standard env values for the sqlite "service"
// (which isn't a podman container — just a per-project file). Kept hardcoded
// because there's no preset YAML to host it: sqlite has no image, no port,
// and no install flow.
var sqliteEnvVars = []string{
	"DB_CONNECTION=sqlite",
	"DB_DATABASE=database/database.sqlite",
}

// serviceEnvVars returns the recommended Laravel .env KEY=VALUE pairs for a
// default-preset service or sqlite. Returns nil for any other name.
func serviceEnvVars(name string) []string {
	if name == "sqlite" {
		return sqliteEnvVars
	}
	return config.DefaultPresetEnvVars(name)
}

// isKnownService returns true if name is a default-preset service.
func isKnownService(name string) bool { return config.IsDefaultPreset(name) }

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
	cmd.AddCommand(newServiceUpdateCmd())
	cmd.AddCommand(newServiceRollbackCmd())
	cmd.AddCommand(newServiceMigrateCmd())
	cmd.AddCommand(newServiceRemoveCmd())
	cmd.AddCommand(newServiceReinstallCmd())
	cmd.AddCommand(newServiceConfigCmd())
	cmd.AddCommand(newServiceExposeCmd())
	cmd.AddCommand(newServicePinCmd())
	cmd.AddCommand(newServiceUnpinCmd())
	cmd.AddCommand(newServicePortCmd())

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

			feedback.Begin()
			svcStep := feedback.Start("starting " + unit)
			if err := podman.StartUnit(unit); err != nil {
				svcStep.Fail(err)
				return err
			}
			svcStep.OK("")
			_ = config.SetServicePaused(name, false)
			_ = config.SetServiceManuallyStarted(name, true)

			// Start any custom services that depend on this one.
			for _, dep := range config.CustomServicesDependingOn(name) {
				if err := ensureServiceRunning(dep); err != nil {
					feedback.Warn("could not start dependent service %s: %v", dep, err)
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
			feedback.Begin()
			// StopServiceAndDependents emits its own "stopping <service>" step
			// (per service in the dependency cascade), so don't wrap it here.
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

// serviceUpdateHint queries the registry for an update / upgrade tag and
// returns a short, colorised hint suitable for the list view's Update column.
// Returns "" for inactive services or when nothing is available.
func serviceUpdateHint(name, status string) string {
	if status != "active" {
		return ""
	}
	avail, err := serviceops.CheckUpdateAvailable(name)
	if err != nil || avail == nil {
		return ""
	}
	parts := []string{}
	if avail.Available && avail.LatestTag != "" {
		parts = append(parts, feedback.Green("→ "+avail.LatestTag))
	}
	if avail.UpgradeTag != "" {
		parts = append(parts, feedback.Amber("⇧ "+avail.UpgradeTag))
	}
	return strings.Join(parts, " ")
}

func newServiceUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update <service> [tag]",
		Short: "Pull a newer image for a service and restart it",
		Long: `Pull a newer image for a service and restart it.

With no tag argument, applies the safe in-strategy update (e.g. mysql 8.4 → 8.4.9).
Pass an explicit tag to opt into the cross-strategy upgrade (e.g. meilisearch
v1.7.6 → v1.42.1) — this may require manual data migration; you've been warned.`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			targetImage := ""
			if len(args) == 2 {
				avail, err := serviceops.CheckUpdateAvailable(name)
				if err != nil {
					return err
				}
				if avail.CurrentImage == "" {
					return fmt.Errorf("could not resolve current image for %q", name)
				}
				if at := strings.LastIndex(avail.CurrentImage, ":"); at > 0 {
					targetImage = avail.CurrentImage[:at] + ":" + args[1]
				} else {
					targetImage = avail.CurrentImage + ":" + args[1]
				}
			}
			emit := func(ev serviceops.PhaseEvent) {
				switch ev.Phase {
				case "checking_registry":
					feedback.Line("checking registry")
				case "pulling_image":
					if ev.Message != "" {
						feedback.Note(strings.TrimSpace(ev.Message))
					} else if ev.Image != "" {
						feedback.Line("pulling " + ev.Image)
					}
				case "writing_quadlet":
					feedback.Line("writing quadlet for " + ev.Image)
				case "restarting_unit":
					feedback.Line("restarting " + ev.Unit)
				case "done":
					if ev.Message != "" {
						feedback.Done(ev.Message)
					} else {
						feedback.Done("now on " + feedback.Val(ev.Image))
					}
				}
			}
			feedback.Begin()
			return serviceops.UpdateServiceStreaming(name, targetImage, emit)
		},
	}
	return cmd
}

func newServiceMigrateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "migrate <service> <version>",
		Short: "Migrate a service across data-incompatible versions (dump + restore)",
		Long: `Migrate a service across versions whose on-disk data dirs are NOT compatible
(e.g. mysql 8.x → 9.x, postgres 16 → 17, meilisearch v1.x → v1.y across minors).

<version> is a preset version label such as 18, resolved to that version's
image (postgres 18 becomes postgis/postgis:18-3.6-alpine). An argument that
matches no preset version is used verbatim as the target image tag.

Flow: dump current data into ~/.local/share/lerd/backups/<svc>-<ts>.sql, stop
the unit, move the data dir aside, pull the target image, start fresh, restore
the dump. The old data dir is preserved alongside the dump so manual recovery
is always possible.

Supported families: mysql, mariadb, postgres.`,
		Args: cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			name, version := args[0], args[1]
			avail, err := serviceops.CheckUpdateAvailable(name)
			if err != nil {
				return err
			}
			if avail.CurrentImage == "" {
				return fmt.Errorf("could not resolve current image for %q", name)
			}
			targetImage, err := serviceops.ResolveMigrateTarget(name, avail.CurrentImage, version)
			if err != nil {
				return err
			}
			feedback.Begin()
			feedback.Line("migrating " + name + ": " + avail.CurrentImage + " → " + feedback.Val(targetImage))
			feedback.Note("dumps and the previous data dir are kept under ~/.local/share/lerd/backups")
			emit := func(ev serviceops.PhaseEvent) {
				switch ev.Phase {
				case "dumping_data":
					feedback.Line("dumping: " + ev.Message)
				case "stopping_unit":
					feedback.Line("stopping " + ev.Unit)
				case "swapping_data_dir":
					feedback.Line("moving data dir aside")
				case "pulling_image":
					if ev.Message != "" {
						feedback.Note(strings.TrimSpace(ev.Message))
					} else if ev.Image != "" {
						feedback.Line("pulling " + ev.Image)
					}
				case "writing_quadlet":
					feedback.Line("writing quadlet for " + ev.Image)
				case "restarting_unit":
					feedback.Line("restarting " + ev.Unit)
				case "waiting_ready":
					feedback.Line("waiting for the new container to be ready")
				case "restoring_data":
					feedback.Line("restoring " + ev.Message)
				case "done":
					if ev.Message != "" {
						feedback.Note(ev.Message)
					}
					feedback.Done("migration complete")
				}
			}
			return serviceops.MigrateService(name, targetImage, emit)
		},
	}
}

func newServiceRollbackCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rollback <service>",
		Short: "Roll back a service to its previously-running image",
		Long: `Roll back a service to whatever it was running before the last update.
Toggling: a second rollback returns to the original (so you can flip-flop).
Errors when no previous image is recorded — i.e. the service was never updated.`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			emit := func(ev serviceops.PhaseEvent) {
				switch ev.Phase {
				case "pulling_image":
					if ev.Message != "" {
						feedback.Note(strings.TrimSpace(ev.Message))
					} else if ev.Image != "" {
						feedback.Line("pulling " + ev.Image)
					}
				case "writing_quadlet":
					feedback.Line("writing quadlet for " + ev.Image)
				case "restarting_unit":
					feedback.Line("restarting " + ev.Unit)
				case "done":
					feedback.Done("rolled back to " + feedback.Val(ev.Image))
				}
			}
			feedback.Begin()
			return serviceops.RollbackService(name, emit)
		},
	}
}

func newServiceRestartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restart <service>",
		Short: "Restart a service",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			unit := "lerd-" + name
			// Refresh the quadlet first so config edits and preset file
			// mounts land before the unit picks them up. If regen fails,
			// warn and restart the existing quadlet — failing here would
			// strand a healthy unit on a transient render error.
			if isKnownService(name) {
				if err := ensureServiceQuadlet(name); err != nil {
					fmt.Fprintf(os.Stderr, "[WARN] regenerating quadlet for %s failed: %v; restarting with the existing one\n", name, err)
				}
			} else if svc, err := config.LoadCustomService(name); err == nil {
				if err := ensureCustomServiceQuadlet(svc); err != nil {
					fmt.Fprintf(os.Stderr, "[WARN] regenerating quadlet for %s failed: %v; restarting with the existing one\n", name, err)
				}
			}
			feedback.Begin()
			svcStep := feedback.Start("restarting " + unit)
			if err := podman.RestartUnit(unit); err != nil {
				svcStep.Fail(err)
				return err
			}
			svcStep.OK("")
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
			var rows [][]string
			statusCell := func(name, status string) string {
				cell := colorStatus(status)
				if status == "inactive" {
					if reason := serviceInactiveReason(name); reason != "" {
						cell += feedback.Dim(reason)
					}
				}
				return cell
			}
			for _, svc := range knownServices() {
				unit := "lerd-" + svc
				status, err := podman.UnitStatus(unit)
				if err != nil {
					status = "unknown"
				}
				ver := podman.ServiceVersionLabel(podman.InstalledImage(unit))
				rows = append(rows, []string{svc, ver, statusCell(svc, status), serviceUpdateHint(svc, status)})
			}
			customs, _ := config.ListCustomServices()
			for _, svc := range customs {
				unit := "lerd-" + svc.Name
				status, err := podman.UnitStatus(unit)
				if err != nil {
					status = "unknown"
				}
				ver := podman.ServiceVersionLabel(svc.Image)
				name := svc.Name + " " + feedback.Dim("[custom]")
				rows = append(rows, []string{name, ver, statusCell(svc.Name, status), serviceUpdateHint(svc.Name, status)})
				if len(svc.DependsOn) > 0 {
					rows = append(rows, []string{feedback.Dim("↳ depends on: " + strings.Join(svc.DependsOn, ", ")), "", "", ""})
				}
			}
			feedback.Table([]string{"Service", "Version", "Status", "Update"}, rows)
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
			if serviceops.ServiceInstalled(svc.Name) {
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
		label := v.Label
		if label == "" {
			label = v.Tag
		}
		if serviceops.ServiceInstalled(config.PresetVersionServiceName(p.Name, v)) {
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
	var rows [][]string
	for _, p := range presets {
		status := "available"
		if len(p.Versions) == 0 {
			if serviceops.ServiceInstalled(p.Name) {
				status = "installed"
			}
		} else {
			for _, v := range p.Versions {
				if serviceops.ServiceInstalled(config.PresetVersionServiceName(p.Name, v)) {
					status = "installed"
					break
				}
			}
		}
		rows = append(rows, []string{p.Name, status, p.Description})
		if len(p.DependsOn) > 0 {
			rows = append(rows, []string{"", "", feedback.Dim("depends on: " + strings.Join(p.DependsOn, ", "))})
		}
		if p.Dashboard != "" {
			rows = append(rows, []string{"", "", feedback.Dim("dashboard: " + p.Dashboard)})
		}
		for _, v := range p.Versions {
			versionStatus := "available"
			label := v.Tag
			if v.Label != "" {
				label = v.Label
			}
			if serviceops.ServiceInstalled(config.PresetVersionServiceName(p.Name, v)) {
				versionStatus = "installed"
			}
			name := "↳ " + v.Tag
			if v.Tag == p.DefaultVersion {
				name = "↳ * " + v.Tag
			}
			rows = append(rows, []string{name, versionStatus, label})
		}
	}
	feedback.Table([]string{"Preset", "Status", "Description"}, rows)
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
	var purge bool
	cmd := &cobra.Command{
		Use:   "remove <service>",
		Short: "Stop and remove a service (custom or default)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]

			feedback.Begin()
			if purge {
				feedback.Note("removing service " + name + " and ALL its data at " + config.DataSubDir(name))
			}

			emit := func(e serviceops.PhaseEvent) {
				switch e.Phase {
				case "stopping_unit":
					feedback.Line("stopping " + e.Unit)
				case "removing_data":
					feedback.Line("renaming data dir aside: " + e.Message)
				case "done":
					feedback.Done("removed service " + feedback.Val(name))
				}
			}

			if err := serviceops.RemoveService(name, serviceops.RemoveOptions{RemoveData: purge}, emit); err != nil {
				return err
			}

			if !purge {
				fmt.Printf("Data at %s was NOT removed. Pass --purge to wipe it.\n", config.DataSubDir(name))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&purge, "purge", false, "also rename the service data dir aside (recoverable as <dir>.pre-remove-<ts>)")
	return cmd
}

// newServiceReinstallCmd returns the `service reinstall` command. Stops,
// removes, and reinstalls the same version. With --reset-data the data dir
// is renamed-aside and per-site DBs/buckets are recreated on the fresh service.
func newServiceReinstallCmd() *cobra.Command {
	var resetData bool
	cmd := &cobra.Command{
		Use:   "reinstall <service>",
		Short: "Stop, remove, and reinstall a service in place",
		Long:  "Reinstall the service at its current version. With --reset-data the data dir is renamed-aside (recoverable) and any linked sites' databases or buckets are recreated on the fresh service.",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			feedback.Begin()
			emit := func(e serviceops.PhaseEvent) {
				switch e.Phase {
				case "reinstall_starting":
					feedback.Line("reinstalling " + name)
				case "stopping_unit":
					feedback.Note("stopping " + e.Unit)
				case "removing_data":
					feedback.Note("renaming data dir aside: " + e.Message)
				case "pulling_image":
					if e.Message == "" && e.Image != "" {
						feedback.Note("pulling " + e.Image)
					}
				case "starting_unit":
					feedback.Note("starting " + e.Unit)
				case "waiting_ready":
					feedback.Note("waiting for " + e.Unit + " to be ready")
				case "reprovisioning_sites":
					feedback.Note("reprovisioning linked sites: " + e.Message)
				case "reprovisioning_site":
					feedback.Note(e.Message)
				case "reprovisioning_skipped":
					feedback.Note("reprovisioning skipped: " + e.Message)
				}
			}
			if err := serviceops.ReinstallService(name, resetData, emit); err != nil {
				return err
			}
			fmt.Printf("Reinstalled %q.\n", name)
			return nil
		},
	}
	cmd.Flags().BoolVar(&resetData, "reset-data", false, "wipe data dir (rename-aside) and recreate linked-site databases/buckets on the fresh service")
	return cmd
}

// migrateServiceUnits rewrites unit files for all globally configured services.
// This ensures BindForLAN and other install-time settings are applied even for
// services that already have a unit file on disk. Default presets are skipped
// when no quadlet exists for them (i.e. the user removed the service); without
// this skip a subsequent CLI invocation would silently recreate the quadlet.
func migrateServiceUnits() {
	for _, svc := range knownServices() {
		if !podman.QuadletInstalled("lerd-" + svc) {
			continue
		}
		ensureServiceQuadlet(svc) //nolint:errcheck
	}
	customs, _ := config.ListCustomServices()
	for _, svc := range customs {
		ensureCustomServiceQuadlet(svc) //nolint:errcheck
	}
}

// ensureServiceQuadlet writes the unit file for a default-preset service and
// reloads the service manager. Delegates to serviceops so install + runtime +
// MCP all generate byte-identical quadlets.
func ensureServiceQuadlet(name string) error {
	return serviceops.EnsureDefaultPresetQuadlet(name)
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
			if remove {
				if err := serviceops.RemoveExtraPort(name, port); err != nil {
					return err
				}
				fmt.Printf("Removed extra port %s from %s.\n", port, name)
				return nil
			}
			if err := serviceops.AddExtraPort(name, port); err != nil {
				return err
			}
			fmt.Printf("Added extra port %s to %s.\n", port, name)
			return nil
		},
	}
	cmd.Flags().BoolVar(&remove, "remove", false, "Remove the port mapping instead of adding it")
	return cmd
}

// printEnvVars prints the recommended .env variables for a service.
func printEnvVars(name string) {
	if envs := serviceEnvVars(name); len(envs) > 0 {
		fmt.Println("\nAdd to your .env:")
		for _, v := range envs {
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
				feedback.Warn("could not start %s: %v", name, err)
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

// newServicePortCmd returns the `service port` command, which moves a service's
// published host port (e.g. lerd-mysql 3306 → 3307) so a host server can keep the
// default port. The container-internal port is untouched. Works for any built-in
// or installed custom service.
func newServicePortCmd() *cobra.Command {
	var reset bool
	cmd := &cobra.Command{
		Use:   "port <service> [port]",
		Short: "Set or reset a service's published host port",
		Long: `Move a service's published host port without touching its
container-internal port. For example:

    lerd service port mysql 3307

frees 127.0.0.1:3306 for a host-installed MySQL while lerd-mysql stays reachable
on 3307. Containerized apps reach the service over the lerd network by name, so
they are unaffected. Works for any built-in or installed service. Use --reset
(or "port mysql 0") to return to the default.`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			newPort := 0
			if !reset {
				if len(args) < 2 {
					return fmt.Errorf("provide a port, or pass --reset to use the default")
				}
				p, err := strconv.Atoi(args[1])
				if err != nil {
					return fmt.Errorf("invalid port %q: must be 0-65535", args[1])
				}
				newPort = p
			}
			res, err := serviceops.SetPublishedPort(name, newPort)
			if err != nil {
				if errors.Is(err, serviceops.ErrPortInUse) {
					return fmt.Errorf("%w; pick another (check: %s)", err, FindListenerCmd(strconv.Itoa(newPort)))
				}
				return err
			}
			switch {
			case res.NoOp && res.Actual == 0:
				fmt.Printf("%s already uses its default published port.\n", name)
			case res.NoOp:
				fmt.Printf("%s is already published on port %d.\n", name, res.Actual)
			case !res.Installed && res.Requested == 0:
				fmt.Printf("%s is not installed; cleared its saved published-port override.\n", name)
			case !res.Installed:
				fmt.Printf("%s is not installed; saved published port %d for the next install.\n", name, res.Requested)
			case res.Actual == 0:
				fmt.Printf("Reset %s to its default published port.\n", name)
			case res.Requested == 0:
				fmt.Printf("%s stays published on 127.0.0.1:%d — a host server owns its default port.\n", name, res.Actual)
			default:
				fmt.Printf("lerd-%s now publishes 127.0.0.1:%d (container-internal port unchanged).\n", name, res.Actual)
				fmt.Printf("Update host clients pointed at lerd's %s to port %d; containerized apps are unaffected.\n", name, res.Actual)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&reset, "reset", false, "Reset to the preset default published port")
	return cmd
}

// refreshHostProxySitesForService regenerates the .env of every host-proxy site
// that uses service, so a published-port change — set manually via `lerd service
// port` or by the auto port-ownership guard — is reflected in the loopback
// host:port those sites connect through. Container sites are left untouched: they
// reach the service by name over the lerd network on its unchanged
// container-internal port, which a published-port move never alters. Per site it
// warns rather than failing, so one unwritable site can't block the rest.
func refreshHostProxySitesForService(service string) {
	refreshed := 0
	for _, s := range config.SitesUsingService(service) {
		if !s.IsHostProxy() {
			continue
		}
		if err := runLerdEnv(s.Path); err != nil {
			fmt.Printf("Warning: could not refresh host-proxy site %q for the new %s port: %v\n", s.Name, service, err)
			continue
		}
		refreshed++
	}
	if refreshed > 0 {
		fmt.Printf("Refreshed %d host-proxy site(s) to follow %s's published port.\n", refreshed, service)
	}
}

// Wire the port-ownership guard's shift callback to the host-proxy refresh so any
// quadlet-write path that auto-shifts a DB port (install, start, reinstall) keeps
// host-proxy sites pointed at the right loopback port. `lerd service port` silences
// this hook and refreshes once itself (see newServicePortCmd).
func init() {
	serviceops.OnPublishedPortShift = func(service string, _ int) {
		refreshHostProxySitesForService(service)
	}
	// Route reconcile's "is the unit installed" check through the platform
	// service manager (launchd plist on macOS, .container quadlet on Linux) so
	// it matches `lerd start`'s notion of an installed unit.
	serviceops.UnitInstalledFn = services.Mgr.ContainerUnitInstalled
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
	candidates := knownServices()
	if customs, err := config.ListCustomServices(); err == nil {
		for _, c := range customs {
			candidates = append(candidates, c.Name)
		}
	}
	for _, name := range candidates {
		if config.CountSitesUsingService(name) == 0 && !config.ServiceIsManuallyStarted(name) && !config.ServiceIsPinned(name) {
			unit := "lerd-" + name
			running, _ := podman.ContainerRunning(unit)
			if !running {
				status, _ := podman.UnitStatus(unit)
				running = status == "active" || status == "activating"
			}
			if running {
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
			// A guessed framework definition's PHP range must not constrain the
			// site (a Laravel 6 served by the Laravel 10 def still runs on 7.4),
			// so skip its range and let the real detected version drive which
			// FPM unit coreUnits starts.
			if fw, fwOk := config.GetFrameworkForDir(s.Framework, s.Path); fwOk && !fw.VersionGuessed {
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

// activeFrankenPHPVersions returns the distinct, normalized PHP versions used by
// non-ignored, non-paused FrankenPHP sites, for derived-image rebuild detection.
func activeFrankenPHPVersions() []string {
	reg, err := config.LoadSites()
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, s := range reg.Sites {
		if s.Ignored || s.Paused || !s.IsFrankenPHP() {
			continue
		}
		v := config.NormalizeFrankenPHPVersion(s.PHPVersion)
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

// autoStopUnusedFPMs stops any PHP-FPM container whose PHP version is no longer
// referenced by any active (non-ignored, non-paused) site.
func autoStopUnusedFPMs() {
	versions, err := phpPkg.ListInstalled()
	if err != nil {
		return
	}
	active := activePHPVersions()
	// Never stop the globally configured default PHP version — it must always be
	// available for `php`, `composer`, and `laravel new` shims even when no site
	// explicitly references it (same logic as coreUnits).
	if cfg, cfgErr := config.LoadGlobal(); cfgErr == nil && cfg != nil {
		active[cfg.PHP.DefaultVersion] = true
	}
	for _, v := range versions {
		if active[v] {
			continue
		}
		unit := "lerd-php" + strings.ReplaceAll(v, ".", "") + "-fpm"
		status, _ := podman.UnitStatus(unit)
		if status == "active" || status == "activating" {
			if err := podman.StopUnit(unit); err != nil {
				feedback.Warn("stopping %s: %v", unit, err)
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
		return feedback.Green(status)
	case "inactive":
		return feedback.Amber(status)
	case "failed":
		return feedback.Red(status)
	default:
		return status
	}
}
