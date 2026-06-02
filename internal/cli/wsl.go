package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/geodro/lerd/internal/wsl"
	"github.com/spf13/cobra"
)

// NewWSLSetupCmd returns `lerd wsl:setup`, which applies the WSL2-specific
// tweaks the install guide documents: enabling systemd, the journald events
// logger podman needs for `logs --follow`, mirrored networking in the Windows
// .wslconfig, trusting the mkcert root CA from Windows browsers, and masking the
// tray (WSL has no tray host). Everything is idempotent; the only step it can't
// do for you is `wsl --shutdown`, which has to run from Windows.
func NewWSLSetupCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "wsl:setup",
		Short:        "Apply the WSL2 tweaks lerd needs (systemd, podman journald, mirrored networking, Windows CA trust)",
		SilenceUsage: true,
		RunE:         runWSLSetup,
	}
}

func runWSLSetup(cmd *cobra.Command, _ []string) error {
	w := cmd.OutOrStdout()
	if !wsl.IsWSL() {
		return fmt.Errorf("not running inside WSL2 (no WSL_DISTRO_NAME / microsoft kernel); nothing to do")
	}

	needShutdown := false
	fmt.Fprintln(w, "Configuring lerd for WSL2...")

	// 1. systemd — /etc/wsl.conf [boot] systemd=true (root-owned, written via sudo).
	if changed, err := patchRootFile(w, "/etc/wsl.conf", func(c string) (string, bool) {
		return wsl.EnsureSectionLine(c, "boot", "systemd", "systemd=true")
	}); err != nil {
		fmt.Fprintf(w, "  ! systemd: %v (set [boot] systemd=true in /etc/wsl.conf manually)\n", err)
	} else if changed {
		fmt.Fprintln(w, "  ✓ enabled systemd in /etc/wsl.conf")
		needShutdown = true
	} else {
		fmt.Fprintln(w, "  - systemd already enabled")
	}

	// 2. podman events logger — ~/.config/containers/containers.conf. Without
	//    journald here, every `podman logs --follow` (dashboard log panes,
	//    `lerd logs`) errors out on a systemd host.
	home, _ := os.UserHomeDir()
	ccPath := filepath.Join(home, ".config", "containers", "containers.conf")
	if changed, err := patchUserFile(ccPath, func(c string) (string, bool) {
		return wsl.EnsureSectionLine(c, "engine", "events_logger", `events_logger = "journald"`)
	}); err != nil {
		fmt.Fprintf(w, "  ! podman events_logger: %v\n", err)
	} else if changed {
		fmt.Fprintln(w, "  ✓ set podman events_logger = journald")
	} else {
		fmt.Fprintln(w, "  - podman events_logger already journald")
	}

	// 3. mirrored networking — %USERPROFILE%\.wslconfig (Windows side, via /mnt).
	if changed, err := patchWSLConfig(); err != nil {
		fmt.Fprintf(w, "  ! .wslconfig: %v (add [wsl2] networkingMode=mirrored manually)\n", err)
	} else if changed {
		fmt.Fprintln(w, "  ✓ enabled mirrored networking in .wslconfig")
		needShutdown = true
	} else {
		fmt.Fprintln(w, "  - .wslconfig already set for mirrored networking")
	}

	// 4. trust the mkcert root CA from Windows browsers.
	switch trustCAOnWindows() {
	case caTrusted:
		fmt.Fprintln(w, "  ✓ imported mkcert root CA into the Windows trust store")
	case caSkippedNoMkcert:
		fmt.Fprintln(w, "  - skipped Windows CA trust (run `lerd secure` first, then re-run)")
	case caSkippedNoInterop:
		fmt.Fprintln(w, "  ! couldn't reach certutil.exe; import the CA manually (see the WSL2 guide)")
	}

	// 5. tray — no StatusNotifier host on WSL, so mask it to silence the failing unit.
	if maskTrayUnit() {
		fmt.Fprintln(w, "  ✓ masked lerd-tray (no tray host on WSL2)")
	} else {
		fmt.Fprintln(w, "  - lerd-tray already masked or absent")
	}

	fmt.Fprintln(w)
	if needShutdown {
		fmt.Fprintln(w, "Done. One manual step left, from a Windows PowerShell or CMD prompt run:")
		fmt.Fprintln(w, "    wsl --shutdown")
		fmt.Fprintln(w, "then reopen your distro so the systemd / networking changes take effect.")
	} else {
		fmt.Fprintln(w, "Done. No reboot needed.")
	}
	return nil
}

