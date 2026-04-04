//go:build darwin

package cli

import "os/exec"

func openBrowser(url string) error {
	return exec.Command("open", url).Start()
}
