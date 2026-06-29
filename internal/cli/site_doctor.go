package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/feedback"
	"github.com/geodro/lerd/internal/sitedoctor"
	"github.com/spf13/cobra"
)

// NewSiteDoctorCmd returns the `site:doctor` command — framework-agnostic
// app-level health checks for a single site (distinct from `lerd doctor`, which
// diagnoses the lerd environment).
func NewSiteDoctorCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:          "site:doctor [domain]",
		Short:        "Run app-level health checks for a site",
		Long:         "Run app-level health checks (env, dependencies, security audit, framework specifics) for a site. Defaults to the site in the current directory; pass a domain to target another.",
		Example:      "  lerd site:doctor\n  lerd site:doctor acme.test\n  lerd site:doctor --json",
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(_ *cobra.Command, args []string) error {
			domain := ""
			if len(args) == 1 {
				domain = args[0]
			}
			return runSiteDoctor(domain, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output the report as JSON")
	return cmd
}

func runSiteDoctor(domain string, asJSON bool) error {
	path, fwName, label, err := resolveSiteDoctorTarget(domain)
	if err != nil {
		return err
	}
	fw, _ := config.GetFrameworkForDir(fwName, path)
	resp := sitedoctor.Run(context.Background(), path, fw)

	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(resp)
	}
	printSiteDoctor(resp, label)
	if resp.Failures > 0 {
		os.Exit(1)
	}
	return nil
}

// resolveSiteDoctorTarget returns the project path, framework name, and a label
// for the report. With a domain it resolves the registered site; without one it
// uses the current directory and detects the framework.
func resolveSiteDoctorTarget(domain string) (path, fwName, label string, err error) {
	if domain != "" {
		site, err := config.FindSiteByDomain(domain)
		if err != nil {
			return "", "", "", fmt.Errorf("site not found: %s", domain)
		}
		return site.Path, site.Framework, domain, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", "", "", err
	}
	name, _ := config.DetectFrameworkForDir(cwd)
	return cwd, name, cwd, nil
}

func printSiteDoctor(resp sitedoctor.Response, label string) {
	fmt.Printf("Doctor %s\n\n", feedback.Dim("· "+label))
	if len(resp.Checks) == 0 {
		fmt.Println(feedback.Dim("  no checks applied to this site"))
		return
	}
	for _, c := range resp.Checks {
		label := c.Label
		if label == "" {
			label = c.Name
		}
		fmt.Printf("  %s %s\n", doctorGlyph(c.Status), label)
		if c.Detail != "" {
			fmt.Printf("      %s\n", feedback.Dim(c.Detail))
		}
		if c.Fix != "" {
			fmt.Printf("      %s %s\n", feedback.Dim("fix:"), c.Fix)
		}
	}
	fmt.Println()
	switch {
	case resp.Failures > 0 || resp.Warnings > 0:
		fmt.Printf("  %s\n", feedback.Dim(fmt.Sprintf("%d failing · %d warning", resp.Failures, resp.Warnings)))
	default:
		fmt.Printf("  %s\n", feedback.Green("all checks pass"))
	}
}

func doctorGlyph(status string) string {
	switch status {
	case sitedoctor.StatusOK:
		return feedback.Green(feedback.GlyphOK)
	case sitedoctor.StatusWarn:
		return feedback.Amber(feedback.GlyphWarn)
	case sitedoctor.StatusFail:
		return feedback.Red(feedback.GlyphFail)
	default:
		return feedback.Dim("?")
	}
}
