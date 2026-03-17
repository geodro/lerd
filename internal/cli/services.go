package cli

import (
	"fmt"
	"strings"

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

			// Write quadlet if not already installed
			if err := ensureServiceQuadlet(name); err != nil {
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
			fmt.Printf("%-20s %s\n", "Service", "Status")
			fmt.Printf("%-20s %s\n", strings.Repeat("─", 20), strings.Repeat("─", 10))
			for _, svc := range knownServices {
				unit := "lerd-" + svc
				status, err := podman.UnitStatus(unit)
				if err != nil {
					status = "unknown"
				}
				fmt.Printf("%-20s %s\n", svc, colorStatus(status))
			}
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

// printEnvVars prints the recommended .env variables for a service.
func printEnvVars(name string) {
	info, ok := serviceEnvVars[name]
	if !ok || len(info.envVars) == 0 {
		return
	}
	fmt.Println("\nAdd to your .env:")
	for _, v := range info.envVars {
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
