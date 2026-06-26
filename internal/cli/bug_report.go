package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
	"github.com/geodro/lerd/internal/services"
	"github.com/geodro/lerd/internal/version"
	"github.com/spf13/cobra"
)

// NewBugReportCmd returns the bug-report command. It writes a single
// plain-text file aggregating the diagnostics a maintainer needs to triage a
// GitHub issue: doctor output, config files, systemd/podman state, recent
// logs, network state, and a curated set of environment variables.
func NewBugReportCmd() *cobra.Command {
	var outputPath string
	var logLines int
	var showRealNames bool
	cmd := &cobra.Command{
		Use:   "bug-report",
		Short: "Collect diagnostics into a single file for GitHub bug reports",
		Long: "Generates a plain-text report containing the lerd doctor output, " +
			"config files, systemd unit state, recent service logs, network " +
			"state and the relevant environment variables. Attach the file to " +
			"your GitHub issue.\n\n" +
			"Site names, domains and parked-directory paths are replaced with " +
			"site-1/site-2/$PARK_1/etc. by default. Pass --show-real-names if " +
			"you want raw values for local debugging.",
		RunE: func(_ *cobra.Command, _ []string) error {
			path, err := writeBugReport(outputPath, logLines, !showRealNames)
			if err != nil {
				return err
			}
			fmt.Printf("Bug report written to: %s\n", path)
			fmt.Println("Skim it for anything sensitive before posting, then attach it to your GitHub issue.")
			return nil
		},
	}
	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "output file path (default: ./lerd-bug-report-<timestamp>.txt)")
	cmd.Flags().IntVar(&logLines, "log-lines", 200, "number of recent log lines to include per service/container")
	cmd.Flags().BoolVar(&showRealNames, "show-real-names", false, "include real site names, domains and parked-directory paths instead of placeholders")
	return cmd
}

func writeBugReport(outputPath string, logLines int, anonymize bool) (string, error) {
	if logLines <= 0 {
		logLines = 200
	}
	if outputPath == "" {
		ts := time.Now().Format("20060102-150405")
		outputPath = fmt.Sprintf("lerd-bug-report-%s.txt", ts)
	}
	abs, err := filepath.Abs(outputPath)
	if err != nil {
		return "", fmt.Errorf("resolving output path: %w", err)
	}
	f, err := os.Create(abs)
	if err != nil {
		return "", fmt.Errorf("creating output file: %w", err)
	}
	defer f.Close()

	// Build the anonymizer up-front so the header can advertise whether
	// site/domain/parked-dir replacement is in effect.
	var anon *anonymizer
	if anonymize {
		anon = newAnonymizer()
	}
	// The log filter is independent of the anonymizer: even with
	// --show-real-names we still want to drop request-shaped noise. The
	// domain-aware safety net only fires when we have sites.yaml entries.
	filter := newLogFilter()

	var buf bytes.Buffer
	collectBugReport(&buf, logLines, anon, filter)

	scrubbed := scrubHomePath(buf.String())
	scrubbed = anon.Apply(scrubbed)
	scrubbed = redactGenericPII(scrubbed)
	if _, err := io.WriteString(f, scrubbed); err != nil {
		return "", fmt.Errorf("writing report: %w", err)
	}
	return abs, nil
}

func collectBugReport(w io.Writer, logLines int, anon *anonymizer, filter *logFilter) {
	writeBugReportHeader(w, anon)

	section(w, "Doctor")
	if _, _, err := RunDoctorTo(w, false); err != nil {
		fmt.Fprintf(w, "doctor failed: %v\n", err)
	}

	section(w, "Config files")
	dumpConfigFiles(w)

	section(w, "Installed runtimes")
	dumpRuntimes(w)

	section(w, "Systemd / launchd unit state")
	dumpUnitState(w)

	section(w, "Containers")
	dumpContainers(w)

	section(w, fmt.Sprintf("Recent service logs (last %d lines)", logLines))
	dumpServiceLogs(w, logLines, filter)

	section(w, fmt.Sprintf("Recent container logs (last %d lines)", logLines))
	dumpContainerLogs(w, logLines, filter)

	section(w, "Network")
	dumpNetwork(w)

	section(w, "Environment")
	dumpEnvironment(w)
}

