package git

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/envfile"
)

// Worktree represents a git worktree checkout for a registered site.
type Worktree struct {
	Name   string // subdirectory name under .git/worktrees/
	Branch string // sanitized branch (subdomain-safe)
	Path   string // absolute path to checkout dir
	Domain string // "<sanitized-branch>.<siteDomain>"
}

// MainBranch returns the current branch of the main repo checkout at sitePath,
// or an empty string if it cannot be determined.
func MainBranch(sitePath string) string {
	data, err := os.ReadFile(filepath.Join(sitePath, ".git", "HEAD"))
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(data))
	const prefix = "ref: refs/heads/"
	if strings.HasPrefix(line, prefix) {
		return strings.TrimPrefix(line, prefix)
	}
	if len(line) >= 7 {
		return "detached-" + line[:7]
	}
	return ""
}

// IsMainRepo returns true if sitePath/.git is a directory (not a file).
// A file means the repo itself is a worktree, not the main checkout.
func IsMainRepo(sitePath string) bool {
	info, err := os.Stat(filepath.Join(sitePath, ".git"))
	if err != nil {
		return false
	}
	return info.IsDir()
}

// DetectWorktrees returns the list of active worktrees for the given site.
func DetectWorktrees(sitePath, siteDomain string) ([]Worktree, error) {
	if !IsMainRepo(sitePath) {
		return nil, nil
	}

	worktreesDir := filepath.Join(sitePath, ".git", "worktrees")
	entries, err := os.ReadDir(worktreesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var result []Worktree
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		wtDir := filepath.Join(worktreesDir, name)

		branch := readBranch(wtDir)
		path := readCheckoutPath(wtDir)
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err != nil {
			continue // checkout dir gone
		}

		sanitized := SanitizeBranch(branch)
		result = append(result, Worktree{
			Name:   name,
			Branch: sanitized,
			Path:   path,
			Domain: sanitized + "." + siteDomain,
		})
	}
	return result, nil
}

// readBranch reads the branch name from .git/worktrees/<name>/HEAD.
func readBranch(wtDir string) string {
	data, err := os.ReadFile(filepath.Join(wtDir, "HEAD"))
	if err != nil {
		return "detached"
	}
	line := strings.TrimSpace(string(data))
	const prefix = "ref: refs/heads/"
	if strings.HasPrefix(line, prefix) {
		return strings.TrimPrefix(line, prefix)
	}
	// detached HEAD — use first 7 chars of SHA
	if len(line) >= 7 {
		return "detached-" + line[:7]
	}
	return "detached"
}

// readCheckoutPath reads the checkout directory path from .git/worktrees/<name>/gitdir.
func readCheckoutPath(wtDir string) string {
	data, err := os.ReadFile(filepath.Join(wtDir, "gitdir"))
	if err != nil {
		return ""
	}
	// gitdir contains the path to the .git file inside the checkout, e.g. /path/to/checkout/.git
	gitFile := strings.TrimSpace(string(data))
	return filepath.Dir(gitFile)
}

// EnsureWorktreeDeps sets up a worktree checkout with the dependencies it needs:
//   - vendor/ and node_modules/ are seeded from the main repo via a reflink
//     copy (near-instant on btrfs, xfs-reflink, APFS; plain copy on ext4),
//     then reconciled against the worktree's own lockfiles via
//     composer install / npm ci.
//   - .env is copied from the main repo with APP_URL rewritten to http(s)://<worktreeDomain>
//
// Copying (rather than symlinking) is required because PHP resolves __DIR__
// through symlinks, which would make Composer's ClassLoader initialise
// against the main repo directory and silently load stale classes from
// there.
func EnsureWorktreeDeps(mainRepoPath, worktreePath, worktreeDomain string, secured bool) {
	// Each entry: filesystem dir to seed from main, plus a sibling lockfile
	// (or files) that gates the copy. When the worktree's lockfile differs
	// from main's, skip the copy and let composer/npm rebuild the tree from
	// scratch — copying main's vendor/node_modules with a different package
	// set leaves stale autoload paths and bootstrap caches that fail to load
	// classes the worktree's lock doesn't include.
	//
	// public/build (Laravel's Vite manifest output) is intentionally NOT
	// seeded: it's a build artefact of the source tree, not a dependency
	// cache, and copying it makes the worktree silently render main's UI
	// even when the branch has touched assets. Fresh worktrees get
	// ViteManifestNotFoundException until `npm run dev` / `npm run build`
	// runs inside them, which is the honest signal.
	type seed struct {
		dir       string
		lockfiles []string // any one matching is enough; empty = always copy
	}
	seeds := []seed{
		{dir: "vendor", lockfiles: []string{"composer.lock"}},
		{dir: "node_modules", lockfiles: []string{
			"pnpm-lock.yaml", "yarn.lock", "bun.lockb", "bun.lock",
			"package-lock.json", "npm-shrinkwrap.json",
		}},
	}
	for _, s := range seeds {
		dst := filepath.Join(worktreePath, s.dir)
		if info, err := os.Lstat(dst); err == nil {
			if info.Mode()&os.ModeSymlink == 0 {
				continue // real dir already exists, leave it
			}
			_ = os.Remove(dst) // legacy symlink from older lerd, replace it
		}
		src := filepath.Join(mainRepoPath, s.dir)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		if !lockfilesMatch(mainRepoPath, worktreePath, s.lockfiles) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			_, _ = os.Stderr.WriteString("[WARN] mkdir for " + s.dir + " into worktree: " + err.Error() + "\n")
			continue
		}
		if err := CopyTree(src, dst); err != nil {
			_, _ = os.Stderr.WriteString("[WARN] copy " + s.dir + " into worktree: " + err.Error() + "\n")
		}
	}

	// .env must be in place BEFORE InstallDependencies, since the JS build
	// step reads VITE_* env vars at compile time. Without this, the worktree
	// ships with assets compiled against missing env (Reverb host empty,
	// APP_URL falling back to literal "/", etc.).
	EnsureWorktreeEnv(mainRepoPath, worktreePath, worktreeDomain, secured)

	if err := InstallDependencies(worktreePath); err != nil {
		_, _ = os.Stderr.WriteString("[WARN] worktree dependency install: " + err.Error() + "\n")
	}
}

