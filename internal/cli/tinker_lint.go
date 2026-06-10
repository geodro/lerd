package cli

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/geodro/lerd/internal/config"
	phpDet "github.com/geodro/lerd/internal/php"
	"github.com/geodro/lerd/internal/podman"
)

// LintDiagnostic is one issue surfaced by the linter, suitable for the
// frontend to render as a CodeMirror diagnostic.
type LintDiagnostic struct {
	Line     int    `json:"line"`
	Column   int    `json:"column"`
	Message  string `json:"message"`
	Severity string `json:"severity"` // "error" | "warning"
}

// LintTinkerCode runs `php -l` against the given code in the site's PHP
// container and parses the output. We pipe the code via stdin (`php -l`
// reads from stdin when given `-` or no file argument depending on the
// build, but the most portable is to write the code to a stdin pipe and
// invoke `php -l /dev/stdin`).
func LintTinkerCode(ctx context.Context, sitePath, code string) ([]LintDiagnostic, error) {
	if strings.TrimSpace(code) == "" {
		return nil, nil
	}

	version, err := phpDet.DetectVersion(sitePath)
	if err != nil {
		cfg, cfgErr := config.LoadGlobal()
		if cfgErr != nil {
			return nil, fmt.Errorf("cannot detect PHP version: %w", err)
		}
		version = cfg.PHP.DefaultVersion
	}
	container := fpmContainerForDir(sitePath, version)
	if running, _ := podman.ContainerRunning(container); !running {
		return nil, fmt.Errorf("PHP %s FPM container is not running", version)
	}

	body := strings.TrimLeft(code, " \t\r\n")
	lineOffset := 0
	if !strings.HasPrefix(body, "<?php") && !strings.HasPrefix(body, "<?=") {
		body = "<?php\n" + body
		lineOffset = 1
	}

	cmd := exec.CommandContext(ctx, podman.PodmanBin(),
		"exec", "-i", container, "php", "-l", "/dev/stdin",
	)
	cmd.Stdin = strings.NewReader(body)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	_ = cmd.Run() // non-zero exit means lint errors; we read stdout/stderr regardless

	combined := stdout.String() + stderr.String()
	diags := parsePHPLintOutput(combined)
	for i := range diags {
		diags[i].Line -= lineOffset
		if diags[i].Line < 1 {
			diags[i].Line = 1
		}
	}
	return diags, nil
}

// phpLintRe matches a single php -l error line. Examples:
//
//	"Parse error: syntax error, unexpected token \";\" in /dev/stdin on line 3"
//	"PHP Parse error:  syntax error, unexpected ... in - on line 5"
//	"Fatal error: Cannot use ... in /dev/stdin on line 7"
var phpLintRe = regexp.MustCompile(
	`(?i)(?:PHP\s+)?(Parse error|Fatal error|Warning|Notice|Deprecated):\s*(.+?)\s+in\s+(?:/dev/stdin|-|.+?)\s+on line\s+(\d+)`,
)

func parsePHPLintOutput(output string) []LintDiagnostic {
	var diags []LintDiagnostic
	for _, m := range phpLintRe.FindAllStringSubmatch(output, -1) {
		severity := "error"
		switch strings.ToLower(m[1]) {
		case "warning", "notice", "deprecated":
			severity = "warning"
		}
		line, _ := strconv.Atoi(m[3])
		// PHP's line is 1-based and includes our injected "<?php\n", so the
		// user's first line lives at 1 (we don't subtract).
		diags = append(diags, LintDiagnostic{
			Line:     line,
			Column:   0,
			Message:  strings.TrimSpace(m[2]),
			Severity: severity,
		})
	}
	return diags
}
