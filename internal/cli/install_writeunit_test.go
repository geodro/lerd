package cli

import (
	"errors"
	"testing"

	"github.com/geodro/lerd/internal/services"
)

type fakeServiceMgr struct {
	calls        []string
	writeChanged bool
	writeErr     error
	reloadErr    error
}

func (f *fakeServiceMgr) WriteServiceUnit(string, string) error { return nil }
func (f *fakeServiceMgr) WriteServiceUnitIfChanged(name, _ string) (bool, error) {
	f.calls = append(f.calls, "write:"+name)
	return f.writeChanged, f.writeErr
}
func (f *fakeServiceMgr) RemoveServiceUnit(string) error                       { return nil }
func (f *fakeServiceMgr) WriteTimerUnitIfChanged(string, string) (bool, error) { return false, nil }
func (f *fakeServiceMgr) RemoveTimerUnit(string) error                         { return nil }
func (f *fakeServiceMgr) ListTimerUnits(string) []string                       { return nil }
func (f *fakeServiceMgr) ListServiceUnits(string) []string                     { return nil }
func (f *fakeServiceMgr) WriteContainerUnit(string, string) error              { return nil }
func (f *fakeServiceMgr) ContainerUnitInstalled(string) bool                   { return false }
func (f *fakeServiceMgr) RemoveContainerUnit(string) error                     { return nil }
func (f *fakeServiceMgr) ListContainerUnits(string) []string                   { return nil }
func (f *fakeServiceMgr) DaemonReload() error {
	f.calls = append(f.calls, "reload")
	return f.reloadErr
}
func (f *fakeServiceMgr) Start(string) error                { return nil }
func (f *fakeServiceMgr) Stop(string) error                 { return nil }
func (f *fakeServiceMgr) Restart(string) error              { return nil }
func (f *fakeServiceMgr) Enable(string) error               { return nil }
func (f *fakeServiceMgr) Disable(string) error              { return nil }
func (f *fakeServiceMgr) IsActive(string) bool              { return false }
func (f *fakeServiceMgr) IsEnabled(string) bool             { return false }
func (f *fakeServiceMgr) UnitStatus(string) (string, error) { return "", nil }

func swapMgr(t *testing.T, fake services.ServiceManager) {
	t.Helper()
	prev := services.Mgr
	services.Mgr = fake
	t.Cleanup(func() { services.Mgr = prev })
}

func TestWriteUserServiceWithReload_reloadsWhenChanged(t *testing.T) {
	fake := &fakeServiceMgr{writeChanged: true}
	swapMgr(t, fake)

	if err := writeUserServiceWithReload("lerd-ui", "x"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{"write:lerd-ui", "reload"}
	if !equalStrings(fake.calls, want) {
		t.Fatalf("call order: got %v want %v", fake.calls, want)
	}
}

func TestWriteUserServiceWithReload_skipsReloadWhenUnchanged(t *testing.T) {
	fake := &fakeServiceMgr{writeChanged: false}
	swapMgr(t, fake)

	if err := writeUserServiceWithReload("lerd-ui", "x"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{"write:lerd-ui"}
	if !equalStrings(fake.calls, want) {
		t.Fatalf("call order: got %v want %v", fake.calls, want)
	}
}

func TestWriteUserServiceWithReload_returnsWriteError(t *testing.T) {
	wantErr := errors.New("boom")
	fake := &fakeServiceMgr{writeErr: wantErr}
	swapMgr(t, fake)

	if err := writeUserServiceWithReload("lerd-ui", "x"); !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
	if len(fake.calls) != 1 || fake.calls[0] != "write:lerd-ui" {
		t.Fatalf("expected only the write call, got %v", fake.calls)
	}
}

func TestWriteUserServiceWithReload_swallowsReloadError(t *testing.T) {
	fake := &fakeServiceMgr{writeChanged: true, reloadErr: errors.New("dbus down")}
	swapMgr(t, fake)

	if err := writeUserServiceWithReload("lerd-ui", "x"); err != nil {
		t.Fatalf("reload errors should not propagate, got %v", err)
	}
	want := []string{"write:lerd-ui", "reload"}
	if !equalStrings(fake.calls, want) {
		t.Fatalf("call order: got %v want %v", fake.calls, want)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