// patchUserFile reads path (treating absence as empty), runs patch, and writes
// back only when patch reports a change. Creates parent dirs as needed.
func patchUserFile(path string, patch func(string) (string, bool)) (bool, error) {
	cur := ""
	if b, err := os.ReadFile(path); err == nil {
		cur = string(b)
	} else if !os.IsNotExist(err) {
		return false, err
	}
	next, changed := patch(cur)
	if !changed {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	return true, os.WriteFile(path, []byte(next), 0o644)
}

// patchRootFile is patchUserFile for a root-owned file: it reads what it can and
// writes the result back through `sudo tee` so the user is prompted for sudo
// only when a change is actually needed.
func patchRootFile(w io.Writer, path string, patch func(string) (string, bool)) (bool, error) {
	cur := ""
	if b, err := os.ReadFile(path); err == nil {
		cur = string(b)
	}
	next, changed := patch(cur)
	if !changed {
		return false, nil
	}
	cmd := exec.Command("sudo", "tee", path)
	cmd.Stdin = strings.NewReader(next)
	cmd.Stdout = io.Discard
	cmd.Stderr = w
	return true, cmd.Run()
}

// patchWSLConfig resolves %USERPROFILE%\.wslconfig via Windows interop and
// applies the recommended [wsl2] lines. Returns changed=false / err set when
// interop is unavailable so the caller can fall back to a manual hint.
func patchWSLConfig() (bool, error) {
	profile, err := windowsUserProfilePath()
	if err != nil {
		return false, err
	}
	path := filepath.Join(profile, ".wslconfig")
	return patchUserFile(path, func(c string) (string, bool) {
		changedAny := false
		for _, kv := range wsl.WSLConfigLines {
			var ch bool
			c, ch = wsl.EnsureSectionLine(c, "wsl2", kv.Key, kv.Line)
			changedAny = changedAny || ch
		}
		return c, changedAny
	})
}

// windowsUserProfilePath returns the WSL path to the Windows user profile dir
// (e.g. /mnt/c/Users/name), using powershell.exe + wslpath interop.
func windowsUserProfilePath() (string, error) {
	out, err := exec.Command("powershell.exe", "-NoProfile", "-Command", "$env:USERPROFILE").Output()
	if err != nil {
		return "", fmt.Errorf("powershell.exe interop unavailable: %w", err)
	}
	win := strings.TrimRight(strings.TrimSpace(string(out)), "\r\n")
	if win == "" {
		return "", fmt.Errorf("USERPROFILE empty")
	}
	p, err := exec.Command("wslpath", "-u", win).Output()
	if err != nil {
		return "", fmt.Errorf("wslpath: %w", err)
	}
	return strings.TrimSpace(string(p)), nil
}

type caResult int

const (
	caTrusted caResult = iota
	caSkippedNoMkcert
	caSkippedNoInterop
)

// trustCAOnWindows copies the mkcert root CA to the Windows profile and imports
// it into the per-user Root store via certutil.exe (no admin needed), so
// https://*.test is trusted by Edge/Chrome on the Windows side.
func trustCAOnWindows() caResult {
	if _, err := exec.LookPath("mkcert"); err != nil {
		return caSkippedNoMkcert
	}
	rootOut, err := exec.Command("mkcert", "-CAROOT").Output()
	if err != nil {
		return caSkippedNoMkcert
	}
	pem := filepath.Join(strings.TrimSpace(string(rootOut)), "rootCA.pem")
	if _, err := os.Stat(pem); err != nil {
		return caSkippedNoMkcert
	}
	profile, err := windowsUserProfilePath()
	if err != nil {
		return caSkippedNoInterop
	}
	dst := filepath.Join(profile, "lerd-rootCA.crt")
	if err := copyFileContents(pem, dst); err != nil {
		return caSkippedNoInterop
	}
	winPath, err := exec.Command("wslpath", "-w", dst).Output()
	if err != nil {
		return caSkippedNoInterop
	}
	if err := exec.Command("certutil.exe", "-addstore", "-user", "-f", "Root", strings.TrimSpace(string(winPath))).Run(); err != nil {
		return caSkippedNoInterop
	}
	return caTrusted
}

func copyFileContents(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o644)
}

// maskTrayUnit masks lerd-tray.service, returning true only when it actually
// changed state (so the caller can distinguish "masked it" from "already off").
func maskTrayUnit() bool {
	out, _ := exec.Command("systemctl", "--user", "is-enabled", "lerd-tray.service").Output()
	if strings.TrimSpace(string(out)) == "masked" {
		return false
	}
	return exec.Command("systemctl", "--user", "mask", "lerd-tray.service").Run() == nil
}
