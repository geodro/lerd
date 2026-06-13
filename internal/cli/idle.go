package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/geodro/lerd/internal/config"
)

// NewIdleCmd returns the parent `lerd idle` command: a global on/off toggle, a
// global timeout, and a status readout. Idle-suspend is a single global policy,
// not configured per site.
func NewIdleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "idle",
		Short: "Activity-driven worker suspension (idle-suspend)",
		Long: `Idle-suspend gracefully stops each site's suspendable workers (queue, horizon)
once the site has seen no activity for the timeout, and resumes them on the next
request. It is a single global setting, off by default.`,
	}
	cmd.AddCommand(newIdleStatusCmd())
	cmd.AddCommand(newIdleToggleCmd("on", true))
	cmd.AddCommand(newIdleToggleCmd("off", false))
	cmd.AddCommand(newIdleTimeoutCmd())
	cmd.AddCommand(newIdlePinCmd("pin", true))
	cmd.AddCommand(newIdlePinCmd("unpin", false))
	return cmd
}

func newIdlePinCmd(verb string, pinned bool) *cobra.Command {
	short := "Pin a site so idle-suspend never sleeps it"
	if !pinned {
		short = "Unpin a site so idle-suspend can sleep it again"
	}
	return &cobra.Command{
		Use:   verb + " <site>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if err := SetSitePinned(args[0], pinned); err != nil {
				return err
			}
			verbed := "pinned"
			if !pinned {
				verbed = "unpinned"
			}
			fmt.Printf("%s %s (idle-suspend %s).\n", verbed, args[0], map[bool]string{true: "off", false: "on"}[pinned])
			return nil
		},
	}
}

// SetSitePinned toggles whether a site is excluded from idle-suspend.
func SetSitePinned(name string, pinned bool) error {
	site, err := config.FindSite(name)
	if err != nil {
		return err
	}
	site.Pinned = pinned
	return config.AddSite(*site)
}

func newIdleToggleCmd(verb string, enabled bool) *cobra.Command {
	return &cobra.Command{
		Use:   verb,
		Short: fmt.Sprintf("%s idle-suspend", map[bool]string{true: "Enable", false: "Disable"}[enabled]),
		Args:  cobra.NoArgs,
		RunE:  func(_ *cobra.Command, _ []string) error { return setIdleEnabled(enabled) },
	}
}

func newIdleTimeoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "timeout <duration>",
		Short: "Set the idle timeout (e.g. 30m, 2h)",
		Args:  cobra.ExactArgs(1),
		RunE:  func(_ *cobra.Command, args []string) error { return setIdleTimeout(args[0]) },
	}
}

func onOff(b bool) string {
	if b {
		return "enabled"
	}
	return "disabled"
}

func setIdleEnabled(enabled bool) error {
	cfg, err := config.LoadGlobal()
	if err != nil {
		return err
	}
	cfg.IdleSuspend.Enabled = enabled
	if err := config.SaveGlobal(cfg); err != nil {
		return err
	}
	fmt.Printf("Idle-suspend %s.\n", onOff(enabled))
	return nil
}

func setIdleTimeout(dur string) error {
	d, err := time.ParseDuration(dur)
	if err != nil || d <= 0 {
		return fmt.Errorf("invalid duration %q (use e.g. 30m, 2h)", dur)
	}
	cfg, err := config.LoadGlobal()
	if err != nil {
		return err
	}
	cfg.IdleSuspend.Timeout = dur
	if err := config.SaveGlobal(cfg); err != nil {
		return err
	}
	fmt.Printf("Idle timeout set to %s.\n", compactDuration(d))
	return nil
}

func newIdleStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show each site's idle-suspend policy and last-active time",
		Args:  cobra.NoArgs,
		RunE:  func(_ *cobra.Command, _ []string) error { return runIdleStatus() },
	}
}

// idleSiteState is the slice of the /api/sites payload idle status needs: the
// last-active time lives only in the lerd-ui process, so we ask it over the
// unix socket rather than trying to reconstruct it in the CLI. Worktrees idle on
// their own timers, so each carries its own last-active and suspended set.
type idleSiteState struct {
	Name       string        `json:"name"`
	LastActive int64         `json:"last_active"`
	Worktrees  []idleWtState `json:"worktrees"`
}

type idleWtState struct {
	Branch               string   `json:"branch"`
	LastActive           int64    `json:"last_active"`
	IdleSuspendedWorkers []string `json:"idle_suspended_workers"`
}

