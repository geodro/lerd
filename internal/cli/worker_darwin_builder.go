package cli

import (
	"fmt"
	"strings"
)

// These builders are kept in a non-build-tagged file so they can be unit-
// tested on any platform. worker_darwin.go calls into them.

// buildDarwinExecWorkerService renders the systemd-style service unit that
// `services.Mgr.WriteServiceUnit` (macOS launchd backend) will translate
// into a launchd plist. ExecStart points at a pre-written guard script
// because parseServiceUnit in the launchd translator uses strings.Fields
// on ExecStart, so an inline multi-line script would not survive the
// split. scriptPath must contain no whitespace.
func buildDarwinExecWorkerService(scriptPath, restart string) string {
	return fmt.Sprintf(`[Unit]
Description=Lerd Worker (exec mode)

[Service]
Type=simple
Restart=%s
ExecStart=/bin/sh %s

[Install]
WantedBy=default.target
`, restart, scriptPath)
}

// buildDarwinExecWorkerGuardScript returns the shell script body written
// to disk for each worker in exec mode. It wraps buildWorkerGuard with a
// shebang so launchd's /bin/sh invocation has a self-contained executable.
func buildDarwinExecWorkerGuardScript(pidFile, command string) string {
	return "#!/bin/sh\n" + buildWorkerGuard(pidFile, command)
}

// buildDarwinContainerWorkerUnit renders the quadlet `[Container]` body
// used by macOS's current (container) worker runtime mode. One container
// per worker, spawned from the FPM image or the site's custom image.
// Kept as an explicit builder to match buildDarwinExecWorkerService so
// tests cover both branches uniformly.
func buildDarwinContainerWorkerUnit(
	unitName, phpVersion, sitePath, homeDir, phpConfFile, phpUserIniFile, command, restart string,
	customContainer bool,
) string {
	if customContainer {
		// Custom container sites reuse their own image; the caller has
		// already determined it via podman.CustomImageName(siteName), so
		// here we only build the quadlet template and leave the image
		// name to be filled in by the caller. To keep this builder pure
		// we emit a placeholder that the caller substitutes.
		return fmt.Sprintf(`[Container]
Image=<custom-image>
ContainerName=%s
Network=lerd
Volume=%s/.local/share/lerd/hosts:/etc/hosts:ro
Volume=%s:%s:rw
PodmanArgs=--security-opt=label=disable
WorkingDir=%s
Exec=%s

[Service]
Restart=%s
`,
			unitName,
			homeDir,
			sitePath, sitePath,
			sitePath,
			command,
			restart,
		)
	}
	versionShort := strings.ReplaceAll(phpVersion, ".", "")
	image := "lerd-php" + versionShort + "-fpm:local"
	return fmt.Sprintf(`[Container]
Image=%s
ContainerName=%s
Network=lerd
Volume=%s/.local/share/lerd/hosts:/etc/hosts:ro
Volume=%s:%s:rw
Volume=%s:/usr/local/etc/php/conf.d/99-xdebug.ini:ro
Volume=%s:/usr/local/etc/php/conf.d/98-lerd-user.ini:ro
PodmanArgs=--security-opt=label=disable
WorkingDir=%s
Exec=%s

[Service]
Restart=%s
`,
		image,
		unitName,
		homeDir,
		sitePath, sitePath,
		phpConfFile,
		phpUserIniFile,
		sitePath,
		command,
		restart,
	)
}
