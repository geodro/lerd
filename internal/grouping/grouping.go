// Package grouping manages site groups: a main site that owns a base domain and
// one or more secondary sites that each occupy a chosen subdomain label of that
// base domain (e.g. astrolov.test + admin.astrolov.test). A secondary is a fully
// independent site; grouping only computes its primary domain as
// <label>.<main-domain> and drives the same nginx/cert/env regeneration the
// domain commands use. nginx routes the exact subdomain to the secondary over
// the main's *.<domain> wildcard, the identical mechanism worktree subdomains
// rely on.
package grouping

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/geodro/lerd/internal/certs"
	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/envfile"
	gitpkg "github.com/geodro/lerd/internal/git"
	"github.com/geodro/lerd/internal/nginx"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/siteops"
)

// ComputeSecondaryDomain returns the domain a secondary occupies on the group
// main's base domain.
func ComputeSecondaryDomain(mainPrimary, label string) string {
	return label + "." + mainPrimary
}

// ValidateLabel checks that label is a non-empty, DNS-safe subdomain label in
// its canonical form (lowercase, no transformation needed). It reuses the same
// sanitiser worktree branches go through so a label can never collide with a
// reserved name or produce an invalid host.
func ValidateLabel(label string) error {
	if label == "" {
		return fmt.Errorf("subdomain label cannot be empty")
	}
	if gitpkg.SanitizeBranch(label) != label {
		return fmt.Errorf("subdomain label %q is not a valid host label (use lowercase letters, digits and hyphens)", label)
	}
	return nil
}

// AssignSecondary groups an existing site under main as a secondary occupying
// the given subdomain label. The secondary's old standalone domain is replaced
// by <label>.<main-primary>. main is promoted to a group main on first use.
// When shareDB is true the secondary's DB_DATABASE is pointed at the main's
// database instead of keeping its own. If regeneration fails the registry is
// rolled back so the site doesn't end up grouped with no serving vhost.
func AssignSecondary(main, secondary *config.Site, label string, shareDB bool) error {
	if main.Name == secondary.Name {
		return fmt.Errorf("a site cannot be grouped under itself")
	}
	if err := ValidateLabel(label); err != nil {
		return err
	}
	if main.IsGroupSecondary() {
		return fmt.Errorf("site %q is itself a secondary of group %q and cannot be a group main", main.Name, main.Group)
	}
	if secondary.IsGroupSecondary() {
		return fmt.Errorf("site %q is already grouped under %q", secondary.Name, secondary.Group)
	}

	reg, err := config.LoadSites()
	if err != nil {
		return err
	}
	if secondary.IsGroupMain() && groupHasSecondaries(reg, secondary.Name) {
		return fmt.Errorf("site %q is a group main with its own secondaries; groups are only one level deep", secondary.Name)
	}

	newDomain := ComputeSecondaryDomain(main.PrimaryDomain(), label)
	if owner := domainOwner(reg, newDomain); owner != nil && owner.Name != secondary.Name {
		return fmt.Errorf("domain %s is already used by site %q", newDomain, owner.Name)
	}
	if siblingLabelUsed(reg, main.Name, label, secondary.Name) {
		return fmt.Errorf("another secondary in group %q already uses the subdomain %q", main.Name, label)
	}
	if taken, err := WorktreeLabelTaken(main, label); err != nil {
		return err
	} else if taken {
		return fmt.Errorf("the main site %q has a git worktree using the subdomain %q; pick another label", main.Name, label)
	}

	mainPromoted := false
	if main.Group == "" {
		main.Group = main.Name
		if err := config.AddSite(*main); err != nil {
			return fmt.Errorf("promoting %q to group main: %w", main.Name, err)
		}
		mainPromoted = true
	}

	orig := snapshot(secondary)
	oldPrimary := secondary.PrimaryDomain()
	secondary.Group = main.Group
	secondary.GroupSubdomain = label
	secondary.GroupSharedDB = shareDB
	secondary.Domains = []string{newDomain}
	if err := config.AddSite(*secondary); err != nil {
		return fmt.Errorf("updating site registry: %w", err)
	}
	if err := regenerateSecondary(secondary, oldPrimary); err != nil {
		rollbackSecondary(orig, newDomain)
		if mainPromoted {
			main.Group = ""
			_ = config.AddSite(*main)
		}
		return fmt.Errorf("grouping %q: %w", secondary.Name, err)
	}
	if shareDB {
		applySharedDBEnv(secondary, MainDBName(main))
	}
	return nil
}