func runIdleStatus() error {
	cfg, err := config.LoadGlobal()
	if err != nil {
		return err
	}
	reg, err := config.LoadSites()
	if err != nil {
		return err
	}

	fmt.Printf("Idle-suspend: %s, timeout %s\n\n", onOff(cfg.IdleSuspend.Enabled), compactDuration(cfg.IdleSuspendTimeout()))

	states, uiErr := fetchIdleSites()
	lastActive := make(map[string]int64, len(states))
	worktrees := make(map[string][]idleWtState, len(states))
	for _, st := range states {
		lastActive[st.Name] = st.LastActive
		if len(st.Worktrees) > 0 {
			worktrees[st.Name] = st.Worktrees
		}
	}

	timeout := cfg.IdleSuspendTimeout()
	now := time.Now()
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 3, ' ', 0)
	for _, s := range reg.Sites {
		if s.Ignored {
			continue
		}
		fmt.Fprintf(tw, "  %s\t%s\n", s.Name, idleSiteStatus(s, lastActive, uiErr, timeout, now))
		// A worktree idles independently, so list each under its site. The site's
		// pause/pin still applies (the engine skips a paused or pinned site whole).
		for _, wt := range worktrees[s.Name] {
			label := "  " + s.Name + "/" + wt.Branch
			fmt.Fprintf(tw, "  %s\t%s\n", label, idleWorktreeStatus(s, wt, uiErr, timeout, now))
		}
	}
	return tw.Flush()
}

// idleSiteStatus renders a single unambiguous state for a site: paused sites are
// excluded from idle-suspend; a site whose workers are suspended, or that has
// gone past the timeout with nothing left to suspend, reads "idle"; anything
// more recent reads "active".
func idleSiteStatus(s config.Site, lastActive map[string]int64, uiErr error, timeout time.Duration, now time.Time) string {
	if s.Paused {
		return "paused"
	}
	if s.Pinned {
		return "pinned"
	}
	if uiErr != nil {
		return "(lerd-ui not running)"
	}
	return idleTimingStatus(lastActive[s.Name], s.IdleSuspendedWorkers, timeout, now)
}

// idleWorktreeStatus renders a worktree's idle state. It inherits the site's
// pause/pin (the engine never sleeps a paused or pinned site's worktrees), then
// falls to the worktree's own last-active and suspended set.
func idleWorktreeStatus(s config.Site, wt idleWtState, uiErr error, timeout time.Duration, now time.Time) string {
	if s.Paused {
		return "paused"
	}
	if s.Pinned {
		return "pinned"
	}
	if uiErr != nil {
		return "(lerd-ui not running)"
	}
	return idleTimingStatus(wt.LastActive, wt.IdleSuspendedWorkers, timeout, now)
}

// idleTimingStatus turns a last-active time and a suspended-worker set into the
// "idle Nm" / "active Nm ago" / "no activity yet" readout shared by sites and
// worktrees. A non-empty suspended set always reads idle (its workers are stopped).
func idleTimingStatus(lastActiveUnix int64, suspended []string, timeout time.Duration, now time.Time) string {
	hasTS := lastActiveUnix > 0
	var elapsed time.Duration
	if hasTS {
		elapsed = now.Sub(time.Unix(lastActiveUnix, 0))
	}
	if len(suspended) > 0 || (hasTS && elapsed >= timeout) {
		if hasTS {
			return "idle " + compactDuration(elapsed)
		}
		return "idle"
	}
	if !hasTS {
		return "no activity yet"
	}
	return "active " + compactDuration(elapsed) + " ago"
}

// fetchIdleSites asks lerd-ui for the per-site (and per-worktree) idle state,
// since last-active times live only in the lerd-ui process. A non-nil error means
// lerd-ui is unreachable, which the caller renders rather than failing.
func fetchIdleSites() ([]idleSiteState, error) {
	body, code, err := getUnix("/api/sites")
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("lerd-ui returned %d", code)
	}
	var sites []idleSiteState
	if err := json.Unmarshal(body, &sites); err != nil {
		return nil, err
	}
	return sites, nil
}

// compactDuration renders a duration as the largest single unit (41m, 2h, 3d),
// good enough for an at-a-glance "how long ago" / "how long until" readout.
func compactDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours())/24)
	}
}
