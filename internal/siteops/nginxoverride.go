package siteops

import (
	"os"
	"path/filepath"

	"github.com/geodro/lerd/internal/cfgedit"
	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/nginx"
)

// NginxTestFn / NginxReloadFn are indirections so tests can stub the
// podman-bound `nginx -t` and reload. The global-nginx editor reuses these so
// site and global saves share one validator/reload pair. Defaults are real.
var (
	NginxTestFn   = nginx.Test
	NginxReloadFn = nginx.Reload
)

// nginxSiteTemplate seeds the editor when no override exists yet. Everything
// is commented so the file is an inert no-op until the user opts in.
const nginxSiteTemplate = `# Lerd per-site nginx overrides.
#
# Included at the end of this site's server { } block. Lerd never overwrites
# this file, so edits survive vhost regeneration and ` + "`lerd update`" + `. Add
# directives valid inside a server block, then save to reload nginx.

# client_max_body_size 100m;
# location /ws { proxy_pass http://127.0.0.1:6001; proxy_http_version 1.1; }
`

// CustomNginxPath is the on-disk path of a domain's custom override.
func CustomNginxPath(domain string) string {
	return filepath.Join(config.NginxCustomD(), domain+".conf")
}

// nginxFile builds the cfgedit.File for a domain's custom override. Backups and
// write-staging live in custom.d.bkp/, kept off the custom.d/*.conf* glob.
func nginxFile(domain string) cfgedit.File {
	return cfgedit.File{
		Path:     CustomNginxPath(domain),
		BkpDir:   config.NginxCustomDBkp(),
		BkpName:  domain + ".conf",
		Template: nginxSiteTemplate,
	}
}

// nginxValidate runs `nginx -t` (via the stubbable indirection) so cfgedit can
// pre-flight a save without importing nginx itself.
func nginxValidate(string) (string, error) { return NginxTestFn() }

// ReadCustomNginx returns the saved override, or the seeded template
// (Exists=false) when nothing is on disk yet.
func ReadCustomNginx(domain string) (cfgedit.Content, error) {
	return nginxFile(domain).Read()
}

// SaveCustomNginx writes, validates with `nginx -t`, rolls back on a failure
// that names our file, and reloads nginx.
func SaveCustomNginx(domain, content string, backup bool) (cfgedit.SaveResult, error) {
	return nginxFile(domain).Save(content, cfgedit.SaveOpts{
		Backup:   backup,
		Validate: nginxValidate,
		Owns:     cfgedit.MentionsFile,
		Apply:    func() error { return NginxReloadFn() },
	})
}

// ResetCustomNginx deletes the override and reloads nginx. Backups are kept.
func ResetCustomNginx(domain string) error {
	return nginxFile(domain).Reset(func() error { return NginxReloadFn() })
}

// ListCustomNginxBackups returns a domain's override backups, newest first.
func ListCustomNginxBackups(domain string) ([]cfgedit.Backup, error) {
	return nginxFile(domain).ListBackups()
}

// ReadCustomNginxBackup returns the raw bytes of one backup (os.ErrNotExist
// when the name is invalid or the file is gone).
func ReadCustomNginxBackup(domain, name string) ([]byte, error) {
	return nginxFile(domain).ReadBackup(name)
}

// RestoreCustomNginx swaps a backup over the live override and reloads nginx.
func RestoreCustomNginx(domain, name string) (cfgedit.RestoreResult, error) {
	return nginxFile(domain).Restore(name, func() error { return NginxReloadFn() })
}

// ValidNginxBackupName reports whether name is a well-formed backup for domain.
func ValidNginxBackupName(domain, name string) bool {
	return nginxFile(domain).ValidBackupName(name)
}

// InheritCustomNginxConfig seeds a new worktree's override from its parent's:
// it copies custom.d/{parent}.conf to custom.d/{worktree}.conf only when the
// parent exists and the worktree override does not. Callers must invoke it only
// on genuine worktree creation — running it on every resync would resurrect an
// override the user deliberately reset (it can't tell "new" from "reset").
func InheritCustomNginxConfig(parentDomain, worktreeDomain string) error {
	if parentDomain == worktreeDomain {
		return nil
	}
	dst := CustomNginxPath(worktreeDomain)
	if _, err := os.Stat(dst); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	data, err := os.ReadFile(CustomNginxPath(parentDomain))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(config.NginxCustomD(), 0o755); err != nil {
		return err
	}
	return nginx.WriteFileAtomic(dst, data, 0o644)
}

// RemoveCustomNginxConfig deletes a worktree's live override and every
// timestamped backup for that domain. Used when a worktree is removed.
func RemoveCustomNginxConfig(domain string) error {
	if err := os.Remove(CustomNginxPath(domain)); err != nil && !os.IsNotExist(err) {
		return err
	}
	f := nginxFile(domain)
	backups, err := f.ListBackups()
	if err != nil {
		return err
	}
	var firstErr error
	for _, b := range backups {
		if err := os.Remove(filepath.Join(f.BkpDir, b.Name)); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