func writeBugReportHeader(w io.Writer, anon *anonymizer) {
	fmt.Fprintln(w, "Lerd bug report")
	fmt.Fprintln(w, "════════════════════════════════════════════════════════")
	fmt.Fprintln(w, "Review this file before posting publicly. It contains")
	fmt.Fprintln(w, "your config and recent service logs but no .env contents.")
	fmt.Fprintln(w, "Home paths and the username are replaced with $HOME / $USER.")
	fmt.Fprintln(w, "Logs are kept only for lerd's own infra (nginx, ui, dns,")
	fmt.Fprintln(w, "watcher, tray); preset services, FPM, workers and HTTP access")
	fmt.Fprintln(w, "lines are dropped. Custom services and per-site containers")
	fmt.Fprintln(w, "are omitted entirely.")
	if anon.active() {
		fmt.Fprintln(w, "Site names, domains and parked-directory paths have been")
		fmt.Fprintln(w, "anonymized (site-N, siteN.<tld>, $PARK_N). Re-run with")
		fmt.Fprintln(w, "--show-real-names to keep the raw values.")
	}
	fmt.Fprintln(w, "════════════════════════════════════════════════════════")
	fmt.Fprintf(w, "Generated:  %s\n", time.Now().Format(time.RFC3339))
	fmt.Fprintf(w, "lerd:       %s\n", version.String())
	fmt.Fprintf(w, "OS:         %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(w, "Go runtime: %s\n", runtime.Version())
	if runtime.GOOS == "linux" {
		if name := readOSRelease(); name != "" {
			fmt.Fprintf(w, "Distro:     %s\n", name)
		}
		if out, err := exec.Command("uname", "-r").Output(); err == nil {
			fmt.Fprintf(w, "Kernel:     %s\n", strings.TrimSpace(string(out)))
		}
	}
	if runtime.GOOS == "darwin" {
		if out, err := exec.Command("sw_vers", "-productVersion").Output(); err == nil {
			fmt.Fprintf(w, "macOS:      %s\n", strings.TrimSpace(string(out)))
		}
	}
}

func section(w io.Writer, title string) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, "════════════════════════════════════════════════════════")
	fmt.Fprintf(w, "  %s\n", title)
	fmt.Fprintln(w, "════════════════════════════════════════════════════════")
}

func dumpConfigFiles(w io.Writer) {
	for _, p := range []string{config.GlobalConfigFile(), config.SitesFile()} {
		fmt.Fprintf(w, "── %s\n", p)
		data, err := os.ReadFile(p)
		if err != nil {
			fmt.Fprintf(w, "(unreadable: %v)\n\n", err)
			continue
		}
		fmt.Fprintln(w, string(data))
		if !bytes.HasSuffix(data, []byte("\n")) {
			fmt.Fprintln(w)
		}
	}
}

func dumpRuntimes(w io.Writer) {
	if out, err := podman.Cmd("--version").Output(); err == nil {
		fmt.Fprintf(w, "podman:    %s", string(out))
	} else {
		fmt.Fprintln(w, "podman:    (not found)")
	}
	if out, err := podman.Cmd("info", "--format", "{{.Host.OCIRuntime.Name}} {{.Host.OCIRuntime.Version}}").Output(); err == nil {
		fmt.Fprintf(w, "OCI:       %s", string(out))
	}
	if cfg, err := config.LoadGlobal(); err == nil {
		fmt.Fprintf(w, "PHP default: %s\n", cfg.PHP.DefaultVersion)
		fmt.Fprintf(w, "Node default: %s\n", cfg.Node.DefaultVersion)
	}
}