// SetSecondarySharedDB toggles whether a grouped secondary shares the group
// main's database. Turning it on rewrites the secondary's DB_DATABASE to the
// main's; turning it off restores the secondary's own database name.
func SetSecondarySharedDB(secondary *config.Site, shareDB bool) error {
	if !secondary.IsGroupSecondary() {
		return fmt.Errorf("site %q is not a grouped secondary", secondary.Name)
	}
	if secondary.GroupSharedDB == shareDB {
		return nil
	}
	reg, err := config.LoadSites()
	if err != nil {
		return err
	}
	main := findGroupMain(reg, secondary.Group)
	if main == nil {
		return fmt.Errorf("group %q has no main site", secondary.Group)
	}
	secondary.GroupSharedDB = shareDB
	if err := config.AddSite(*secondary); err != nil {
		return fmt.Errorf("updating site registry: %w", err)
	}
	if shareDB {
		applySharedDBEnv(secondary, MainDBName(main))
	} else {
		applySharedDBEnv(secondary, config.SiteSlug(secondary.Name))
	}
	return nil
}

// MainDBName returns the database name a shared-DB secondary should use: the
// group main's DB_DATABASE, falling back to the main's canonical slug.
func MainDBName(main *config.Site) string {
	if db := envfile.ReadKey(filepath.Join(main.Path, ".env"), "DB_DATABASE"); db != "" {
		return db
	}
	return config.SiteSlug(main.Name)
}

// SharedDBNameFor returns the database name a site should use when it is a
// shared-DB group secondary, and whether that override applies. The env setup
// path consults this so it never resets a shared secondary back to its own DB.
func SharedDBNameFor(s *config.Site) (string, bool) {
	if s == nil || !s.IsGroupSecondary() || !s.GroupSharedDB {
		return "", false
	}
	reg, err := config.LoadSites()
	if err != nil {
		return "", false
	}
	main := findGroupMain(reg, s.Group)
	if main == nil {
		return "", false
	}
	return MainDBName(main), true
}

// applySharedDBEnv rewrites DB_DATABASE in the secondary's .env, but only when
// the file already declares it (a site with no database is left untouched).
func applySharedDBEnv(secondary *config.Site, dbName string) {
	envPath := filepath.Join(secondary.Path, ".env")
	if envfile.ReadKey(envPath, "DB_DATABASE") == "" {
		return
	}
	if err := envfile.ApplyUpdates(envPath, map[string]string{"DB_DATABASE": dbName}); err != nil {
		fmt.Fprintf(os.Stderr, "lerd: updating DB_DATABASE for %s: %v\n", secondary.Name, err)
	}
}

// UnassignSecondary removes a secondary from its group, restoring a standalone
// domain derived from its directory name. When the group main is left with no
// secondaries its group key is cleared too.
func UnassignSecondary(secondary *config.Site) error {
	if !secondary.IsGroupSecondary() {
		return fmt.Errorf("site %q is not a grouped secondary", secondary.Name)
	}
	group := secondary.Group
	oldPrimary := secondary.PrimaryDomain()
	wasSharedDB := secondary.GroupSharedDB

	reg, err := config.LoadSites()
	if err != nil {
		return err
	}
	_, standalone := siteops.SiteNameAndDomain(filepath.Base(secondary.Path), config.EffectiveTLD())
	if owner := domainOwner(reg, standalone); owner != nil && owner.Name != secondary.Name {
		return fmt.Errorf("cannot restore standalone domain: %s is already used by site %q", standalone, owner.Name)
	}

	orig := snapshot(secondary)
	secondary.Group = ""
	secondary.GroupSubdomain = ""
	secondary.GroupSharedDB = false
	secondary.Domains = []string{standalone}
	if err := config.AddSite(*secondary); err != nil {
		return fmt.Errorf("updating site registry: %w", err)
	}
	if err := regenerateSecondary(secondary, oldPrimary); err != nil {
		rollbackSecondary(orig, standalone)
		return fmt.Errorf("ungrouping %q: %w", secondary.Name, err)
	}
	if wasSharedDB {
		applySharedDBEnv(secondary, config.SiteSlug(secondary.Name))
	}
	return clearMainIfEmpty(group)
}

