package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/geodro/lerd/internal/config"
	nodeDet "github.com/geodro/lerd/internal/node"
	phpDet "github.com/geodro/lerd/internal/php"
	"github.com/spf13/cobra"
)

// NewWhichCmd returns the which command.
func NewWhichCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "which",
		Short: "Show resolved PHP, Node, document root, and nginx config for the current site",
		RunE:  runWhich,
	}
}

func runWhich(_ *cobra.Command, _ []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	site, err := config.FindSiteByPath(cwd)
	if err != nil {
		return fmt.Errorf("no site registered for %s — link it first with lerd link", cwd)
	}

	phpVersion, _ := phpDet.DetectVersion(cwd)
	nodeVersion, _ := nodeDet.DetectVersion(cwd)

	publicDir := site.PublicDir
	if publicDir == "" {
		if fw, ok := config.GetFramework(site.Framework); ok && fw.PublicDir != "" {
			publicDir = fw.PublicDir
		} else {
			publicDir = "public"
		}
	}

	docRoot := filepath.Join(site.Path, publicDir)
	nginxConf := filepath.Join(config.NginxConfD(), site.Domain+".conf")

	fmt.Printf("  Site         %s\n", site.Domain)
	fmt.Printf("  PHP          %s\n", phpVersion)
	fmt.Printf("  Node         %s\n", nodeVersion)
	fmt.Printf("  Document root  %s\n", docRoot)
	fmt.Printf("  Nginx config   %s\n", nginxConf)

	if site.Secured {
		sslConf := filepath.Join(config.NginxConfD(), site.Domain+"-ssl.conf")
		fmt.Printf("  Nginx SSL      %s\n", sslConf)
	}

	return nil
}
