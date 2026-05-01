package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/envfile"
	gitpkg "github.com/geodro/lerd/internal/git"
	"github.com/geodro/lerd/internal/nginx"
)

// sitesWithTLD returns the names of registered sites that have at least one
// domain ending in "."+oldTLD, in registry order. Used to drive the install
// migration prompt when the user flips dns.enabled or otherwise picks a new
// TLD: only sites whose stored domains still carry the previous TLD are
// candidates for rewrite.
func sitesWithTLD(oldTLD string) []string {
	suffix := "." + oldTLD
	reg, err := config.LoadSites()
	if err != nil || reg == nil {
		return nil
	}
	var names []string
	for _, s := range reg.Sites {
		for _, d := range s.Domains {
			if strings.HasSuffix(d, suffix) {
				names = append(names, s.Name)
				break
			}
		}
	}
	return names
}

// migrateSiteTLD rewrites every site's domain suffix from oldTLD to newTLD,
// removes stale nginx vhost confs at the previous primary-domain paths, and
// updates each site's .env APP_URL (plus Vite/Reverb keys) via
// envfile.SyncPrimaryDomain. When forceUnsecure is true (DNS being disabled,
// so HTTPS is unavailable) the site's Secured flag is also flipped off so the
// regen pass writes plain HTTP vhosts.
//
// Returns the list of sites that were actually mutated. Errors on individual
// sites are printed but do not stop the migration: a partial rename is still
// preferable to leaving the user halfway between two TLDs.
func migrateSiteTLD(oldTLD, newTLD string, forceUnsecure bool) []string {
	if oldTLD == "" || newTLD == "" || oldTLD == newTLD {
		return nil
	}
	oldSuffix := "." + oldTLD
	newSuffix := "." + newTLD

	reg, err := config.LoadSites()
	if err != nil || reg == nil {
		return nil
	}

	var changed []string
	for _, s := range reg.Sites {
		oldPrimary := s.PrimaryDomain()
		rewrote := false
		newDomains := make([]string, len(s.Domains))
		for i, d := range s.Domains {
			if strings.HasSuffix(d, oldSuffix) {
				newDomains[i] = strings.TrimSuffix(d, oldSuffix) + newSuffix
				rewrote = true
			} else {
				newDomains[i] = d
			}
		}
		if !rewrote {
			continue
		}

		// Capture worktrees against the OLD primary so we can identify
		// their stale conf files before mutating the site.
		worktrees, _ := gitpkg.DetectWorktrees(s.Path, oldPrimary)

		s.Domains = newDomains
		if forceUnsecure {
			s.Secured = false
		}
		if err := config.AddSite(s); err != nil {
			fmt.Printf("    WARN: %s: persist domains: %v\n", s.Name, err)
			continue
		}

		removeStaleVhosts(oldPrimary)
		if forceUnsecure {
			removeStaleCerts(oldPrimary)
		}
		newPrimary := s.PrimaryDomain()
		migrateWorktreeVhosts(worktrees, newPrimary, s.PHPVersion, s.Secured)

		scheme := "http"
		if s.Secured {
			scheme = "https"
		}
		if err := envfile.SyncPrimaryDomain(s.Path, newPrimary, s.Secured); err != nil {
			fmt.Printf("    WARN: %s: update .env: %v\n", s.Name, err)
		}
		_ = config.SetProjectSecured(s.Path, s.Secured)
		_ = config.SyncProjectDomains(s.Path, s.Domains, newTLD)

		fmt.Printf("    --> %s: %s -> %s://%s\n", s.Name, oldPrimary, scheme, newPrimary)
		changed = append(changed, s.Name)
	}
	return changed
}

// migrateWorktreeVhosts removes each worktree's stale vhost confs (built from
// its old <branch>.<oldPrimary> domain), regenerates a fresh vhost at the new
// <branch>.<newPrimary> domain, and rewrites the worktree's .env APP_URL.
// Worktree errors are warnings, not fatal; the parent site rename has already
// landed and partial worktree state is preferable to abandoning the migration.
func migrateWorktreeVhosts(worktrees []gitpkg.Worktree, newPrimary, phpVersion string, secured bool) {
	for _, wt := range worktrees {
		removeStaleVhosts(wt.Domain)
		newWTDomain := wt.Branch + "." + newPrimary
		var err error
		if secured {
			err = nginx.GenerateWorktreeSSLVhost(newWTDomain, wt.Path, phpVersion, newPrimary)
		} else {
			err = nginx.GenerateWorktreeVhost(newWTDomain, wt.Path, phpVersion)
		}
		if err != nil {
			fmt.Printf("    WARN: worktree %s: regenerate vhost: %v\n", wt.Branch, err)
		}
		scheme := "http"
		if secured {
			scheme = "https"
		}
		if err := envfile.UpdateAppURL(wt.Path, scheme, newWTDomain); err != nil {
			fmt.Printf("    WARN: worktree %s: update .env: %v\n", wt.Branch, err)
		}
	}
}

// removeStaleVhosts deletes the old <domain>.conf and <domain>-ssl.conf so
// the next regen pass does not have two vhosts (old + new) competing for the
// same upstream. Errors are swallowed: missing files are the common case.
func removeStaleVhosts(oldDomain string) {
	if oldDomain == "" {
		return
	}
	for _, suffix := range []string{".conf", "-ssl.conf"} {
		_ = os.Remove(filepath.Join(config.NginxConfD(), oldDomain+suffix))
	}
}

// removeStaleCerts deletes the .crt and .key for a site whose TLS state was
// flipped off as part of the migration (HTTPS unavailable in disabled-DNS
// mode). Mirrors the cleanup UnsecureSite does but for the OLD primary so we
// do not leave dead cert files under the previous TLD on disk.
func removeStaleCerts(oldDomain string) {
	if oldDomain == "" {
		return
	}
	dir := filepath.Join(config.CertsDir(), "sites")
	for _, ext := range []string{".crt", ".key"} {
		_ = os.Remove(filepath.Join(dir, oldDomain+ext))
	}
}
