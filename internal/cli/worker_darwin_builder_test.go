package cli

import (
	"strings"
	"testing"
)

// These builder tests are platform-agnostic — they exercise pure string
// generation used on macOS in the `exec` and `container` worker modes.
// We test them here so the logic stays covered on Linux CI runs too.

func TestBuildDarwinExecWorkerService_PointsAtGuardScript(t *testing.T) {
	serviceUnit := buildDarwinExecWorkerService("/run/workers/lerd-queue-alpha.sh", "always")

	if !strings.Contains(serviceUnit, "ExecStart=/bin/sh /run/workers/lerd-queue-alpha.sh") {
		t.Errorf("service unit should call guard script via /bin/sh, got:\n%s", serviceUnit)
	}
	if !strings.Contains(serviceUnit, "Restart=always") {
		t.Errorf("service unit missing Restart=always")
	}
	if !strings.Contains(serviceUnit, "WantedBy=default.target") {
		t.Errorf("service unit missing default.target WantedBy")
	}
}

func TestBuildDarwinExecWorkerService_UnquotedPath(t *testing.T) {
	// launchd's parseServiceUnit uses strings.Fields on ExecStart, which
	// splits on whitespace — the path to the guard script must contain
	// no spaces or quotes. Our constructor should produce a clean
	// whitespace-free argument.
	unit := buildDarwinExecWorkerService("/run/workers/lerd-queue-alpha.sh", "always")
	line := findLine(unit, "ExecStart=")
	if line == "" {
		t.Fatalf("no ExecStart= line")
	}
	// "ExecStart=/bin/sh /path" should have exactly 3 whitespace-split
	// fields after the `=`: /bin/sh, /path.
	rhs := strings.TrimPrefix(line, "ExecStart=")
	fields := strings.Fields(rhs)
	if len(fields) != 2 {
		t.Errorf("ExecStart RHS should have 2 fields, got %d: %q", len(fields), rhs)
	}
}

func TestBuildDarwinExecWorkerGuardScript_WrapsPodmanExec(t *testing.T) {
	pidFile := "/run/workers/lerd-queue-alpha.pid"
	podmanBin := "/opt/homebrew/bin/podman"
	container := "lerd-php84-fpm"
	sitePath := "/Users/u/alpha"
	workerCmd := "php artisan queue:work"
	runCmd := "/opt/homebrew/bin/podman exec -w /Users/u/alpha lerd-php84-fpm php artisan queue:work"

	script := buildDarwinExecWorkerGuardScript(pidFile, podmanBin, container, sitePath, workerCmd, runCmd)

	if !strings.HasPrefix(script, "#!/bin/sh") {
		t.Errorf("guard script should start with shebang, got:\n%s", script)
	}
	for _, want := range []string{pidFile, runCmd, container, sitePath, "pgrep -f", "readlink /proc/$p/cwd", "'php artisan queue:work'"} {
		if !strings.Contains(script, want) {
			t.Errorf("guard script missing %q:\n%s", want, script)
		}
	}
}

func TestBuildDarwinContainerWorkerUnit_UsesFPMImage(t *testing.T) {
	unit := buildDarwinContainerWorkerUnit(
		"lerd-queue-alpha",   // unitName
		"8.4",                // phpVersion
		"/Users/u/alpha",     // sitePath
		"/Users/u/home",      // homeDir
		"/lerd/php.conf",     // phpConfFile
		"/lerd/php-user.ini", // phpUserIniFile
		"php artisan queue:work",
		"always",
		false, // custom container
	)

	for _, want := range []string{
		"Image=lerd-php84-fpm:local",
		"ContainerName=lerd-queue-alpha",
		"WorkingDir=/Users/u/alpha",
		"Exec=php artisan queue:work",
		"Restart=always",
	} {
		if !strings.Contains(unit, want) {
			t.Errorf("container unit missing %q:\n%s", want, unit)
		}
	}
}

