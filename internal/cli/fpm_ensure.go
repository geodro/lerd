package cli

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/geodro/lerd/internal/feedback"
	phpDet "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
	"golang.org/x/term"
)

// interactiveTTY reports whether both stdin and stdout are real terminals, so a
// prompt would actually reach a human who can answer it. It deliberately does
// not fall back to /dev/tty (unlike the installer's promptSource): the php/shell
// shims must not steal a piped stdin or block when run from a script or agent.
func interactiveTTY() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

// Seams for the shared FPM up/installed fast path, swappable in tests.
var (
	fpmContainerRunning = podman.ContainerRunning
	fpmIsInstalled      = phpDet.FPMInstalled
	fpmStart            = startFPM
)

// startInstalledFPM handles the two outcomes ensureFPMRunning and ensureFPMStarted
// share before any human decision: the container is already up, or it is installed
// but stopped (start it). handled is true when the container is now up and no
// install prompt is needed; false means the version is not installed.
func startInstalledFPM(version, container string) (handled bool, err error) {
	if running, _ := fpmContainerRunning(container); running {
		return true, nil
	}
	if fpmIsInstalled(version, container) {
		return true, fpmStart(version, container)
	}
	return false, nil
}

// ensureFPMRunning makes sure the FPM container for the detected PHP version is
// up before lerd execs into it, replacing the old "container is not running"
// dead end. Behaviour when the container is down:
//
//   - version installed but stopped → start the unit and wait for it.
//   - version not installed, on a TTY → offer to install it; if the user
//     declines, offer to switch to an already-installed version (persisted to
//     the project's .php-version pin).
//   - version not installed, no TTY → return a clear error.
//
// It returns the version and container the caller should actually exec against,
// which differ from the requested ones when the user switches versions.
func ensureFPMRunning(cwd, version, container string) (string, string, error) {
	for {
		// Already up, or installed-but-stopped (auto-started) — done in both cases.
		if handled, err := startInstalledFPM(version, container); err != nil {
			return version, container, err
		} else if handled {
			return version, container, nil
		}

		// Not installed — needs a human decision. Only prompt at a real
		// interactive terminal: when stdin is piped (a script feeding `php`) or
		// there's no TTY (agent/CI), prompting would steal stdin or block, and
		// silently building an image is too heavy a side effect to assume.
		if !interactiveTTY() {
			return version, container, notInstalledErr(version)
		}
		newVersion, install, err := resolveUninstalledFPM(bufio.NewReader(os.Stdin), cwd, version)
		if err != nil {
			return version, container, err
		}

		if install {
			if err := ensureFPMQuadletTo(version, os.Stderr); err != nil {
				return version, container, fmt.Errorf("installing PHP %s: %w", version, err)
			}
			container = fpmContainerForDir(cwd, version)
			continue
		}

		// User switched to an installed version; loop will auto-start it.
		version = newVersion
		container = fpmContainerForDir(cwd, version)
	}
}

// ensureFPMStarted is the no-switch variant of ensureFPMRunning for callers that
// operate on an explicit, already-chosen PHP version (tinker eval, pest-browser
// and php:bun installs) and have done version-specific work around this check.
// It auto-starts a stopped-but-installed container, offers to install a missing
// version (same version only — never switches), and otherwise errors.
func ensureFPMStarted(version, container string) error {
	if handled, err := startInstalledFPM(version, container); handled || err != nil {
		return err
	}
	if !interactiveTTY() {
		return notInstalledErr(version)
	}
	reader := bufio.NewReader(os.Stdin)
	if !readConfirmAnswerReader(reader, fmt.Sprintf("PHP %s is not installed. Install it now?", version), true) {
		return notInstalledErr(version)
	}
	if err := ensureFPMQuadletTo(version, os.Stderr); err != nil {
		return fmt.Errorf("installing PHP %s: %w", version, err)
	}
	return startFPM(version, container)
}