func dumpUnitState(w io.Writer) {
	units := lerdUnits()
	if len(units) == 0 {
		fmt.Fprintln(w, "(no lerd units found)")
		return
	}
	// Bucket per-site worker units by type so the listing reports
	// "lerd-queue-* (N units, all active)" instead of one row per
	// (worker, site). The mapping between sites and which workers they
	// run is metadata we don't need for infra debugging.
	buckets := map[string][]string{}
	var infra []string
	for _, name := range units {
		if pfx := workerUnitPrefix(name); pfx != "" {
			buckets[pfx] = append(buckets[pfx], name)
			continue
		}
		infra = append(infra, name)
	}
	for _, name := range infra {
		state, err := services.Mgr.UnitStatus(name)
		if err != nil {
			state = fmt.Sprintf("error: %v", err)
		}
		enabled := "no"
		if services.Mgr.IsEnabled(name) {
			enabled = "yes"
		}
		fmt.Fprintf(w, "%-30s  state=%s  enabled=%s\n", name, state, enabled)
	}
	keys := make([]string, 0, len(buckets))
	for k := range buckets {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		members := buckets[k]
		var states, enabled, failed int
		for _, name := range members {
			state, _ := services.Mgr.UnitStatus(name)
			if state == "active" {
				states++
			}
			if state == "failed" {
				failed++
			}
			if services.Mgr.IsEnabled(name) {
				enabled++
			}
		}
		summary := fmt.Sprintf("%d total, %d active, %d enabled", len(members), states, enabled)
		if failed > 0 {
			summary += fmt.Sprintf(", %d failed", failed)
		}
		fmt.Fprintf(w, "%-30s  %s\n", k+"-*", summary)
	}
}

// workerUnitPrefix returns the leading worker-type prefix for a unit
// name (e.g. `lerd-queue` for `lerd-queue-laravel`), or "" if the unit
// is not a per-site worker. Kept in sync with isContentUnit so the unit
// state aggregation matches the log-skip policy.
func workerUnitPrefix(name string) string {
	for _, p := range []string{"lerd-queue", "lerd-schedule", "lerd-horizon", "lerd-stripe", "lerd-reverb"} {
		if strings.HasPrefix(name, p+"-") {
			return p
		}
	}
	return ""
}

