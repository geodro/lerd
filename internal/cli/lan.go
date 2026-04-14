package cli

import (
	"fmt"
	"net/http"
	"os"

	"github.com/geodro/lerd/internal/config"
	"github.com/spf13/cobra"
)

// NewLANCmd returns the `lerd lan` parent command. Subcommands flip lerd
// between the safe-on-coffee-shop-wifi default (everything bound to
// 127.0.0.1) and the LAN-exposed state (containers bound to 0.0.0.0,
// dnsmasq answering with the LAN IP, lerd-ui on 0.0.0.0:7073). The
// previous standalone `lerd dns:expose` flag was folded in here because
// there is no meaningful state where the DNS resolver answers the LAN
// but the actual services don't.
func NewLANCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lan",
		Short: "Expose lerd to other devices on the local network",
		Long: `Toggle whether lerd's services are reachable from other devices on
the local network.

By default lerd binds every container PublishPort to 127.0.0.1 and the
dashboard (lerd-ui) listens only on 127.0.0.1:7073. Other devices on the
LAN cannot reach the sites, services, mail UI, or dashboard. This is the
safe default for untrusted networks (cafés, conference wifi, hotel
networks).

Run 'lerd lan:expose on' to flip everything to 0.0.0.0 binds and start
the userspace DNS forwarder so LAN devices can resolve and reach your
sites. Run 'lerd lan:expose off' to revert.`,
	}
	cmd.AddCommand(newLANExposeCmd())
	cmd.AddCommand(newLANUnexposeCmd())
	cmd.AddCommand(newLANStatusCmd())
	cmd.AddCommand(newLANShareCmd())
	cmd.AddCommand(newLANUnshareCmd())
	return cmd
}

// NewLANExposeCmd returns the `lerd lan:expose` colon-style alias.
func NewLANExposeCmd() *cobra.Command {
	cmd := newLANExposeCmd()
	cmd.Use = "lan:expose"
	cmd.Hidden = true
	return cmd
}

// NewLANUnexposeCmd returns the `lerd lan:unexpose` colon-style alias.
func NewLANUnexposeCmd() *cobra.Command {
	cmd := newLANUnexposeCmd()
	cmd.Use = "lan:unexpose"
	cmd.Hidden = true
	return cmd
}

// NewLANStatusCmd returns the `lerd lan:status` colon-style alias.
func NewLANStatusCmd() *cobra.Command {
	cmd := newLANStatusCmd()
	cmd.Use = "lan:status"
	cmd.Hidden = true
	return cmd
}

// NewLANShareCmd returns the `lerd lan:share` colon-style alias.
func NewLANShareCmd() *cobra.Command {
	cmd := newLANShareCmd()
	cmd.Use = "lan:share"
	return cmd
}

// NewLANUnshareCmd returns the `lerd lan:unshare` colon-style alias.
func NewLANUnshareCmd() *cobra.Command {
	cmd := newLANUnshareCmd()
	cmd.Use = "lan:unshare"
	return cmd
}

func newLANExposeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "expose",
		Short: "Make lerd reachable from other devices on the local network",
		Long: `Flips lerd from its safe loopback default to LAN-exposed mode:

  - Rewrites every installed lerd-* container quadlet so PublishPort=
    bindings drop the 127.0.0.1 prefix (sites, services, mail UI, etc.
    become reachable from other devices on the LAN).
  - Restarts each affected container so the new bind takes effect.
  - Rewrites the dnsmasq config to answer *.test queries with the host's
    auto-detected LAN IP and starts the userspace lerd-dns-forwarder so
    LAN devices can resolve those names.

The dashboard at port 7073 is still gated by the remote-control middleware:
LAN clients get 403 unless you have run 'lerd remote-control on' to set
HTTP Basic auth credentials. The two switches are independent — sites
become LAN-reachable on lan:expose, the dashboard becomes LAN-reachable
on remote-control on, and you can have either or both.

The state is persisted in ~/.config/lerd/config.yaml so reboots and
reinstalls restore the exposed state. Idempotent — re-running heals any
state drift between the config flag and the actual on-disk units.

Make sure your firewall allows the relevant ports (typically 80, 443,
5300, 7073) from the devices you want to grant access. 'lerd remote-setup'
generates a one-shot bootstrap code for a remote machine.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			lanIP, err := EnableLANExposure(func(step string) {
				fmt.Printf("  • %s\n", step)
			})
			if err != nil {
				return err
			}
			cfg, _ := config.LoadGlobal()
			fmt.Printf("Lerd is now reachable on the LAN at %s.\n", lanIP)
			fmt.Printf("  - sites: http://*.test (resolved via dnsmasq on %s:5300)\n", lanIP)
			if cfg != nil && cfg.UI.PasswordHash != "" {
				fmt.Printf("  - dashboard: http://%s:7073 (HTTP Basic auth required)\n", lanIP)
			} else {
				fmt.Printf("  - dashboard: http://%s:7073 (LAN clients get 403 — run `lerd remote-control on` to grant LAN access)\n", lanIP)
			}
			fmt.Println("Make sure your firewall allows ports 80, 443, 5300, 7073 from the devices you want to grant access.")
			fmt.Println("Run `lerd remote-setup` to generate a one-time bootstrap code for a remote machine.")
			return nil
		},
	}
}

func newLANUnexposeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unexpose",
		Short: "Restrict lerd to loopback only — safe for untrusted wifi",
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := DisableLANExposure(func(step string) {
				fmt.Printf("  • %s\n", step)
			}); err != nil {
				return err
			}
			fmt.Println("Lerd is now restricted to loopback (127.0.0.1).")
			fmt.Println("LAN devices can no longer reach sites, services, or the dashboard.")
			fmt.Println("Any active remote-setup code has been revoked.")
			return nil
		},
	}
}

func newLANShareCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "share",
		Short: "Share the current site on a stable LAN port (no DNS setup required on clients)",
		Long: `Assigns a stable port to the current site and starts a host-level reverse