// lockfilesMatch returns true when the first lockfile that exists in main
// has byte-identical contents to the same file in the worktree. An empty
// list (no lockfile-gated dir) returns true so callers always copy.
// A lockfile that exists in main but is missing in the worktree counts as a
// mismatch — the package set differs, do not seed stale state.
func lockfilesMatch(mainRepoPath, worktreePath string, lockfiles []string) bool {
	if len(lockfiles) == 0 {
		return true
	}
	for _, name := range lockfiles {
		mainData, err := os.ReadFile(filepath.Join(mainRepoPath, name))
		if err != nil {
			continue
		}
		wtData, err := os.ReadFile(filepath.Join(worktreePath, name))
		if err != nil {
			return false
		}
		return bytes.Equal(mainData, wtData)
	}
	return true
}

// EnsureWorktreeEnv copies .env from the main repo when missing (gitignored,
// so `git worktree add` never carries it across) and rewrites APP_URL to the
// worktree domain. When the main repo's .lerd.yaml defines env_overrides,
// those values are resolved and layered on top — only keys declared in
// env_overrides are touched, so partial overrides (e.g. SESSION_DOMAIN only)
// don't suppress the default APP_URL rewrite. Idempotent and cheap; safe to
// call on every request.
func EnsureWorktreeEnv(mainRepoPath, worktreePath, worktreeDomain string, secured bool) {
	scheme := "http"
	if secured {
		scheme = "https"
	}
	worktreeEnv := filepath.Join(worktreePath, ".env")
	if _, err := os.Lstat(worktreeEnv); err != nil {
		mainEnv := filepath.Join(mainRepoPath, ".env")
		if err := copyFile(mainEnv, worktreeEnv); err != nil {
			return
		}
	}

	updates := map[string]string{
		"APP_URL": scheme + "://" + worktreeDomain,
	}

	cfg, _ := config.LoadProjectConfig(mainRepoPath)
	if cfg != nil && len(cfg.EnvOverrides) > 0 {
		// {{site}}: legacy DB-safe slug of the FULL worktree domain, e.g.
		// feat_a_acme_test. Kept for backward compatibility — new templates
		// should prefer {{branch}} or {{parent}} which match user intent.
		// {{branch}}: first segment of the worktree domain, e.g. "feat-a".
		// {{parent}}: parent site name slug (DB-safe), e.g. "acme".
		site := config.SiteSlug(worktreeDomain)
		branch := worktreeDomain
		if i := strings.IndexByte(worktreeDomain, '.'); i > 0 {
			branch = worktreeDomain[:i]
		}
		parent := ""
		if s, err := config.FindSiteByPath(mainRepoPath); err == nil && s != nil {
			parent = config.SiteSlug(s.Name)
		}
		for k, v := range cfg.EnvOverrides {
			v = strings.ReplaceAll(v, "{{domain}}", worktreeDomain)
			v = strings.ReplaceAll(v, "{{scheme}}", scheme)
			v = strings.ReplaceAll(v, "{{site}}", site)
			v = strings.ReplaceAll(v, "{{branch}}", branch)
			v = strings.ReplaceAll(v, "{{parent}}", parent)
			updates[k] = v
		}
	}

	_ = envfile.ApplyUpdates(worktreeEnv, updates)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

var nonSlugChars = regexp.MustCompile(`[^a-z0-9-]`)
var multiHyphen = regexp.MustCompile(`-{2,}`)

// SanitizeBranch converts a branch name to a subdomain-safe slug.
func SanitizeBranch(branch string) string {
	s := strings.ToLower(branch)
	// Replace common separators with hyphens
	s = strings.NewReplacer("/", "-", "_", "-", ".", "-").Replace(s)
	// Strip anything not alphanumeric or hyphen
	s = nonSlugChars.ReplaceAllString(s, "")
	// Collapse consecutive hyphens
	s = multiHyphen.ReplaceAllString(s, "-")
	// Trim leading/trailing hyphens
	s = strings.Trim(s, "-")
	// Truncate to 50 chars
	if len(s) > 50 {
		s = strings.TrimRight(s[:50], "-")
	}
	if s == "" {
		return "branch"
	}
	return s
}