func TestBuildDarwinContainerWorkerUnit_CustomContainerUsesSiteImage(t *testing.T) {
	unit := buildDarwinContainerWorkerUnit(
		"lerd-custom-alpha",
		"",
		"/Users/u/alpha",
		"/Users/u/home",
		"", "",
		"node worker.js",
		"always",
		true, // custom container = true, image comes from caller
	)
	if strings.Contains(unit, "lerd-php") {
		t.Errorf("custom container unit should not reference a PHP FPM image:\n%s", unit)
	}
}

func TestBuildDarwinHostWorkerService_PointsAtGuardScript(t *testing.T) {
	unit := buildDarwinHostWorkerService("/run/workers/lerd-vite-alpha.sh", "always")

	if !strings.Contains(unit, "ExecStart=/bin/sh /run/workers/lerd-vite-alpha.sh") {
		t.Errorf("host worker service unit should /bin/sh the guard, got:\n%s", unit)
	}
	if !strings.Contains(unit, "Restart=always") {
		t.Errorf("host worker service unit missing Restart=always")
	}
	if !strings.Contains(unit, "Description=Lerd Worker (host mode)") {
		t.Errorf("host worker service unit missing host-mode description")
	}

	line := findLine(unit, "ExecStart=")
	rhs := strings.TrimPrefix(line, "ExecStart=")
	if fields := strings.Fields(rhs); len(fields) != 2 {
		t.Errorf("ExecStart RHS must split into 2 fields for the launchd translator; got %d: %q", len(fields), rhs)
	}
}

func TestBuildDarwinHostWorkerGuardScript_WrapsFnmExec(t *testing.T) {
	fnm := "/Users/u/.local/share/lerd/bin/fnm"
	binDir := "/Users/u/.local/share/lerd/bin"
	sitePath := "/Users/u/alpha"
	command := "npm run dev"

	script := buildDarwinHostWorkerGuardScript(fnm, binDir, "22", sitePath, command, "")

	if !strings.HasPrefix(script, "#!/bin/sh") {
		t.Errorf("guard script should start with shebang, got:\n%s", script)
	}
	for _, want := range []string{
		"cd '/Users/u/alpha'",
		"'/Users/u/.local/share/lerd/bin/fnm' exec --using=22",
		"-- /bin/sh -c 'npm run dev'",
		"export PATH=",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("guard script missing %q:\n%s", want, script)
		}
	}
}

func TestBuildDarwinHostWorkerGuardScript_EscapesSingleQuotes(t *testing.T) {
	// A worker command containing a single quote must survive the
	// '"'"' shell-escape idiom we use to keep the surrounding 'sh -c …'
	// quoting intact. Without the escape, the trailing ' would close
	// the sh -c string early and the rest of the command would parse
	// as separate shell tokens.
	script := buildDarwinHostWorkerGuardScript(
		"/bin/fnm", "/Users/u/.local/share/lerd/bin", "22", "/site",
		`node -e 'console.log("x")'`, "",
	)
	if !strings.Contains(script, `'"'"'console.log("x")'"'"'`) {
		t.Errorf("expected escaped single quotes in guard script, got:\n%s", script)
	}
}

// Vite's Inertia/Wayfinder plugin shells out to `php artisan` from inside
// `npm run dev`. lerd's BinDir holds the php/composer/laravel shims, so
// it must lead PATH for the subprocess to find them — issue #375.
func TestBuildDarwinHostWorkerGuardScript_PrependsLerdBinDirToPath(t *testing.T) {
	binDir := "/Users/u/.local/share/lerd/bin"
	script := buildDarwinHostWorkerGuardScript("/bin/fnm", binDir, "22", "/site", "npm run dev", "")
	want := `export PATH="/Users/u/.local/share/lerd/bin:/opt/homebrew/bin:`
	if !strings.Contains(script, want) {
		t.Errorf("guard script must prepend lerd BinDir to PATH; got:\n%s", script)
	}
}

func findLine(body, prefix string) string {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, prefix) {
			return line
		}
	}
	return ""
}
