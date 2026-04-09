package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/geodro/lerd/internal/config"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"
)

// NewRemoteControlCmd returns the `lerd remote-control` parent command with
// on / off / status subcommands. Controls whether the lerd dashboard at
// http://<server>:7073 accepts requests from non-loopback (LAN) sources.
//
// State machine (single field, presence of cfg.UI.PasswordHash):
//
//   - empty:   loopback only — LAN sources get 403 Forbidden
//   - present: loopback bypasses, LAN sources must present HTTP Basic auth
//
// The `/api/remote-setup` bootstrap endpoint is not affected by this gate
// (it has its own token + IP + brute-force gate). Loopback always bypasses
// both states so the local user can never lock themselves out of their own
// machine.
func NewRemoteControlCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remote-control",
		Short: "Toggle dashboard access from LAN clients (off by default)",
		Long: `By default the lerd dashboard at port 7073 only accepts requests from
the loopback interface (127.0.0.1) — LAN sources get 403 Forbidden. Run
'lerd remote-control on' to set a Basic-auth password and grant LAN
clients access. The local user is never affected: loopback always
bypasses authentication so you can't lock yourself out of your own
machine.

The /api/remote-setup laptop-bootstrap endpoint is independent of this
flag — it has its own token + IP + brute-force gate.`,
	}
	cmd.AddCommand(newRemoteControlOnCmd())
	cmd.AddCommand(newRemoteControlOffCmd())
	cmd.AddCommand(newRemoteControlStatusCmd())
	return cmd
}

// NewRemoteControlOnCmd returns the `lerd remote-control:on` colon alias.
func NewRemoteControlOnCmd() *cobra.Command {
	cmd := newRemoteControlOnCmd()
	cmd.Use = "remote-control:on"
	cmd.Hidden = true
	return cmd
}

// NewRemoteControlOffCmd returns the `lerd remote-control:off` colon alias.
func NewRemoteControlOffCmd() *cobra.Command {
	cmd := newRemoteControlOffCmd()
	cmd.Use = "remote-control:off"
	cmd.Hidden = true
	return cmd
}

// NewRemoteControlStatusCmd returns the `lerd remote-control:status` colon alias.
func NewRemoteControlStatusCmd() *cobra.Command {
	cmd := newRemoteControlStatusCmd()
	cmd.Use = "remote-control:status"
	cmd.Hidden = true
	return cmd
}

func newRemoteControlOnCmd() *cobra.Command {
	var username string
	cmd := &cobra.Command{
		Use:   "on [--user <name>]",
		Short: "Enable LAN access to the dashboard with HTTP Basic auth",
		Long: `Prompts for a password (twice for confirmation), bcrypt-hashes it, and
stores the hash and username in ~/.config/lerd/config.yaml. From this
point on, LAN clients hitting the dashboard must present HTTP Basic
auth with the configured username and password. Loopback continues to
bypass authentication.

Re-running this command rotates the password.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := config.LoadGlobal()
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			if !cfg.LAN.Exposed {
				return fmt.Errorf("LAN exposure is off — run `lerd lan:expose` first. Dashboard credentials are only meaningful while the dashboard is reachable from other devices")
			}

			if username == "" {
				username = os.Getenv("USER")
				if username == "" {
					username = os.Getenv("LOGNAME")
				}
				if username == "" {
					username = "lerd"
				}
			}

			password, err := readPasswordTwice()
			if err != nil {
				return err
			}

			hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
			if err != nil {
				return fmt.Errorf("hashing password: %w", err)
			}

			cfg.UI.Username = username
			cfg.UI.PasswordHash = string(hash)
			if err := config.SaveGlobal(cfg); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}

			fmt.Println("Remote dashboard access enabled.")
			fmt.Printf("  Username: %s\n", username)
			fmt.Println("  LAN clients can now reach http://<server-ip>:7073 with HTTP Basic auth.")
			fmt.Println("  Loopback (127.0.0.1) bypasses authentication as always.")
			fmt.Println("  Run `lerd remote-control off` to lock LAN access back down.")
			return nil
		},
	}
	cmd.Flags().StringVarP(&username, "user", "u", "", "username for Basic auth (defaults to $USER)")
	return cmd
}

func newRemoteControlOffCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "off",
		Short: "Disable LAN access to the dashboard",
		Long: `Clears the stored Basic-auth credentials so LAN clients hitting the
dashboard get 403 Forbidden. Loopback access is unaffected.

Safe to run from a loopback shell at any time, even if you've forgotten
the password — you cannot lock yourself out of your own machine.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := config.LoadGlobal()
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}
			cfg.UI.Username = ""
			cfg.UI.PasswordHash = ""
			if err := config.SaveGlobal(cfg); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}
			fmt.Println("Remote dashboard access disabled. LAN clients now get 403 Forbidden.")
			return nil
		},
	}
}

func newRemoteControlStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show whether LAN access to the dashboard is enabled",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := config.LoadGlobal()
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}
			if cfg.UI.PasswordHash == "" {
				fmt.Println("Remote dashboard access: DISABLED")
				fmt.Println("  LAN clients get 403 Forbidden. Loopback (127.0.0.1) is always allowed.")
				fmt.Println("  Enable with: lerd remote-control on")
				return nil
			}
			fmt.Println("Remote dashboard access: ENABLED")
			fmt.Printf("  Username: %s\n", cfg.UI.Username)
			fmt.Println("  LAN clients must present HTTP Basic auth.")
			fmt.Println("  Loopback (127.0.0.1) bypasses authentication.")
			fmt.Println("  Disable with: lerd remote-control off")
			return nil
		},
	}
}

// readPasswordTwice prompts for a password on stdin twice and returns it
// when the two inputs match. Used by `lerd remote-control on` so the user
// doesn't accidentally store a typo'd password they can't reproduce.
func readPasswordTwice() (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("password prompt requires a TTY — pipe `echo` won't work, run interactively")
	}
	fmt.Fprint(os.Stderr, "Password: ")
	first, err := term.ReadPassword(int(os.Stdin.Fd()))
	if err != nil {
		return "", fmt.Errorf("reading password: %w", err)
	}
	fmt.Fprintln(os.Stderr)
	if len(first) == 0 {
		return "", fmt.Errorf("empty password")
	}

	fmt.Fprint(os.Stderr, "Confirm:  ")
	second, err := term.ReadPassword(int(os.Stdin.Fd()))
	if err != nil {
		return "", fmt.Errorf("reading password: %w", err)
	}
	fmt.Fprintln(os.Stderr)

	if string(first) != string(second) {
		return "", fmt.Errorf("passwords do not match")
	}
	return strings.TrimSpace(string(first)), nil
}