// lerdUnits returns the union of installed container units and service units
// matching the lerd-* prefix, deduplicated and sorted. Custom services and
// per-site custom/FrankenPHP containers are filtered out — they expose user
// app names and aren't useful for debugging lerd itself.
func lerdUnits() []string {
	seen := map[string]struct{}{}
	for _, n := range services.Mgr.ListContainerUnits("lerd-*") {
		seen[n] = struct{}{}
	}
	for _, n := range services.Mgr.ListServiceUnits("lerd-*") {
		seen[n] = struct{}{}
	}
	priv := privateUnitSet()
	out := make([]string, 0, len(seen))
	for n := range seen {
		if isPrivateUnit(n, priv) {
			continue
		}
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// privateUnitSet is the set of unit base names (without .service/.container
// suffix) that belong to user-defined custom services. Built once per
// invocation so we don't reload the YAML for every name check.
func privateUnitSet() map[string]struct{} {
	out := map[string]struct{}{}
	list, err := config.ListCustomServices()
	if err != nil {
		return out
	}
	for _, s := range list {
		if s == nil || s.Name == "" {
			continue
		}
		out["lerd-"+s.Name] = struct{}{}
	}
	return out
}

// isPrivateUnit reports whether a unit/container name belongs to user-defined
// config: custom services, per-site custom containers (lerd-custom-*) and
// FrankenPHP per-site containers (lerd-fp-*). These are dropped entirely
// from the report — names alone reveal app identifiers and their state is
// not useful for triaging lerd itself.
func isPrivateUnit(name string, custom map[string]struct{}) bool {
	base := stripUnitSuffix(name)
	if strings.HasPrefix(base, "lerd-custom-") || strings.HasPrefix(base, "lerd-fp-") {
		return true
	}
	_, ok := custom[base]
	return ok
}

func stripUnitSuffix(s string) string {
	s = strings.TrimSuffix(s, ".service")
	s = strings.TrimSuffix(s, ".container")
	return s
}

func dumpContainers(w io.Writer) {
	out, err := podman.Run("ps", "-a", "--filter", "name=lerd-", "--format",
		"{{.Names}}\t{{.Status}}\t{{.Image}}")
	if err != nil {
		fmt.Fprintf(w, "(podman ps failed: %v)\n", err)
		return
	}
	out = strings.TrimRight(out, "\n")
	if out == "" {
		fmt.Fprintln(w, "(no lerd containers)")
		return
	}
	// Same aggregation as dumpUnitState: collapse per-site worker
	// containers (stripe-, reverb-, etc) into a "<type>-* (N total)"
	// summary so the listing doesn't reveal site→container fan-out.
	// Custom services and per-site custom/FrankenPHP containers are
	// dropped entirely (privacy: their names are user-defined).
	priv := privateUnitSet()
	buckets := map[string]int{}
	var infra []string
	for _, line := range strings.Split(out, "\n") {
		name := line
		if i := strings.Index(line, "\t"); i >= 0 {
			name = line[:i]
		}
		if isPrivateUnit(name, priv) {
			continue
		}
		if pfx := workerUnitPrefix(name); pfx != "" {
			buckets[pfx]++
			continue
		}
		infra = append(infra, line)
	}
	for _, line := range infra {
		fmt.Fprintln(w, line)
	}
	keys := make([]string, 0, len(buckets))
	for k := range buckets {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(w, "%s-*\t%d total (per-site workers)\n", k, buckets[k])
	}
}

func dumpServiceLogs(w io.Writer, n int, filter *logFilter) {
	if runtime.GOOS != "linux" {
		fmt.Fprintln(w, "(skipped: journalctl is Linux-only)")
		return
	}
	for _, unit := range lerdUnits() {
		if isContentUnit(unit) {
			continue
		}
		fmt.Fprintf(w, "── journalctl --user -u %s --no-pager -n %d\n", unit, n)
		out, err := exec.Command("journalctl", "--user", "-u", unit,
			"--no-pager", "-n", fmt.Sprintf("%d", n)).CombinedOutput()
		if err != nil {
			fmt.Fprintf(w, "(failed: %v)\n", err)
		}
		cleaned := filter.clean(strings.TrimRight(string(out), "\n"))
		fmt.Fprintln(w, cleaned)
		fmt.Fprintln(w)
	}
}

func dumpContainerLogs(w io.Writer, n int, filter *logFilter) {
	out, err := podman.Run("ps", "-a", "--filter", "name=lerd-", "--format", "{{.Names}}")
	if err != nil {
		fmt.Fprintf(w, "(podman ps failed: %v)\n", err)
		return
	}
	names := strings.Fields(out)
	if len(names) == 0 {
		fmt.Fprintln(w, "(no lerd containers)")
		return
	}
	priv := privateUnitSet()
	for _, name := range names {
		if isPrivateUnit(name, priv) {
			continue
		}
		if isContentUnit(name) {
			continue
		}
		fmt.Fprintf(w, "── podman logs --tail %d %s\n", n, name)
		logs, err := podman.Run("logs", "--tail", fmt.Sprintf("%d", n), name)
		if err != nil {
			fmt.Fprintf(w, "(failed: %v)\n\n", err)
			continue
		}
		fmt.Fprintln(w, filter.clean(logs))
		fmt.Fprintln(w)
	}
}

func dumpNetwork(w io.Writer) {
	fmt.Fprintln(w, "── listening sockets (lerd-relevant ports)")
	listing := PortListOutput()
	for _, port := range []string{"53", "80", "443", "5300", "7073"} {
		for _, line := range strings.Split(listing, "\n") {
			if strings.Contains(line, ":"+port+" ") {
				fmt.Fprintf(w, "  port %-4s  %s\n", port, redactNonLoopbackAddrs(strings.TrimSpace(line)))
			}
		}
	}
	fmt.Fprintln(w)

	fmt.Fprintln(w, "── /etc/resolv.conf")
	if data, err := os.ReadFile("/etc/resolv.conf"); err == nil {
		fmt.Fprintln(w, redactResolvConf(strings.TrimRight(string(data), "\n")))
	} else {
		fmt.Fprintf(w, "(unreadable: %v)\n", err)
	}
	fmt.Fprintln(w)

	fmt.Fprintln(w, "── host gateway probe (host.containers.internal)")
	if !services.Mgr.IsActive("lerd-nginx") || !services.Mgr.IsActive("lerd-ui") {
		fmt.Fprintln(w, "skipped: lerd-nginx and/or lerd-ui not running")
	} else if ip := podman.DetectHostGatewayIPProbeOnly(); ip != "" {
		fmt.Fprintf(w, "reachable via %s\n", ip)
	} else {
		fmt.Fprintln(w, "no candidate routed back to host (Xdebug + container→host calls will fail)")
	}
}

// envAllowlist is the curated set of environment variables included in the
// bug report. We intentionally avoid dumping the full environment because
// CI/CD or shell setups frequently contain secrets in unrelated variables.
var envAllowlist = []string{
	"PATH",
	"SHELL",
	"USER",
	"LOGNAME",
	"LANG",
	"LC_ALL",
	"TERM",
	"XDG_CONFIG_HOME",
	"XDG_DATA_HOME",
	"XDG_RUNTIME_DIR",
	"XDG_SESSION_TYPE",
	"DBUS_SESSION_BUS_ADDRESS",
	"DOCKER_HOST",
	"CONTAINER_HOST",
}

func dumpEnvironment(w io.Writer) {
	for _, key := range envAllowlist {
		val := os.Getenv(key)
		if val == "" {
			continue
		}
		fmt.Fprintf(w, "%s=%s\n", key, val)
	}
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "LERD_") {
			continue
		}
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			fmt.Fprintln(w, kv)
			continue
		}
		key, val := kv[:eq], kv[eq+1:]
		if isSecretShapedKey(key) {
			fmt.Fprintf(w, "%s=<redacted>\n", key)
			continue
		}
		fmt.Fprintf(w, "%s=%s\n", key, val)
	}
}

