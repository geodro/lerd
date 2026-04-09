package cli

import (
	"fmt"

	"github.com/geodro/lerd/internal/config"
)

// filterConflictingDomains splits desired into the domains that ownPath is
// allowed to claim and those already taken by a different site. The check is
// strict: a domain is a conflict regardless of TLS scheme (HTTPS vs HTTP).
// Two sites cannot share the same domain even on different ports — DNS and
// browser caches don't disambiguate by scheme reliably. Order is preserved
// so the user's preferred primary domain stays primary if it survives.
//
// Re-linking the same path is not a conflict — domains owned by ownPath stay
// in the kept list. Reserved domains (e.g. lerd's own internal hosts) are
// always filtered out.
func filterConflictingDomains(desired []string, ownPath string, allSites []config.Site) (kept, removed []string) {
	// Build a domain → owning path index from the registry.
	owners := make(map[string]string, len(allSites)*2)
	for _, s := range allSites {
		for _, d := range s.Domains {
			owners[d] = s.Path
		}
	}

	for _, d := range desired {
		if isReservedDomain(d) {
			removed = append(removed, d)
			continue
		}
		owner, taken := owners[d]
		if taken && owner != ownPath {
			removed = append(removed, d)
			continue
		}
		kept = append(kept, d)
	}
	return kept, removed
}

// resolveSiteDomains takes the desired domain list (built from .lerd.yaml or
// auto-generated), filters out conflicts using the live site registry, and
// returns the final list to write into the site struct. Falls back to a
// freshly generated `<baseName>.<tld>` (with numeric suffix to avoid both
// name and domain collisions) when every desired domain is conflicted.
//
// Any filtered domains are reported to the caller via the removed slice so
// the caller can print a warning. The .lerd.yaml file on disk is never
// touched — the discrepancy lives only in memory and the site registry.
func resolveSiteDomains(desired []string, baseName, ownPath, tld string) (kept, removed []string) {
	reg, err := config.LoadSites()
	var sites []config.Site
	if err == nil {
		sites = reg.Sites
	}

	kept, removed = filterConflictingDomains(desired, ownPath, sites)
	if len(kept) > 0 {
		return kept, removed
	}

	// Every desired domain was filtered. Fall back to a generated name+domain
	// that's free in both axes. Loop with a numeric suffix until we find one.
	for i := 0; ; i++ {
		candidate := baseName
		if i > 0 {
			candidate = fmt.Sprintf("%s-%d", baseName, i+1)
		}
		domain := candidate + "." + tld
		if isReservedDomain(domain) {
			continue
		}
		if existing, _ := config.FindSite(candidate); existing != nil && existing.Path != ownPath {
			continue
		}
		owners, _ := filterConflictingDomains([]string{domain}, ownPath, sites)
		if len(owners) == 0 {
			continue
		}
		return []string{domain}, removed
	}
}

// warnFilteredDomains prints a single-line warning for each domain that
// resolveSiteDomains had to drop because another site already owns it.
// Includes the conflicting site name when known so the user can react.
func warnFilteredDomains(removed []string) {
	if len(removed) == 0 {
		return
	}
	for _, d := range removed {
		if existing, err := config.IsDomainUsed(d); err == nil && existing != nil {
			fmt.Printf("  [WARN] domain %q already used by site %q — skipped\n", d, existing.Name)
			continue
		}
		fmt.Printf("  [WARN] domain %q is reserved — skipped\n", d)
	}
}
