package cli

import "testing"

const xdebugctlPS = `       PID      RSS     TIME COMMAND
        15    50384    76.20 /home/george/Projects/tallyboard/artisan (Xdebug3.5.3)
        36    49527    76.11 /home/george/Projects/starlane.com/artisan (Xdebug3.5.3)
        52    51077    75.19 /home/george/Projects/tallyboard/artisan (Xdebug3.5.3)
       104 Error: No response on: @xdebug-ctrl.104
`

func TestParseXdebugctlProcs_skipsHeaderAndErrors(t *testing.T) {
	procs := parseXdebugctlProcs(xdebugctlPS)
	if len(procs) != 3 {
		t.Fatalf("want 3 procs (header + Error row skipped), got %d: %+v", len(procs), procs)
	}
	if procs[0].pid != 15 || procs[0].command != "/home/george/Projects/tallyboard/artisan (Xdebug3.5.3)" {
		t.Errorf("unexpected first proc: %+v", procs[0])
	}
}

// xdebugctl colours its output; the parser must strip ANSI so the PID is field 0.
func TestParseXdebugctlProcs_stripsANSIColour(t *testing.T) {
	const colored = "\x1b[2m       PID\x1b[0m      RSS     TIME \x1b[97mCOMMAND\x1b[0m\n" +
		"\x1b[2m        81\x1b[0m    49183    37.17 \x1b[97m/home/george/Projects/tallyboard/artisan\x1b[0m \x1b[2m(Xdebug3.5.3)\x1b[0m\n"
	procs := parseXdebugctlProcs(colored)
	if len(procs) != 1 {
		t.Fatalf("want 1 proc from coloured output, got %d: %+v", len(procs), procs)
	}
	if procs[0].pid != 81 {
		t.Errorf("pid = %d, want 81 (ANSI not stripped?)", procs[0].pid)
	}
	if scoped := procsForSite(procs, "/home/george/Projects/tallyboard"); len(scoped) != 1 {
		t.Errorf("coloured path should still scope, got %d", len(scoped))
	}
}

func TestProcsForSite_scopesByProjectPath(t *testing.T) {
	procs := parseXdebugctlProcs(xdebugctlPS)
	scoped := procsForSite(procs, "/home/george/Projects/tallyboard")
	if len(scoped) != 2 {
		t.Fatalf("want 2 tallyboard procs, got %d: %+v", len(scoped), scoped)
	}
	for _, p := range scoped {
		if p.pid != 15 && p.pid != 52 {
			t.Errorf("unexpected pid in scoped set: %d", p.pid)
		}
	}

	// Empty path is a no-op (returns all).
	if got := len(procsForSite(procs, "")); got != 3 {
		t.Errorf("empty sitePath should return all procs, got %d", got)
	}
}

// A site path must match on a directory boundary, not a bare substring, so
// /proj/app does not capture /proj/app2's workers.
func TestProcsForSite_pathBoundaryNotSubstring(t *testing.T) {
	procs := []xdebugProc{
		{pid: 1, command: "/proj/app/artisan (Xdebug3.5.3)"},
		{pid: 2, command: "/proj/app2/artisan (Xdebug3.5.3)"},
	}
	scoped := procsForSite(procs, "/proj/app")
	if len(scoped) != 1 || scoped[0].pid != 1 {
		t.Fatalf("want only /proj/app (pid 1), got %+v", scoped)
	}
	// A trailing slash on the site path must not break matching.
	if got := procsForSite(procs, "/proj/app/"); len(got) != 1 || got[0].pid != 1 {
		t.Errorf("trailing-slash sitePath should still match pid 1, got %+v", got)
	}
}