// isSecretShapedKey returns true when the env-var name suggests its value is
// sensitive. Used for the LERD_* dump in bug reports — even though the rest
// of the bug-report scrubbers redact common secret value shapes, key names
// containing TOKEN/SECRET/PASSWORD/PASSWD/KEY are a stronger signal.
func isSecretShapedKey(key string) bool {
	upper := strings.ToUpper(key)
	for _, needle := range []string{"TOKEN", "SECRET", "PASSWORD", "PASSWD", "API_KEY", "PRIVATE_KEY"} {
		if strings.Contains(upper, needle) {
			return true
		}
	}
	if strings.HasSuffix(upper, "_KEY") || upper == "KEY" {
		return true
	}
	return false
}

// scrubHomePath replaces occurrences of the user's home directory with
// "$HOME" and bare occurrences of the username with "$USER". Cosmetic
// privacy only — not a security boundary. Usernames shorter than three
// characters are skipped to avoid corrupting unrelated substrings.
func scrubHomePath(s string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" || home == "/" {
		home = ""
	}
	user := currentUsername(home)
	var pairs []string
	if home != "" {
		pairs = append(pairs, home, "$HOME")
	}
	if len(user) >= 3 {
		pairs = append(pairs, user, "$USER")
	}
	if len(pairs) == 0 {
		return s
	}
	return strings.NewReplacer(pairs...).Replace(s)
}

// currentUsername resolves the login name used to anonymize bare
// occurrences. Prefers $USER/$LOGNAME (cheap, matches what the user
// sees in their shell) and falls back to the basename of $HOME.
func currentUsername(home string) string {
	for _, env := range []string{"USER", "LOGNAME"} {
		if v := os.Getenv(env); v != "" {
			return v
		}
	}
	if home != "" {
		return filepath.Base(home)
	}
	return ""
}

// anonymizer rewrites site names, domains and parked-directory paths into
// stable placeholders (site-1, site1.<tld>, $PARK_1, ...). It is a single
// pass strings.Replacer applied to the assembled report. Same security
// caveat as scrubHomePath: not a guarantee, only a courtesy.
type anonymizer struct {
	r *strings.Replacer
}

// active reports whether at least one replacement was registered. Used by
// the header to decide whether to advertise anonymization to the reader.
func (a *anonymizer) active() bool {
	return a != nil && a.r != nil
}

// Apply runs the replacements. Safe to call on a nil receiver or an empty
// anonymizer — returns the input unchanged.
func (a *anonymizer) Apply(s string) string {
	if !a.active() {
		return s
	}
	return a.r.Replace(s)
}

