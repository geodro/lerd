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
	command := "podman exec -w /Users/u/alpha lerd-php84-fpm php artisan queue:work"

	script := buildDarwinExecWorkerGuardScript(pidFile, command)

	if !strings.HasPrefix(script, "#!/bin/sh") {
		t.Errorf("guard script should start with shebang, got:\n%s", script)
	}
	if !strings.Contains(script, pidFile) {
		t.Errorf("guard script should mention pid file %q", pidFile)
	}
	if !strings.Contains(script, command) {
		t.Errorf("guard script should wrap command %q", command)
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

func findLine(body, prefix string) string {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, prefix) {
			return line
		}
	}
	return ""
}
