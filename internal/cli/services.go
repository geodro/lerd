package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
	"github.com/spf13/cobra"
)

var knownServices = []string{"mysql", "redis", "postgres", "meilisearch", "minio", "mailpit", "soketi"}

// serviceInfo holds the quadlet name and Laravel .env hints for a service.
type serviceInfo struct {
	envVars []string
}

var serviceEnvVars = map[string]serviceInfo{
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
	"minio": {envVars: []string{
		"FILESYSTEM_DISK=s3",
		"AWS_ACCESS_KEY_ID=lerd",
		"AWS_SECRET_ACCESS_KEY=lerdpassword",
		"AWS_DEFAULT_REGION=us-east-1",
		"AWS_BUCKET=lerd",
		"AWS_URL=http://lerd-minio:9000",
		"AWS_ENDPOINT=http://lerd-minio:9000",
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
	"soketi": {envVars: []string{
		"BROADCAST_CONNECTION=pusher",
		"PUSHER_APP_ID=lerd",
		"PUSHER_APP_KEY=lerd-key",
		"PUSHER_APP_SECRET=lerd-secret",
		"PUSHER_HOST=lerd-soketi",
		"PUSHER_PORT=6001",
		"PUSHER_SCHEME=http",
		"PUSHER_APP_CLUSTER=mt1",
		"VITE_PUSHER_APP_KEY=\"${PUSHER_APP_KEY}\"",
		"VITE_PUSHER_HOST=\"${PUSHER_HOST}\"",
		"VITE_PUSHER_PORT=\"${PUSHER_PORT}\"",
		"VITE_PUSHER_SCHEME=\"${PUSHER_SCHEME}\"",
		"VITE_PUSHER_APP_CLUSTER=\"${PUSHER_APP_CLUSTER}\"",
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
		Short: "Manage Lerd services (mysql, redis, postgres, meilisearch, minio, mailpit, soketi)",
	}

	cmd.AddCommand(newServiceStartCmd())
	cmd.AddCommand(newServiceStopCmd())
	cmd.AddCommand(newServiceRestartCmd())
	cmd.AddCommand(newServiceStatusCmd())
	cmd.AddCommand(newServiceListCmd())
	cmd.AddCommand(newServiceAddCmd())
	cmd.AddCommand(newServiceRemoveCmd())

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

			var err error
			if isKnownService(name) {
				err = ensureServiceQuadlet(name)
			} else {
				svc, loadErr := config.LoadCustomService(name)
				if loadErr != nil {
					return fmt.Errorf("unknown service %q", name)
				}
				err = ensureCustomServiceQuadlet(svc)
			}
			if err != nil {
				return err
			}

			fmt.Printf("Starting %s...\n", unit)
			if err := podman.StartUnit(unit); err != nil {
				return err
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
			unit := "lerd-" + args[0]
			fmt.Printf("Stopping %s...\n", unit)
			return podman.StopUnit(unit)
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
			fmt.Printf("%s: %s\n", unit, colorStatus(status))
			return nil
		},
	}
}

func newServiceListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all services and their status",
		RunE: func(_ *cobra.Command, _ []string) error {
			fmt.Printf("%-20s %-10s %s\n", "Service", "Type", "Status")
			fmt.Printf("%-20s %-10s %s\n", strings.Repeat("─", 20), strings.Repeat("─", 10), strings.Repeat("─", 10))
			for _, svc := range knownServices {
				unit := "lerd-" + svc
				status, err := podman.UnitStatus(unit)
				if err != nil {
					status = "unknown"
				}
				fmt.Printf("%-20s %-10s %s\n", svc, "[builtin]", colorStatus(status))
			}
			customs, _ := config.ListCustomServices()
			for _, svc := range customs {
				unit := "lerd-" + svc.Name
				status, err := podman.UnitStatus(unit)
				if err != nil {
					status = "unknown"
				}
				fmt.Printf("%-20s %-10s %s\n", svc.Name, "[custom]", colorStatus(status))
			}
			return nil
		},
	}
}

// newServiceAddCmd returns the `service add` command.
func newServiceAddCmd() *cobra.Command {
	var (
		name            string
		image           string
		ports           []string
		envVars         []string
		containerEnv    []string
		dataDir         string
		detectKey       string
		detectPrefix    string
		description     string
		initExec        string
		initContainer   string
		dashboard       string
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

	return cmd
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

			unit := "lerd-" + name

			// Stop the unit if it is running.
			status, _ := podman.UnitStatus(unit)
			if status == "active" || status == "activating" {
				fmt.Printf("Stopping %s...\n", unit)
				if err := podman.StopUnit(unit); err != nil {
					return fmt.Errorf("could not stop %s: %w\nRemove aborted — the service is still running", unit, err)
				}
			}

			// Remove quadlet and reload
			if err := podman.RemoveQuadlet(unit); err != nil {
				fmt.Printf("  WARN: could not remove quadlet: %v\n", err)
			}
			if err := podman.DaemonReload(); err != nil {
				fmt.Printf("  WARN: daemon-reload failed: %v\n", err)
			}

			// Remove config file
			if err := config.RemoveCustomService(name); err != nil {
				return fmt.Errorf("removing service config: %w", err)
			}

			dataPath := config.DataSubDir(name)
			fmt.Printf("Removed service %q.\n", name)
			fmt.Printf("Data at %s was NOT removed. Delete it manually if no longer needed.\n", dataPath)
			return nil
		},
	}
}

// ensureServiceQuadlet writes the quadlet for a known service and reloads systemd if needed.
func ensureServiceQuadlet(name string) error {
	quadletName := "lerd-" + name
	content, err := podman.GetQuadletTemplate(quadletName + ".container")
	if err != nil {
		return fmt.Errorf("unknown service %q", name)
	}
	if err := podman.WriteQuadlet(quadletName, content); err != nil {
		return fmt.Errorf("writing quadlet for %s: %w", name, err)
	}
	return podman.DaemonReload()
}

// ensureCustomServiceQuadlet writes the quadlet for a custom service and reloads systemd.
func ensureCustomServiceQuadlet(svc *config.CustomService) error {
	if svc.DataDir != "" {
		if err := os.MkdirAll(config.DataSubDir(svc.Name), 0755); err != nil {
			return fmt.Errorf("creating data directory for %s: %w", svc.Name, err)
		}
	}
	content := podman.GenerateCustomQuadlet(svc)
	quadletName := "lerd-" + svc.Name
	if err := podman.WriteQuadlet(quadletName, content); err != nil {
		return fmt.Errorf("writing quadlet for %s: %w", svc.Name, err)
	}
	return podman.DaemonReload()
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
