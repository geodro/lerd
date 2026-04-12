package siteops

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/nginx"
	"github.com/geodro/lerd/internal/podman"
)

// RegenerateSiteVhost regenerates the nginx vhost for a site after domain changes.
// If the primary domain changed, the old vhost file is removed. For secured sites
// the SSL vhost is generated and renamed to the main .conf path.
func RegenerateSiteVhost(site *config.Site, oldPrimary string) error {
	newPrimary := site.PrimaryDomain()

	if oldPrimary != newPrimary {
		_ = nginx.RemoveVhost(oldPrimary)
	}

	if site.Secured {
		if err := nginx.GenerateSSLVhost(*site, site.PHPVersion); err != nil {
			return fmt.Errorf("generating SSL vhost: %w", err)
		}
		sslConf := filepath.Join(config.NginxConfD(), newPrimary+"-ssl.conf")
		mainConf := filepath.Join(config.NginxConfD(), newPrimary+".conf")
		_ = os.Remove(mainConf)
		if err := os.Rename(sslConf, mainConf); err != nil {
			return fmt.Errorf("installing SSL vhost: %w", err)
		}
	} else {
		if err := nginx.GenerateVhost(*site, site.PHPVersion); err != nil {
			return fmt.Errorf("generating vhost: %w", err)
		}
	}
	if podman.AfterUnitChange != nil {
		podman.AfterUnitChange("site:" + site.Name)
	}
	return nil
}
