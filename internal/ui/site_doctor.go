package ui

import (
	"net/http"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/sitedoctor"
)

// doctorRoute handles the doctor subroutes for a site. Loopback-only: checks and
// fixes exec in the site's container, the same trust level as the command runner.
// Returns true when it owns the request. The check logic itself lives in
// internal/sitedoctor so the TUI and CLI share it.
//
//	GET  /api/sites/{domain}/doctor                 → run checks
//	POST /api/sites/{domain}/doctor/fix/{key}/run   → run a package-manager fix
func doctorRoute(w http.ResponseWriter, r *http.Request, domain string, rest []string) bool {
	if len(rest) == 0 || rest[0] != "doctor" {
		return false
	}
	if !isLoopbackRequest(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return true
	}
	site, err := config.FindSiteByDomain(domain)
	if err != nil {
		writeJSON(w, map[string]any{"error": "site not found: " + domain})
		return true
	}
	switch {
	case len(rest) == 1 && r.Method == http.MethodGet:
		handleDoctorRun(w, r, site)
	case len(rest) == 4 && rest[1] == "fix" && rest[3] == "run" && r.Method == http.MethodPost:
		handleDoctorFixRun(w, r, site, rest[2])
	default:
		http.NotFound(w, r)
	}
	return true
}

func handleDoctorRun(w http.ResponseWriter, r *http.Request, site *config.Site) {
	branch := r.URL.Query().Get("branch")
	path, ok := resolveDoctorPath(w, site, branch)
	if !ok {
		return
	}
	// Freshly added worktrees don't carry .env (it's gitignored), so materialise
	// it first — otherwise every file check reads a missing .env and reports a
	// healthy worktree as broken. No-op for the parent and idempotent.
	ensureWorktreeEnvIfBranch(site, branch)
	fw, _ := config.GetFrameworkForDir(site.Framework, path)
	writeJSON(w, sitedoctor.Run(r.Context(), path, fw))
}

// handleDoctorFixRun runs an allowlisted package-manager fix (composer
// install/update, npm install, npm audit fix) and streams its output as SSE,
// reusing the command runner's stream and per-site run lock.
func handleDoctorFixRun(w http.ResponseWriter, r *http.Request, site *config.Site, key string) {
	shell, ok := sitedoctor.DoctorFixCommands[key]
	if !ok {
		writeJSON(w, map[string]any{"error": "unknown doctor fix: " + key})
		return
	}
	branch := r.URL.Query().Get("branch")
	path, ok := resolveDoctorPath(w, site, branch)
	if !ok {
		return
	}
	lockKey := site.Name
	if lockKey == "" && len(site.Domains) > 0 {
		lockKey = site.Domains[0]
	}
	if lockKey == "" {
		lockKey = site.Path
	}
	release, busyWith, ok := tryAcquireRun(lockKey, key)
	if !ok {
		w.WriteHeader(http.StatusConflict)
		writeJSON(w, map[string]any{"error": "another command is already running on this site: " + busyWith})
		return
	}
	defer release()
	streamShellRun(w, r.Context(), path, shell, false)
}

// resolveDoctorPath returns the project path for the site, refusing an
// unresolved worktree branch rather than falling back to the parent checkout
// (which would diagnose or mutate the main site's files and database).
func resolveDoctorPath(w http.ResponseWriter, site *config.Site, branch string) (string, bool) {
	if branch == "" {
		return site.Path, true
	}
	wt := resolveSitePath(site, branch)
	if wt == "" {
		writeJSON(w, map[string]any{"error": "unknown worktree branch: " + branch})
		return "", false
	}
	return wt, true
}
