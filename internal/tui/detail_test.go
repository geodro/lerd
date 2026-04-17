package tui

import (
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/siteinfo"
)

func TestDetailRows_IncludesDomainsWorkersAndToggles(t *testing.T) {
	s := &siteinfo.EnrichedSite{
		Name:           "alpha",
		Domains:        []string{"alpha.test", "alpha-admin.test"},
		PHPVersion:     "8.3",
		NodeVersion:    "20",
		HasQueueWorker: true,
		HasHorizon:     true,
	}
	rows := detailRows(s)
	kinds := rowKinds(rows)

	// First kindInfo is a header placeholder, then 2 domain rows, then
	// the add-domain row, then workers, PHP, Node, HTTPS, LAN share.
	assertKindCount(t, kinds, kindDomain, 2)
	assertKindCount(t, kinds, kindDomainAdd, 1)
	assertKindCount(t, kinds, kindWorker, 2)
	assertKindCount(t, kinds, kindPHP, 1)
	assertKindCount(t, kinds, kindNode, 1)
	assertKindCount(t, kinds, kindHTTPS, 1)
	assertKindCount(t, kinds, kindLANShare, 1)
}

func TestDetailRows_CustomContainerSkipsPHP(t *testing.T) {
	s := &siteinfo.EnrichedSite{
		Name:          "nodeapp",
		Domains:       []string{"nodeapp.test"},
		ContainerPort: 3000,
	}
	rows := detailRows(s)
	kinds := rowKinds(rows)
	assertKindCount(t, kinds, kindPHP, 0)
	assertKindCount(t, kinds, kindNode, 0) // NodeVersion empty
	assertKindCount(t, kinds, kindHTTPS, 1)
	assertKindCount(t, kinds, kindLANShare, 1)
}

func TestNavigableRows_SkipsInfo(t *testing.T) {
	rows := []detailRow{
		{kind: kindInfo},
		{kind: kindWorker, workerName: "queue"},
		{kind: kindInfo},
		{kind: kindHTTPS},
	}
	nav := navigableRows(rows)
	if len(nav) != 2 {
		t.Fatalf("expected 2 navigable rows, got %d", len(nav))
	}
	if rows[nav[0]].kind != kindWorker || rows[nav[1]].kind != kindHTTPS {
		t.Fatalf("nav picked wrong rows: %+v", nav)
	}
}

func TestTrimTLD_StripsConfiguredTLD(t *testing.T) {
	// Relies on the installed config; if the TLD isn't "test", the
	// fallback path still trims the last dotted component. Both outcomes
	// strip the suffix from "name.test".
	if got := trimTLD("name.test"); got != "name" {
		t.Errorf("trimTLD(name.test) = %q, want name", got)
	}
	if got := trimTLD("sub.name.test"); got != "sub.name" {
		t.Errorf("trimTLD(sub.name.test) = %q, want sub.name", got)
	}
	if got := trimTLD("plain"); got != "plain" {
		t.Errorf("trimTLD(plain) = %q, want plain (no dot, no change)", got)
	}
}

func TestDomainRole_MarksPrimary(t *testing.T) {
	s := &siteinfo.EnrichedSite{Domains: []string{"first.test", "second.test"}}
	if got := domainRole(s, "first.test"); !strings.Contains(got, "primary") {
		t.Errorf("first domain should be primary, got %q", got)
	}
	if got := domainRole(s, "second.test"); !strings.Contains(got, "alias") {
		t.Errorf("second domain should be alias, got %q", got)
	}
}

func TestLogTargetsForSite_IncludesFPMAndWorkers(t *testing.T) {
	s := &siteinfo.EnrichedSite{
		Name:           "alpha",
		Path:           "/tmp/missing-so-no-app-logs",
		PHPVersion:     "8.3",
		HasQueueWorker: true,
		HasHorizon:     true,
	}
	targets := logTargetsForSite(s)
	if len(targets) < 3 {
		t.Fatalf("expected at least 3 targets (fpm+queue+horizon), got %d", len(targets))
	}
	if targets[0].Kind != kindPodman || !strings.Contains(targets[0].ID, "lerd-php83-fpm") {
		t.Errorf("first target should be fpm container, got %+v", targets[0])
	}
	// Every worker target should be a journal tail, not a podman one.
	for _, t2 := range targets[1:] {
		if t2.Kind != kindJournal && t2.Kind != kindFile {
			t.Errorf("non-fpm target %s should be journal or file, got %v", t2.ID, t2.Kind)
		}
	}
}

func TestLogTargetsForSite_CustomContainer(t *testing.T) {
	s := &siteinfo.EnrichedSite{
		Name:          "nodeapp",
		ContainerPort: 3000,
	}
	targets := logTargetsForSite(s)
	if len(targets) != 1 || targets[0].Kind != kindPodman {
		t.Fatalf("custom container should get exactly one podman target, got %+v", targets)
	}
	if !strings.Contains(targets[0].ID, "lerd-custom-nodeapp") {
		t.Errorf("expected lerd-custom-nodeapp, got %s", targets[0].ID)
	}
}

func TestContainerForSite(t *testing.T) {
	if got := containerForSite(&siteinfo.EnrichedSite{ContainerPort: 3000, Name: "x"}); !strings.Contains(got, "lerd-custom-x") {
		t.Errorf("custom container, got %s", got)
	}
	if got := containerForSite(&siteinfo.EnrichedSite{PHPVersion: "8.3"}); got != "lerd-php83-fpm" {
		t.Errorf("php site, got %s", got)
	}
	if got := containerForSite(&siteinfo.EnrichedSite{}); got != "" {
		t.Errorf("empty site should return empty, got %s", got)
	}
}

func TestServiceStatesByName(t *testing.T) {
	m := NewModel("test")
	m.snap = Snapshot{
		Services: []ServiceRow{
			{Name: "mysql", State: stateRunning},
			{Name: "redis", State: stateStopped},
		},
	}
	states := m.serviceStatesByName()
	if states["mysql"] != stateRunning {
		t.Errorf("mysql state wrong")
	}
	if states["redis"] != stateStopped {
		t.Errorf("redis state wrong")
	}
	if _, ok := states["nope"]; ok {
		t.Errorf("unknown service shouldn't be in map")
	}
}

func rowKinds(rows []detailRow) []detailKind {
	out := make([]detailKind, len(rows))
	for i, r := range rows {
		out[i] = r.kind
	}
	return out
}

func assertKindCount(t *testing.T, kinds []detailKind, want detailKind, count int) {
	t.Helper()
	n := 0
	for _, k := range kinds {
		if k == want {
			n++
		}
	}
	if n != count {
		t.Errorf("kind %d: got %d occurrences, want %d (kinds=%v)", want, n, count, kinds)
	}
}
