package nginx

import (
	"fmt"
	"io"
	"os"
)

// ReloadOrWarn reloads nginx and, on failure, prints a single warning line
// to stdout. Pass indent="" for top-level output, or a leading-space prefix
// when the warning sits under an indented status line in CLI output.
func ReloadOrWarn(indent string) {
	reloadOrWarn(Reload, os.Stdout, indent)
}

func reloadOrWarn(reload func() error, w io.Writer, indent string) {
	if err := reload(); err != nil {
		fmt.Fprintf(w, "%s[WARN] nginx reload: %v\n", indent, err)
	}
}