// SetSecondaryLabel changes the subdomain a secondary occupies on its group
// main's base domain.
func SetSecondaryLabel(secondary *config.Site, newLabel string) error {
	if !secondary.IsGroupSecondary() {
		return fmt.Errorf("site %q is not a grouped secondary", secondary.Name)
	}
	if err := ValidateLabel(newLabel); err != nil {
		return err
	}
	if newLabel == secondary.GroupSubdomain {
		return nil
	}
	reg, err := config.LoadSites()
	if err != nil {
		return err
	}
	main := findGroupMain(reg, secondary.Group)
	if main == nil {
		return fmt.Errorf("group %q has no main site", secondary.Group)
	}
	newDomain := ComputeSecondaryDomain(main.PrimaryDomain(), newLabel)
	if owner := domainOwner(reg, newDomain); owner != nil && owner.Name != secondary.Name {
		return fmt.Errorf("domain %s is already used by site %q", newDomain, owner.Name)
	}
	if siblingLabelUsed(reg, secondary.Group, newLabel, secondary.Name) {
		return fmt.Errorf("another secondary in group %q already uses the subdomain %q", secondary.Group, newLabel)
	}
	if taken, err := WorktreeLabelTaken(main, newLabel); err != nil {
		return err
	} else if taken {
		return fmt.Errorf("the main site %q has a git worktree using the subdomain %q; pick another label", main.Name, newLabel)
	}

	orig := snapshot(secondary)
	oldPrimary := secondary.PrimaryDomain()
	secondary.GroupSubdomain = newLabel
	secondary.Domains = []string{newDomain}
	if err := config.AddSite(*secondary); err != nil {
		return fmt.Errorf("updating site registry: %w", err)
	}
	if err := regenerateSecondary(secondary, oldPrimary); err != nil {
		rollbackSecondary(orig, newDomain)
		return fmt.Errorf("relabelling %q: %w", secondary.Name, err)
	}
	return nil
}

