package cli

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/services"
	"github.com/spf13/cobra"
)

// NewMinioMigrateCmd returns the minio:migrate command.
func NewMinioMigrateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "minio:migrate",
		Short: "Migrate MinIO data to RustFS",
		Long: `Migrates an existing MinIO installation to RustFS.

This command will:
  - Stop the lerd-minio service
  - Copy data from ~/.local/share/lerd/data/minio to ~/.local/share/lerd/data/rustfs
  - Start the lerd-rustfs service

RustFS uses the same S3 API and credentials as MinIO, so no application changes are needed.`,
		RunE: runMinioMigrate,
	}
}

func runMinioMigrate(_ *cobra.Command, _ []string) error {
	minioDataDir := config.DataSubDir("minio")
	rustfsDataDir := config.DataSubDir("rustfs")

	// Check that there is something to migrate.
	if _, err := os.Stat(minioDataDir); os.IsNotExist(err) {
		return fmt.Errorf("no MinIO data found at %s\nNothing to migrate", minioDataDir)
	}

	fmt.Println("Migrating MinIO data to RustFS...")

	// Stop minio if running.
	status, _ := services.Mgr.UnitStatus("lerd-minio")
	if status == "active" || status == "activating" {
		fmt.Print("  Stopping lerd-minio...          ")
		if err := services.Mgr.Stop("lerd-minio"); err != nil {
			return fmt.Errorf("could not stop lerd-minio: %w", err)
		}
		fmt.Println("done")
	}

	// Remove minio quadlet so it no longer auto-starts.
	fmt.Print("  Removing MinIO quadlet...       ")
	if err := services.Mgr.RemoveContainerUnit("lerd-minio"); err != nil && !os.IsNotExist(err) {
		fmt.Printf("warn (%v)\n", err)
	} else {
		fmt.Println("done")
	}
	if err := services.Mgr.DaemonReload(); err != nil {
		fmt.Printf("  warn: daemon-reload failed: %v\n", err)
	}

	// Copy minio data to rustfs data dir.
	fmt.Print("  Copying data directory...       ")
	if err := os.MkdirAll(rustfsDataDir, 0755); err != nil {
		return fmt.Errorf("creating rustfs data dir: %w", err)
	}
	cp := exec.Command("cp", "-a", minioDataDir+"/.", rustfsDataDir+"/")
	if out, err := cp.CombinedOutput(); err != nil {
		return fmt.Errorf("copying data: %s", out)
	}
	fmt.Println("done")

	// Update global config: disable minio entry if present, ensure rustfs exists.
	cfg, err := config.LoadGlobal()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if cfg.Services == nil {
		cfg.Services = map[string]config.ServiceConfig{}
	}
	minioWasEnabled := cfg.Services["minio"].Enabled
	delete(cfg.Services, "minio")
	if _, hasRustfs := cfg.Services["rustfs"]; !hasRustfs {
		cfg.Services["rustfs"] = config.ServiceConfig{
			Enabled: minioWasEnabled,
			Image:   "rustfs/rustfs:latest",
			Port:    9000,
		}
	}
	if err := config.SaveGlobal(cfg); err != nil {
		fmt.Printf("  warn: could not save config: %v\n", err)
	}

	// Install and start RustFS.
	fmt.Print("  Installing RustFS...            ")
	if err := ensureServiceQuadlet("rustfs"); err != nil {
		return fmt.Errorf("installing rustfs quadlet: %w", err)
	}
	fmt.Println("done")

	fmt.Print("  Starting lerd-rustfs...         ")
	if err := services.Mgr.Start("lerd-rustfs"); err != nil {
		return fmt.Errorf("starting lerd-rustfs: %w", err)
	}
	_ = config.SetServiceManuallyStarted("rustfs", true)
	fmt.Println("done")

	fmt.Println()
	fmt.Println("Migration complete.")
	fmt.Println("  RustFS console: http://localhost:9001")
	fmt.Println("  Credentials:    lerd / lerdpassword")
	fmt.Println()
	fmt.Printf("MinIO data directory at %s was not removed.\n", minioDataDir)
	fmt.Println("Delete it manually once you have verified the migration: rm -rf " + minioDataDir)
	return nil
}