proxy on 0.0.0.0:<port>. Any device on the same network can reach the site
at http://<your-LAN-IP>:<port> without configuring DNS or a resolver.

The proxy rewrites the Host header so nginx routes correctly, and rewrites
absolute URLs in HTML/CSS/JS responses so asset and redirect URLs point to
the LAN address instead of the .test domain.

The assigned port is stored in sites.yaml and reused across restarts.
Run 'lerd lan:unshare' to stop sharing and release the port.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			siteName, err := queueSiteName(cwd)
			if err != nil {
				return err
			}
			// Persist the port assignment (daemon will start the proxy).
			port, err := LANShareEnsurePort(siteName)
			if err != nil {
				return err
			}
			// Tell the running daemon to start the proxy now.
			site, _ := config.FindSite(siteName)
			if site != nil {
				notifyDaemon(site.PrimaryDomain(), "lan:share") //nolint:errcheck
			}
			ip, _ := detectPrimaryLANIP()
			if ip == "" {
				ip = "<your-LAN-IP>"
			}
			shareURL := fmt.Sprintf("http://%s:%d", ip, port)
			fmt.Printf("Sharing %s at %s\n", siteName, shareURL)
			fmt.Println("Other devices on the network can use that URL directly — no DNS setup needed.")
			fmt.Println()
			PrintLANShareQR(shareURL)
			fmt.Println()
			fmt.Println("Run 'lerd lan:unshare' to stop.")
			return nil
		},
	}
}

func newLANUnshareCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unshare",
		Short: "Stop LAN sharing for the current site",
		RunE: func(_ *cobra.Command, _ []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			siteName, err := queueSiteName(cwd)
			if err != nil {
				return err
			}
			site, err := config.FindSite(siteName)
			if err != nil {
				return err
			}
			// Tell the running daemon to stop the proxy and clear the port.
			// If the daemon is not reachable, clear the port directly so the
			// proxy is not restored on next daemon start.
			if nErr := notifyDaemon(site.PrimaryDomain(), "lan:unshare"); nErr != nil {
				site.LANPort = 0
				_ = config.AddSite(*site)
			}
			fmt.Printf("LAN sharing stopped for %s.\n", siteName)
			return nil
		},
	}
}

// notifyDaemon posts an action to the running lerd-ui daemon API. It is a
// best-effort call; callers should handle errors gracefully.
func notifyDaemon(domain, action string) error {
	resp, err := http.Post(
		fmt.Sprintf("http://127.0.0.1:7073/api/sites/%s/%s", domain, action),
		"application/json", nil,
	)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func newLANStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show whether lerd is currently exposed to the local network",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := config.LoadGlobal()
			if err != nil {
				return err
			}
			if cfg.LAN.Exposed {
				lanIP, _ := detectPrimaryLANIP()
				if lanIP == "" {
					lanIP = "(unknown)"
				}
				fmt.Printf("Lerd is exposed to the LAN at %s.\n", lanIP)
			} else {
				fmt.Println("Lerd is loopback-only (127.0.0.1). LAN devices cannot reach it.")
			}
			return nil
		},
	}
}