// newAnonymizer assembles the replacement map from the current sites.yaml
// and config.yaml. Pairs are sorted longest-first so substring matches do
// not corrupt longer ones (e.g. `laravel.localhost` is replaced before the
// bare `laravel`; and a site path containing another site's name is
// replaced as a whole before the inner name has a chance to match).
func newAnonymizer() *anonymizer {
	type pair struct{ old, new string }
	var pairs []pair
	home, _ := os.UserHomeDir()

	type parkedDir struct {
		raw  string
		home string
		repl string
	}
	var parkedDirs []parkedDir

	if cfg, err := config.LoadGlobal(); err == nil && cfg != nil {
		for i, dir := range cfg.ParkedDirectories {
			if dir == "" {
				continue
			}
			pd := parkedDir{raw: dir, repl: fmt.Sprintf("$PARK_%d", i+1)}
			if home != "" && strings.HasPrefix(dir, home+"/") {
				pd.home = "$HOME" + strings.TrimPrefix(dir, home)
			} else if dir == home {
				pd.home = "$HOME"
			}
			parkedDirs = append(parkedDirs, pd)
			pairs = append(pairs, pair{pd.raw, pd.repl})
			if pd.home != "" {
				pairs = append(pairs, pair{pd.home, pd.repl})
			}
		}
	}

	if reg, err := config.LoadSites(); err == nil && reg != nil {
		for idx, s := range reg.Sites {
			if s.Name == "" {
				continue
			}
			repl := fmt.Sprintf("site-%d", idx+1)

			// Site path: register full path (raw + $HOME-scrubbed) so
			// it always replaces as a unit before the bare name pair
			// can match a substring inside it. When the path falls
			// inside a parked dir the placeholder shows that
			// relationship; otherwise it is opaque.
			if s.Path != "" {
				pathRepl := fmt.Sprintf("$SITE_%d_PATH", idx+1)
				for _, pd := range parkedDirs {
					if strings.HasPrefix(s.Path, pd.raw+"/") {
						pathRepl = pd.repl + "/" + repl
						break
					}
				}
				pairs = append(pairs, pair{s.Path, pathRepl})
				if home != "" && strings.HasPrefix(s.Path, home+"/") {
					pairs = append(pairs, pair{"$HOME" + strings.TrimPrefix(s.Path, home), pathRepl})
				}
			}

			for di, d := range s.Domains {
				if d == "" {
					continue
				}
				tld := tldSuffix(d)
				if di == 0 {
					pairs = append(pairs, pair{d, repl + tld})
				} else {
					pairs = append(pairs, pair{d, fmt.Sprintf("%s-extra%d%s", repl, di, tld)})
				}
			}
			pairs = append(pairs, pair{s.Name, repl})
		}
	}

	sort.Slice(pairs, func(i, j int) bool { return len(pairs[i].old) > len(pairs[j].old) })

	flat := make([]string, 0, len(pairs)*2)
	seen := map[string]struct{}{}
	for _, p := range pairs {
		if p.old == "" || p.old == p.new {
			continue
		}
		if _, dup := seen[p.old]; dup {
			continue
		}
		seen[p.old] = struct{}{}
		flat = append(flat, p.old, p.new)
	}
	if len(flat) == 0 {
		return &anonymizer{}
	}
	return &anonymizer{r: strings.NewReplacer(flat...)}
}

// tldSuffix returns the trailing ".tld" portion of a domain (everything
// from the last dot, dot included), or "" if there is no dot. The empty
// case is intentional — bare names without a dot get replaced as-is.
func tldSuffix(domain string) string {
	if i := strings.LastIndex(domain, "."); i >= 0 {
		return domain[i:]
	}
	return ""
}

// lerdInfraUnits is the allowlist of unit/container base names whose logs
// are included in the bug report. Anything outside this set (preset
// services like redis/mysql/gotenberg, per-site workers, FPM containers,
// custom services and containers) is treated as app-domain content and
// its logs are dropped — they're noisy, often request-shaped, and don't
// help debug lerd itself. State for preset services still appears in the
// unit-state and container tables.
var lerdInfraUnits = map[string]struct{}{
	"lerd-nginx":     {},
	"lerd-ui":        {},
	"lerd-watcher":   {},
	"lerd-dns":       {},
	"lerd-tray":      {},
	"lerd-autostart": {},
	"lerd-fpm-init":  {},
}

// isContentUnit reports whether logs for this unit should be skipped.
// Inverse of the lerdInfraUnits allowlist.
func isContentUnit(name string) bool {
	_, ok := lerdInfraUnits[stripUnitSuffix(name)]
	return !ok
}

