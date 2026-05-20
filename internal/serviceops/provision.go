package serviceops

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/podman"
)

// CreateDatabase creates dbName inside the named service container if it does
// not already exist. svc is the service name (e.g. "mysql", "mysql-5-6",
// "mariadb-11", "postgres-14"); the container is always "lerd-<svc>". The
// SQL client used is determined by the family inferred from svc.
// Returns (true, nil) if created, (false, nil) if it already existed,
// or (false, err) on failure.
func CreateDatabase(svc, name string) (bool, error) {
	container := "lerd-" + svc
	family := svc
	if inferred := config.FamilyOfName(svc); inferred != "" {
		family = inferred
	}
	switch family {
	case "mysql", "mariadb":
		binaries := []string{"mysql", "mariadb"}
		if family == "mariadb" {
			binaries = []string{"mariadb", "mysql"}
		}
		var lastErr error
		for _, bin := range binaries {
			check := podman.Cmd("exec", container, bin, "-uroot", "-plerd",
				"-sNe", fmt.Sprintf("SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name='%s';", name))
			out, err := check.Output()
			if err != nil {
				lastErr = err
				continue
			}
			if strings.TrimSpace(string(out)) != "0" {
				return false, nil
			}
			cmd := podman.Cmd("exec", container, bin, "-uroot", "-plerd",
				"-e", fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s`;", name))
			cmd.Stderr = os.Stderr
			return true, cmd.Run()
		}
		return false, lastErr
	case "postgres":
		cmd := podman.Cmd("exec", container, "psql", "-U", "postgres",
			"-c", fmt.Sprintf(`CREATE DATABASE "%s";`, name))
		out, err := cmd.CombinedOutput()
		if err != nil {
			if strings.Contains(string(out), "already exists") {
				return false, nil
			}
			return false, fmt.Errorf("%s", strings.TrimSpace(string(out)))
		}
		return true, nil
	default:
		return false, nil
	}
}

// S3BucketName converts a project handle into a valid S3 bucket name:
// lowercase, hyphens instead of underscores, leading/trailing non-alphanumerics
// stripped, max length 63.
func S3BucketName(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.':
			b.WriteRune(r)
		case r == '_', r == '-', r == ' ':
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-.")
	if len(out) > 63 {
		out = out[:63]
	}
	if out == "" {
		out = "lerd"
	}
	return out
}

// EnsureS3Bucket creates a bucket for the given name in lerd-rustfs using an
// ephemeral mc container. Returns (true, nil) if created, (false, nil) if it
// already existed, or (false, err) on failure. Retries up to 3 times (2s apart)
// to bridge the window between the host TCP port becoming reachable and the
// container network being fully ready for mc operations.
func EnsureS3Bucket(name string) (bool, error) {
	const (
		alias   = "lerd"
		mcImage = "docker.io/minio/mc:latest"
		mcEnv   = "MC_HOST_lerd=http://lerd:lerdpassword@lerd-rustfs:9000"
	)

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(2 * time.Second)
		}

		lsCmd := podman.Cmd("run", "--rm", "--network", "lerd",
			"-e", mcEnv, mcImage, "ls", alias+"/"+name)
		if lsCmd.Run() == nil {
			return false, nil
		}

		mbCmd := podman.Cmd("run", "--rm", "--network", "lerd",
			"-e", mcEnv, mcImage, "mb", alias+"/"+name)
		out, err := mbCmd.CombinedOutput()
		if err != nil {
			lastErr = fmt.Errorf("%s", strings.TrimSpace(string(out)))
			continue
		}

		pubCmd := podman.Cmd("run", "--rm", "--network", "lerd",
			"-e", mcEnv, mcImage, "anonymous", "set", "public", alias+"/"+name)
		if out, err := pubCmd.CombinedOutput(); err != nil {
			return false, fmt.Errorf("mc anonymous set public: %s", strings.TrimSpace(string(out)))
		}
		return true, nil
	}
	return false, lastErr
}
