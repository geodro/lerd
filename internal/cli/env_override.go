package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/envfile"
	"github.com/spf13/cobra"
)

// envOverrideFile is the personal, gitignored, per-project override file.
// It is plain dotenv syntax: any KEY=VALUE is layered on top of what
// `lerd env` writes, winning over lerd's defaults and computed values.
const envOverrideFile = ".env.lerd_override"

// envOverrideExternalKey is the one reserved key inside envOverrideFile. Its
// comma/space separated value lists services lerd should NOT start or
// provision for this project (you run your own). It is consumed by lerd and
// never written into the project's .env.
const envOverrideExternalKey = "LERD_EXTERNAL_SERVICES"

const envOverrideTemplate = `# lerd per-project overrides — personal, not committed (gitignored).
#
# Any KEY=VALUE below is written into this project's .env on every ` + "`lerd env`" + `,
# winning over lerd's defaults and computed values. Use it to keep your own
# connection settings without hand-editing .env after each run, e.g.:
#   DB_USERNAME=postgres
#   DB_PASSWORD=secret
#
# List services lerd should NOT start or provision here (you run your own),
# comma-separated. lerd still writes their connection vars to .env so you can
# point them at your instance with the lines above:
#   LERD_EXTERNAL_SERVICES=postgres
`

// readEnvOverride loads the personal override file from cwd. It returns the
// KEY=VALUE overrides (with the reserved external key stripped out) and the set
// of externally-managed service names (lowercased). A missing or unreadable
// file yields two empty, non-nil collections. Values are kept verbatim,
// including any surrounding quotes, so they round-trip into .env unchanged —
// quotes matter for values with spaces or '#'.
func readEnvOverride(cwd string) (overrides map[string]string, external map[string]bool) {
	overrides = map[string]string{}
	external = map[string]bool{}

	f, err := os.Open(filepath.Join(cwd, envOverrideFile))
	if err != nil {
		return overrides, external
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		k, v, _ := strings.Cut(line, "=")
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k == "" {
			continue
		}
		if k == envOverrideExternalKey {
			for _, name := range strings.FieldsFunc(strings.Trim(v, `"'`), func(r rune) bool {
				return r == ',' || r == ' ' || r == '\t'
			}) {
				if name = strings.ToLower(name); name != "" {
					external[name] = true
				}
			}
			continue
		}
		overrides[k] = v
	}
	return overrides, external
}

// externalManaged reports whether name is marked externally managed in
// .env.lerd_override, printing the standard notice when it is so callers can
// `continue` past start/provision after writing the connection vars.
func externalManaged(name string, external map[string]bool) bool {
	if external[name] {
		fmt.Printf("  %-12s externally managed (%s) — not starting it\n", name, envOverrideFile)
		return true
	}
	return false
}

// externalDBPicked reports whether any externally-managed service is a database
// (built-in mysql/postgres or a family alternate), mirroring
// userPickedDBFromYAML so the sqlite-swap prompt is skipped when the user is
// pointing the project at their own database.
func externalDBPicked(external map[string]bool) bool {
	for name := range external {
		if name == "mysql" || name == "postgres" {
			return true
		}
		switch config.FamilyOfName(name) {
		case "mysql", "mariadb", "postgres", "mongo":
			return true
		}
	}
	return false
}

// overrideOr returns the override value for key when present, else the base
// (existing .env) value. Used so key generation that gates on the current env
// (APP_KEY, BROADCAST_CONNECTION) sees values supplied via the override file.
func overrideOr(overrides, base map[string]string, key string) string {
	if v, ok := overrides[key]; ok {
		return v
	}
	return base[key]
}

// NewEnvOverrideCmd returns the env:override command, which scaffolds (and
// optionally seeds) the personal .env.lerd_override file and gitignores it.
func NewEnvOverrideCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "env:override [KEY=VALUE ...]",
		Short: "Create/edit a personal, gitignored per-project .env override file",
		Long: `Create (and optionally seed) .env.lerd_override for this project.

This file is personal and never committed: lerd adds it to .gitignore. Every
KEY=VALUE in it is layered on top of what 'lerd env' writes, winning over
lerd's defaults. Keep your own credentials there, e.g.:

  lerd env:override DB_USERNAME=postgres DB_PASSWORD=secret

To run your own instance of a service instead of lerd's container, add the
reserved key (lerd then writes the connection vars but won't start/provision it):

  lerd env:override LERD_EXTERNAL_SERVICES=postgres

Run with no arguments to just create the file from a commented template.
Re-run 'lerd env' to apply the overrides to your .env.`,
		RunE: runEnvOverride,
	}
}

func runEnvOverride(_ *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	created, err := applyEnvOverrideFile(cwd, args)
	if err != nil {
		return err
	}

	if created {
		fmt.Printf("Created %s (gitignored).\n", envOverrideFile)
	} else {
		fmt.Printf("Updated %s.\n", envOverrideFile)
	}
	for _, kv := range args {
		fmt.Printf("  %s\n", kv)
	}
	fmt.Println("Run 'lerd env' to apply these overrides to your .env.")
	return nil
}

// applyEnvOverrideFile creates the override file from the template when absent,
// seeds any KEY=VALUE args into it, and makes sure it is gitignored. It returns
// whether the file was freshly created.
func applyEnvOverrideFile(cwd string, args []string) (created bool, err error) {
	path := filepath.Join(cwd, envOverrideFile)

	if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
		if err := os.WriteFile(path, []byte(envOverrideTemplate), 0644); err != nil {
			return false, fmt.Errorf("creating %s: %w", envOverrideFile, err)
		}
		created = true
	}

	if len(args) > 0 {
		updates := map[string]string{}
		for _, kv := range args {
			k, v, ok := strings.Cut(kv, "=")
			k = strings.TrimSpace(k)
			if !ok || k == "" {
				return created, fmt.Errorf("invalid override %q: expected KEY=VALUE", kv)
			}
			updates[k] = v
		}
		if err := envfile.ApplyUpdates(path, updates); err != nil {
			return created, fmt.Errorf("writing %s: %w", envOverrideFile, err)
		}
	}

	ensureOverrideGitignored(cwd)
	return created, nil
}

// ensureOverrideGitignored makes sure .gitignore lists the override file,
// creating .gitignore when the project has none.
func ensureOverrideGitignored(cwd string) {
	gitignore := filepath.Join(cwd, ".gitignore")
	if _, err := os.Stat(gitignore); os.IsNotExist(err) {
		_ = os.WriteFile(gitignore, []byte(envOverrideFile+"\n"), 0644)
		return
	}
	addToGitignore(cwd, envOverrideFile)
}