// accessLogRes matches lines that look like per-request access logging
// across the various formats lerd's services emit:
//   - Common/Combined Log Format from nginx and phpMyAdmin's Apache
//     (`[time] "VERB ..."` after a quoted-style header)
//   - Meilisearch's `INFO HTTP request{method=...}` structured line
//
// Real errors (nginx `[error]`, PHP `Fatal error`, etc) don't carry
// these markers and survive the filter.
var accessLogRes = []*regexp.Regexp{
	regexp.MustCompile(`\[\d{2}/[A-Za-z]{3}/\d{4}:\d{2}:\d{2}:\d{2} [+\-]\d{4}\] "[A-Z]{3,7} `),
	regexp.MustCompile(`\bINFO HTTP request\{`),
}

// requestURIRedactions matches the URI portion of nginx-style structured
// error lines (`request: "GET /path?q HTTP/1.1"`, `upstream: "http://..."`,
// `referrer: "..."`). The error message itself is infra signal but the
// URI/upstream/referrer values can leak app routes and query data, so we
// keep the line and redact only those fields.
var requestURIRedactions = []struct {
	re      *regexp.Regexp
	replace string
}{
	{regexp.MustCompile(`(request: "[A-Z]+) [^"]+( HTTP/\d(?:\.\d)?")`), `${1} <redacted>${2}`},
	{regexp.MustCompile(`(upstream: ")[^"]+(")`), `${1}<redacted>${2}`},
	{regexp.MustCompile(`(referrer: ")[^"]+(")`), `${1}<redacted>${2}`},
	{regexp.MustCompile(`(referer: ")[^"]+(")`), `${1}<redacted>${2}`},
}

// logFilter cleans log output before it lands in the bug report. It
// owns the regex set so the user-domain safety net (built once per
// invocation from sites.yaml) lives next to the access-log shape filters
// instead of in a global cache.
type logFilter struct {
	dropRes      []*regexp.Regexp
	userDomainRe *regexp.Regexp
}

// newLogFilter builds the per-invocation filter. Always populated with
// the access-log shape patterns; userDomainRe is populated only when
// sites.yaml has at least one domain entry.
func newLogFilter() *logFilter {
	f := &logFilter{dropRes: append([]*regexp.Regexp(nil), accessLogRes...)}
	if reg, err := config.LoadSites(); err == nil && reg != nil {
		var alts []string
		for _, s := range reg.Sites {
			for _, d := range s.Domains {
				if d != "" {
					alts = append(alts, regexp.QuoteMeta(d))
				}
			}
		}
		if len(alts) > 0 {
			f.userDomainRe = regexp.MustCompile(`(?i)(?:` + strings.Join(alts, "|") + `)`)
		}
	}
	return f
}

