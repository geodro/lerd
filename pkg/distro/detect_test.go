package distro

import (
	"os"
	"path/filepath"
	"testing"
)

func writeOsRelease(t *testing.T, content string) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "os-release")
	if err := os.WriteFile(f, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return f
}

// ── detectFromPath ────────────────────────────────────────────────────────────

func TestDetect_Debian(t *testing.T) {
	f := writeOsRelease(t, `ID=ubuntu
ID_LIKE=debian
PRETTY_NAME="Ubuntu 24.04 LTS"
`)
	d, err := detectFromPath(f)
	if err != nil {
		t.Fatalf("detectFromPath: %v", err)
	}
	if !d.IsDebian() {
		t.Errorf("expected IsDebian() for ubuntu, got ID=%q IDLike=%q", d.ID, d.IDLike)
	}
	if d.IsArch() {
		t.Error("ubuntu should not be Arch")
	}
	if d.IsFedora() {
		t.Error("ubuntu should not be Fedora")
	}
}

func TestDetect_DebianByIDLike(t *testing.T) {
	f := writeOsRelease(t, `ID=linuxmint
ID_LIKE=ubuntu debian
PRETTY_NAME="Linux Mint 22"
`)
	d, err := detectFromPath(f)
	if err != nil {
		t.Fatal(err)
	}
	if !d.IsDebian() {
		t.Errorf("Linux Mint should be Debian-like, got ID=%q IDLike=%q", d.ID, d.IDLike)
	}
}

func TestDetect_Fedora(t *testing.T) {
	f := writeOsRelease(t, `ID=fedora
PRETTY_NAME="Fedora Linux 40"
`)
	d, err := detectFromPath(f)
	if err != nil {
		t.Fatal(err)
	}
	if !d.IsFedora() {
		t.Errorf("expected IsFedora() for ID=fedora")
	}
	if d.IsDebian() || d.IsArch() {
		t.Error("fedora should not be Debian or Arch")
	}
}

func TestDetect_FedoraByIDLike(t *testing.T) {
	f := writeOsRelease(t, `ID=centos
ID_LIKE="rhel fedora"
PRETTY_NAME="CentOS Stream 9"
`)
	d, err := detectFromPath(f)
	if err != nil {
		t.Fatal(err)
	}
	if !d.IsFedora() {
		t.Errorf("CentOS should be Fedora-like")
	}
}

func TestDetect_Arch(t *testing.T) {
	f := writeOsRelease(t, `ID=arch
PRETTY_NAME="Arch Linux"
`)
	d, err := detectFromPath(f)
	if err != nil {
		t.Fatal(err)
	}
	if !d.IsArch() {
		t.Errorf("expected IsArch() for ID=arch")
	}
	if d.IsDebian() || d.IsFedora() {
		t.Error("arch should not be Debian or Fedora")
	}
}

func TestDetect_ArchByIDLike(t *testing.T) {
	f := writeOsRelease(t, `ID=endeavouros
ID_LIKE=arch
PRETTY_NAME="EndeavourOS"
`)
	d, err := detectFromPath(f)
	if err != nil {
		t.Fatal(err)
	}
	if !d.IsArch() {
		t.Errorf("EndeavourOS should be Arch-like")
	}
}

func TestDetect_Unknown(t *testing.T) {
	f := writeOsRelease(t, `ID=slackware
PRETTY_NAME="Slackware 15.0"
`)
	d, err := detectFromPath(f)
	if err != nil {
		t.Fatalf("detectFromPath: %v", err)
	}
	if d == nil {
		t.Fatal("expected non-nil Distro for unknown distro")
	}
	if d.IsArch() || d.IsDebian() || d.IsFedora() {
		t.Error("slackware should not match any known family")
	}
	if d.PrettyName != "Slackware 15.0" {
		t.Errorf("PrettyName = %q, want %q", d.PrettyName, "Slackware 15.0")
	}
}

func TestDetect_QuotedValues(t *testing.T) {
	f := writeOsRelease(t, `ID="ubuntu"
PRETTY_NAME="Ubuntu 22.04 LTS"
`)
	d, err := detectFromPath(f)
	if err != nil {
		t.Fatal(err)
	}
	if d.ID != "ubuntu" {
		t.Errorf("expected quotes stripped, ID = %q", d.ID)
	}
}

func TestDetect_CommentsSkipped(t *testing.T) {
	f := writeOsRelease(t, `# This is a comment
ID=arch
# Another comment
PRETTY_NAME="Arch Linux"
`)
	d, err := detectFromPath(f)
	if err != nil {
		t.Fatal(err)
	}
	if d.ID != "arch" {
		t.Errorf("ID = %q, expected arch (comments should be skipped)", d.ID)
	}
}

func TestDetect_MissingFile(t *testing.T) {
	_, err := detectFromPath("/nonexistent/os-release")
	if err == nil {
		t.Error("expected error for missing os-release file")
	}
}

// ── IsDebian edge cases ───────────────────────────────────────────────────────

func TestIsDebian_ByID(t *testing.T) {
	cases := []struct {
		id   string
		want bool
	}{
		{"debian", true},
		{"ubuntu", true},
		{"linuxmint", false},
		{"arch", false},
	}
	for _, c := range cases {
		d := &Distro{ID: c.id}
		if got := d.IsDebian(); got != c.want {
			t.Errorf("IsDebian() for ID=%q = %v, want %v", c.id, got, c.want)
		}
	}
}