// CascadeMainDomainChange recomputes every secondary's domain against the group
// main's current primary domain and regenerates each. Call it after the main's
// own domain has changed (e.g. via a domain edit) so the subdomains follow.
func CascadeMainDomainChange(main *config.Site) error {
	if !main.IsGroupMain() {
		return nil
	}
	reg, err := config.LoadSites()
	if err != nil {
		return err
	}
	mainPrimary := main.PrimaryDomain()
	var firstErr error
	for _, s := range reg.Sites {
		if s.Group != main.Group || !s.IsGroupSecondary() {
			continue
		}
		sec := s
		oldPrimary := sec.PrimaryDomain()
		newDomain := ComputeSecondaryDomain(mainPrimary, sec.GroupSubdomain)
		if newDomain == oldPrimary {
			continue
		}
		sec.Domains = []string{newDomain}
		if err := config.AddSite(sec); err != nil && firstErr == nil {
			firstErr = err
			continue
		}
		if err := regenerateSecondary(&sec, oldPrimary); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// DissolveGroup ungroups every secondary in the group, restoring standalone
// domains, then clears the main's group key.
func DissolveGroup(group string) error {
	reg, err := config.LoadSites()
	if err != nil {
		return err
	}
	var firstErr error
	for _, s := range reg.Sites {
		if s.Group == group && s.IsGroupSecondary() {
			sec := s
			if err := UnassignSecondary(&sec); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	if err := clearMainIfEmpty(group); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

// WorktreeLabelTaken reports whether the main site has an active git worktree
// whose subdomain would collide with the given label.
func WorktreeLabelTaken(main *config.Site, label string) (bool, error) {
	wts, err := gitpkg.DetectWorktrees(main.Path, main.PrimaryDomain())
	if err != nil {
		return false, err
	}
	newDomain := ComputeSecondaryDomain(main.PrimaryDomain(), label)
	for _, wt := range wts {
		if wt.Branch == label || wt.Domain == newDomain {
			return true, nil
		}
	}
	return false, nil
}

// regenerateSecondary mirrors the proven domain-edit regeneration sequence:
// sync .lerd.yaml, regenerate the vhost (renaming on primary change), reissue
// the cert when secured, rewrite container hosts, reload nginx and sync .env.
// It is a package var so tests can stub out the heavy filesystem/container side
// effects and assert on registry state alone.
var regenerateSecondary = func(secondary *config.Site, oldPrimary string) error {
	_ = config.SyncProjectDomains(secondary.Path, secondary.Domains, config.EffectiveTLD())
	if err := siteops.RegenerateSiteVhost(secondary, oldPrimary); err != nil {
		return err
	}
	if secondary.Secured {
		if err := certs.ReissueCertForWorktree(*secondary); err != nil {
			fmt.Fprintf(os.Stderr, "lerd: reissuing certificate for %s: %v\n", secondary.PrimaryDomain(), err)
		}
	}
	_ = podman.WriteContainerHosts()
	_ = nginx.Reload()
	if err := siteops.SyncEnvIfPrimaryChanged(secondary, oldPrimary); err != nil {
		fmt.Fprintf(os.Stderr, "lerd: syncing .env to new primary domain: %v\n", err)
	}
	return nil
}

// snapshot deep-copies a site so it can be restored verbatim on rollback.
func snapshot(s *config.Site) config.Site {
	cp := *s
	cp.Domains = append([]string(nil), s.Domains...)
	return cp
}

// rollbackSecondary best-effort restores a secondary to orig after a failed
// regeneration: it re-persists the original record and regenerates the original
// vhost (failedPrimary is the half-applied domain whose vhost must be replaced).
func rollbackSecondary(orig config.Site, failedPrimary string) {
	s := orig
	if err := config.AddSite(s); err != nil {
		fmt.Fprintf(os.Stderr, "lerd: rolling back %s in registry: %v\n", orig.Name, err)
		return
	}
	if err := regenerateSecondary(&s, failedPrimary); err != nil {
		fmt.Fprintf(os.Stderr, "lerd: rolling back %s vhost: %v\n", orig.Name, err)
	}
}

// domainOwner returns the registered site that owns domain, or nil if free.
func domainOwner(reg *config.SiteRegistry, domain string) *config.Site {
	for i := range reg.Sites {
		if reg.Sites[i].HasDomain(domain) {
			s := reg.Sites[i]
			return &s
		}
	}
	return nil
}

func siblingLabelUsed(reg *config.SiteRegistry, group, label, exceptName string) bool {
	for _, s := range reg.Sites {
		if s.Name == exceptName {
			continue
		}
		if s.Group == group && s.IsGroupSecondary() && s.GroupSubdomain == label {
			return true
		}
	}
	return false
}

func groupHasSecondaries(reg *config.SiteRegistry, group string) bool {
	for _, s := range reg.Sites {
		if s.Group == group && s.IsGroupSecondary() {
			return true
		}
	}
	return false
}

func findGroupMain(reg *config.SiteRegistry, group string) *config.Site {
	for _, s := range reg.Sites {
		if s.Group == group && s.IsGroupMain() {
			m := s
			return &m
		}
	}
	return nil
}

// clearMainIfEmpty loads a fresh registry (it runs after mutations) and clears
// the main's group key when no secondaries remain in the group.
func clearMainIfEmpty(group string) error {
	if group == "" {
		return nil
	}
	reg, err := config.LoadSites()
	if err != nil {
		return err
	}
	var main *config.Site
	for _, s := range reg.Sites {
		if s.Group == group && s.IsGroupSecondary() {
			return nil // still has secondaries
		}
		if s.Group == group && s.IsGroupMain() {
			m := s
			main = &m
		}
	}
	if main == nil {
		return nil
	}
	main.Group = ""
	return config.AddSite(*main)
}