// clean drops access-log shaped lines, drops any line that mentions a
// configured user domain (case-insensitive), and redacts URI/upstream
// fragments from nginx structured error lines that survive both checks.
func (f *logFilter) clean(s string) string {
	if f == nil {
		return s
	}
	lines := strings.Split(s, "\n")
	out := lines[:0]
lineLoop:
	for _, line := range lines {
		for _, re := range f.dropRes {
			if re.MatchString(line) {
				continue lineLoop
			}
		}
		if f.userDomainRe != nil && f.userDomainRe.MatchString(line) {
			continue lineLoop
		}
		for _, r := range requestURIRedactions {
			line = r.re.ReplaceAllString(line, r.replace)
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// piiRedactors is the ordered set of regex/replace pairs applied to the
// fully-assembled bug report. Defined as a package-level var so the regexes
// are compiled once. Applied AFTER scrubHomePath and the site anonymizer so
// it cleans whatever the earlier passes left behind without re-rewriting the
// substitutions they introduced ($HOME, site-N, etc).
//
// Coverage: emails, JWTs, common bearer/Authorization headers, RFC 1918 +
// public IPv4 (loopback and link-local survive), and git remote URLs.
var piiRedactors = []struct {
	re      *regexp.Regexp
	replace string
}{
	// Bearer/Authorization tokens before the email regex catches the prefix.
	{regexp.MustCompile(`(?i)(authorization:\s*(?:bearer|basic|token)\s+)\S+`), `${1}<redacted>`},
	{regexp.MustCompile(`\b(?:bearer|token)\s+[A-Za-z0-9_\-\.~+/]{16,}=*\b`), `<redacted-token>`},
	// JWTs: three base64url segments separated by dots. Length floor at 16
	// per segment to avoid matching arbitrary dotted tokens.
	{regexp.MustCompile(`\b[A-Za-z0-9_-]{16,}\.[A-Za-z0-9_-]{16,}\.[A-Za-z0-9_-]{16,}\b`), `<redacted-jwt>`},
	// Slack/GitHub/AWS-style API keys (long alnum strings prefixed by a
	// recognisable marker). Keep tight so we don't false-positive on hashes.
	{regexp.MustCompile(`\b(xox[bopas]-|ghp_|ghu_|gho_|ghs_|github_pat_|sk-)[A-Za-z0-9_-]{16,}\b`), `<redacted-token>`},
	// Git SSH/HTTPS remotes BEFORE the email regex, because `git@host:path`
	// parses as an email otherwise (the `@` and TLD-shaped host fool it).
	{regexp.MustCompile(`\bgit@[A-Za-z0-9._-]+:[A-Za-z0-9._/-]+(?:\.git)?\b`), `<redacted-git-remote>`},
	{regexp.MustCompile(`\bhttps?://[A-Za-z0-9._-]+/[A-Za-z0-9._/-]+\.git\b`), `<redacted-git-remote>`},
	// Emails. The pattern keeps the trailing word-boundary tight so we don't
	// chew through the next token.
	{regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b`), `<redacted-email>`},
}

// redactGenericPII applies the regex-based PII redactors in piiRedactors.
// Idempotent: applying it twice is a no-op because every replacement starts
// with `<redacted` which none of the patterns match.
func redactGenericPII(s string) string {
	for _, r := range piiRedactors {
		s = r.re.ReplaceAllString(s, r.replace)
	}
	return s
}

// nonLoopbackIPv4Re matches an IPv4 address (each octet 0–255). The loopback
// (127.x) / link-local (169.254.x) / 0.0.0.0 carve-outs are applied in the
// ReplaceAllStringFunc callback below, not in the regex itself, so the
// pattern stays simple and readable.
var nonLoopbackIPv4Re = regexp.MustCompile(`\b(?:25[0-5]|2[0-4]\d|1\d{2}|[1-9]?\d)\.(?:25[0-5]|2[0-4]\d|1\d{2}|[1-9]?\d)\.(?:25[0-5]|2[0-4]\d|1\d{2}|[1-9]?\d)\.(?:25[0-5]|2[0-4]\d|1\d{2}|[1-9]?\d)\b`)

// nonLoopbackIPv6Re catches IPv6 addresses, including the `::` compressed
// form. Heuristic: at least two colons, hex/colon characters only, length
// 3+. The ::1 / fe80::/10 carve-outs are applied in the callback.
var nonLoopbackIPv6Re = regexp.MustCompile(`\b[0-9a-fA-F]*(?::[0-9a-fA-F]*){2,}\b`)

func redactNonLoopbackAddrs(line string) string {
	out := nonLoopbackIPv4Re.ReplaceAllStringFunc(line, func(m string) string {
		if strings.HasPrefix(m, "127.") || strings.HasPrefix(m, "169.254.") || m == "0.0.0.0" {
			return m
		}
		return "<redacted-ip>"
	})
	out = nonLoopbackIPv6Re.ReplaceAllStringFunc(out, func(m string) string {
		lower := strings.ToLower(m)
		if lower == "::1" || strings.HasPrefix(lower, "fe80:") {
			return m
		}
		return "<redacted-ip>"
	})
	return out
}

// redactResolvConf scrubs nameserver / search / domain values from
// /etc/resolv.conf. Corporate or VPN setups put internal IPs here that
// identify the user's employer. We keep the directive name and a redacted
// placeholder so the structure is still legible.
func redactResolvConf(s string) string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		switch {
		case strings.HasPrefix(trimmed, "nameserver"):
			out = append(out, "nameserver <redacted>")
		case strings.HasPrefix(trimmed, "search"):
			out = append(out, "search <redacted>")
		case strings.HasPrefix(trimmed, "domain"):
			out = append(out, "domain <redacted>")
		default:
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

// readOSRelease reads /etc/os-release and returns the PRETTY_NAME value.
// Returns empty string on any error.
func readOSRelease() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			val := strings.TrimPrefix(line, "PRETTY_NAME=")
			return strings.Trim(val, `"`)
		}
	}
	return ""
}
