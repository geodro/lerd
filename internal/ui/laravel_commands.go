package ui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/geodro/lerd/internal/config"
)

// laravelSignatureRe captures the contents of $signature in a Console\Command
// subclass. Single, double, or heredoc-style multiline signatures all share
// the same prefix, so we cap match at the first closing quote on the same line.
// Examples it handles:
//
//	protected $signature = 'app:rebuild-index';
//	protected $signature = "myapp:foo {bar} {--force}";
//	protected $signature = 'reports:weekly
//	                        {--year= : Year to run}';
//
// The leading whitespace + optional access modifier + `$signature` anchor keeps
// us from matching arbitrary other `'app:foo'` strings elsewhere in the file
// (e.g. inside an array literal).
var laravelSignatureRe = regexp.MustCompile(`(?m)(?:public|protected|private)?\s*\$signature\s*=\s*['"]([^'"\n]+)['"]`)

// laravelDescriptionRe is the matching pair for the optional $description
// property. Only the first line is captured because Laravel itself only
// displays the first line in `artisan list`.
var laravelDescriptionRe = regexp.MustCompile(`(?m)(?:public|protected|private)?\s*\$description\s*=\s*['"]([^'"\n]+)['"]`)

// laravelCommandName strips trailing argument annotations from a signature so
// the dashboard shows the canonical command name. `myapp:foo {arg} {--flag}`
// becomes `myapp:foo`. The full signature stays on Command.Command so the
// shellout still passes the flags through to artisan.
func laravelCommandName(sig string) string {
	if i := strings.IndexAny(sig, " \t\n"); i > 0 {
		return sig[:i]
	}
	return sig
}

// hasLaravelArtisan reports whether projectDir looks like a Laravel project:
// presence of the bin/console-style `artisan` file at the root is the cheapest
// and most reliable signal. composer.json parsing would also work but adds
// I/O for no extra precision.
func hasLaravelArtisan(projectDir string) bool {
	_, err := os.Stat(filepath.Join(projectDir, "artisan"))
	return err == nil
}

// scanLaravelCustomCommands walks projectDir/app/Console/Commands and returns
// a FrameworkCommand entry for each user-defined artisan command it finds.
// Returns an empty slice when the project isn't Laravel or has no custom
// commands. Errors reading individual files are swallowed (best-effort
// discovery — we don't want to fail the whole site detail page because of a
// stray syntax issue in one file).
//
// Each emitted command has:
//   - Name:    the artisan signature head (e.g. "app:rebuild-index")
//   - Label:   same as Name (the dashboard shows it monospace already)
//   - Command: `php artisan <full signature without arg placeholders>` so
//              the shell exec runs cleanly. Argument values aren't surfaced
//              in the UI for v1; if needed the user can edit .lerd.yaml.
//   - Description: the $description property if found, otherwise empty.
//   - Output:  "text" so output streams to the existing modal.
//   - Icon:    "play" to visually distinguish custom from framework commands.
//
// Destructive commands (anything matching IsDestructiveCommand) are filtered
// here as well so they never reach the UI list. Defense-in-depth: the run
// handler ALSO checks.
func scanLaravelCustomCommands(projectDir string) []config.FrameworkCommand {
	if !hasLaravelArtisan(projectDir) {
		return nil
	}
	commandsDir := filepath.Join(projectDir, "app", "Console", "Commands")
	out := []config.FrameworkCommand{}

	_ = filepath.WalkDir(commandsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable subtrees
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".php") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		sig := laravelSignatureRe.FindSubmatch(data)
		if len(sig) < 2 {
			return nil
		}
		name := laravelCommandName(string(sig[1]))
		if name == "" {
			return nil
		}
		desc := ""
		if m := laravelDescriptionRe.FindSubmatch(data); len(m) >= 2 {
			desc = string(m[1])
		}
		// Build the artisan shellout. We pass the bare command name so
		// Laravel prompts for any required args interactively — safer than
		// trying to surface arg inputs in the dashboard for v1.
		shell := "php artisan " + name
		if IsDestructiveCommand(shell) || IsDestructiveCommand(string(sig[1])) {
			return nil
		}
		out = append(out, config.FrameworkCommand{
			Name:        name,
			Label:       name,
			Command:     shell,
			Description: desc,
			Output:      config.CommandOutputText,
			Icon:        "play",
		})
		return nil
	})
	return out
}