// startFPM starts a stopped-but-installed FPM container, with a progress spinner.
// The start/wait itself is shared with the MCP exec path via phpDet.StartFPM; the
// spinner goes to stderr so the transparent `php` shim's stdout stays clean.
func startFPM(version, container string) error {
	step := feedback.StartOn(os.Stderr, fmt.Sprintf("Starting PHP %s FPM", version))
	if err := phpDet.StartFPM(version, container); err != nil {
		step.Fail(err)
		return fmt.Errorf("%w (try: %s)", err, serviceStartHint(container))
	}
	step.OK("running")
	return nil
}

// resolveUninstalledFPM drives the prompt for a version that isn't installed.
// It first offers to install the requested version; only if the user declines
// does it list the installed versions to switch to. Returns (chosenVersion,
// installRequested, error): install=true means build the requested version;
// otherwise chosenVersion is an already-installed version to switch to.
func resolveUninstalledFPM(reader *bufio.Reader, cwd, version string) (string, bool, error) {
	if readConfirmAnswerReader(reader, fmt.Sprintf("PHP %s is not installed. Install it now?", version), true) {
		return version, true, nil
	}

	installed := otherInstalledVersions(version)
	if len(installed) == 0 {
		return version, false, notInstalledErr(version)
	}

	chosen := chooseInstalledVersion(reader, installed)
	if chosen == "" {
		return version, false, fmt.Errorf("PHP %s is not installed", version)
	}

	persistPHPVersion(cwd, chosen)
	return chosen, false, nil
}

// readConfirmAnswerReader is readConfirmAnswer over an existing *bufio.Reader so
// the install prompt and the version menu share one buffered reader instead of
// each wrapping /dev/tty separately and racing over buffered bytes.
func readConfirmAnswerReader(reader *bufio.Reader, question string, defaultYes bool) bool {
	feedback.Prompt(question, defaultYes)
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer == "" {
		return defaultYes
	}
	return answer != "n" && answer != "no"
}

// chooseInstalledVersion shows a numbered menu of installed versions and reads
// the user's pick. A number or the literal version both work; blank cancels.
func chooseInstalledVersion(reader *bufio.Reader, versions []string) string {
	feedback.Note("Installed PHP versions you can switch to:")
	for i, v := range versions {
		fmt.Printf("        %d) PHP %s\n", i+1, v)
	}
	fmt.Printf("\n      %s Choose a version %s ",
		feedback.Dim("?"), feedback.Dim(fmt.Sprintf("[1-%d, blank to cancel]", len(versions))))

	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return ""
	}
	if n, err := strconv.Atoi(answer); err == nil && n >= 1 && n <= len(versions) {
		return versions[n-1]
	}
	for _, v := range versions {
		if v == answer {
			return v
		}
	}
	return ""
}

// otherInstalledVersions lists installed PHP versions excluding the requested one.
func otherInstalledVersions(exclude string) []string {
	installed, err := phpDet.ListInstalled()
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(installed))
	for _, v := range installed {
		if v != exclude {
			out = append(out, v)
		}
	}
	return out
}

// persistPHPVersion pins the chosen version for the project by writing the site
// root's .php-version file, the dedicated per-project pin lerd reads before
// composer's constraint. Best-effort: a write failure or a higher-priority
// .lerd.yaml override only warns, since the switch still applies to this run.
func persistPHPVersion(cwd, version string) {
	root := siteRootFor(cwd)
	path := root + "/.php-version"
	if err := os.WriteFile(path, []byte(version+"\n"), 0o644); err != nil {
		feedback.Warn("could not pin PHP %s (%s): %v", version, path, err)
		return
	}
	feedback.Note(fmt.Sprintf("Pinned PHP %s for this project (%s)", version, path))
	if got, err := phpDet.DetectVersion(root); err == nil && got != version {
		feedback.Warn(".lerd.yaml pins PHP %s, which overrides the .php-version file", got)
	}
}

// notInstalledErr builds the error returned when a version isn't installed and
// we can't prompt (no TTY) or have nothing to switch to.
func notInstalledErr(version string) error {
	if installed := otherInstalledVersions(version); len(installed) > 0 {
		return fmt.Errorf("PHP %s is not installed (installed: %s) — pin one with a .php-version file, or run 'lerd install' to add %s",
			version, strings.Join(installed, ", "), version)
	}
	return fmt.Errorf("PHP %s is not installed — run 'lerd install' to add it", version)
}
