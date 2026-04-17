//go:build linux

package cli

// migrateWorkersOnModeChange is a no-op on Linux: systemd always uses the
// exec path regardless of the configured mode, so there's nothing to
// reshape on disk. The `lerd workers mode` command still updates the
// config value for parity but no migration work is needed.
func migrateWorkersOnModeChange(_ /* fromMode */, _ /* toMode */ string) error {
	return nil
}
