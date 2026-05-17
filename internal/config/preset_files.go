package config

import (
	"encoding/json"
	"strconv"
	"strings"
)

// presetFiles holds the file mounts shipped with each bundled preset. This
// lives in Go rather than the preset YAMLs so that new lerd versions can
// update the mounted file contents automatically on the next service start
// without the user having to remove and reinstall the preset.
//
// Files are intentionally not a user feature: the three built-in presets
// below are the only ones that need runtime-generated config. A custom
// service author cannot declare their own file mounts.
var presetFiles = map[string][]FileMount{
	"mysql": {
		{
			Target: "/etc/mysql/conf.d/lerd.cnf",
			// loose- prefix skips directives unknown to a given mysql version;
			// authentication_policy is omitted because mysql 9.x removed
			// mysql_native_password, which made the variable refuse to load.
			Content: `[mysqld]
character-set-server=utf8mb4
collation-server=utf8mb4_unicode_ci
innodb_file_per_table=ON
innodb_strict_mode=OFF
loose-innodb_default_row_format=DYNAMIC
loose-mysql-native-password=ON
loose-restrict-fk-on-non-standard-key=OFF
`,
		},
	},
	"pgadmin": {
		{
			Target:    "/pgadmin4/servers.json",
			ContentFn: pgadminServersJSON,
		},
		{
			Target:    "/pgpass",
			Mode:      "0600",
			Chown:     true,
			ContentFn: pgadminPgpass,
		},
		{
			Target: "/pgadmin4/config_local.py",
			Content: `X_FRAME_OPTIONS = ''
ENHANCED_COOKIE_PROTECTION = False
WTF_CSRF_CHECK_DEFAULT = False

# Allow pgadmin's Flask session + CSRF cookies to flow inside a cross-origin
# iframe (the lerd-ui dashboard). SameSite=None requires Secure=True, which
# browsers accept over HTTP on localhost.
SESSION_COOKIE_SAMESITE = 'None'
SESSION_COOKIE_SECURE = True
`,
		},
	},
	"phpmyadmin": {
		{
			Target: "/etc/phpmyadmin/config.user.inc.php",
			Content: `<?php
// Allow phpmyadmin's session cookie to be sent when it's embedded in
// an iframe served from a different origin (the lerd-ui dashboard).
// The default SameSite=Strict drops the cookie on form POSTs, which
// breaks the server-switch dropdown via CSRF token mismatch.
// SameSite=None requires Secure=1, which phpmyadmin only sets when
// isHttps() is true, so we force the HTTPS env var — browsers treat
// localhost as secure so Secure cookies are accepted over HTTP.
$cfg['CookieSameSite'] = 'None';
$_SERVER['HTTPS'] = 'on';

// The official phpmyadmin image only handles PMA_USER/PMA_PASSWORD for
// single-host setups; in multi-host (PMA_HOSTS) it writes host/verbose
// per server but leaves user/password blank, forcing a login screen.
// Rebuild $cfg['Servers'] from our own parallel env arrays so every
// discovered mysql/mariadb service auto-logs in with config auth.
$hosts = array_values(array_filter(array_map('trim', explode(',', (string) getenv('PMA_HOSTS')))));
$users = array_map('trim', explode(',', (string) getenv('PMA_USERS')));
$passwords = array_map('trim', explode(',', (string) getenv('PMA_PASSWORDS')));
foreach ($hosts as $i => $host) {
    $idx = $i + 1;
    $cfg['Servers'][$idx] = [
        'host'      => $host,
        'verbose'   => $host,
        'auth_type' => 'config',
        'user'      => $users[$i] ?? 'root',
        'password'  => $passwords[$i] ?? 'lerd',
        'AllowNoPassword' => false,
    ];
}
$cfg['AllowThirdPartyFraming'] = true;
`,
		},
	},
}

// PresetFiles returns the hardcoded file mounts for the named preset, or nil
// when the preset has no files. The returned slice is a copy so callers
// cannot mutate the shared definition.
func PresetFiles(presetName string) []FileMount {
	src := presetFiles[presetName]
	if len(src) == 0 {
		return nil
	}
	out := make([]FileMount, len(src))
	copy(out, src)
	return out
}

// pgadminFriendlyName turns a container hostname like "lerd-postgres-18"
// into a human-friendly server label "Lerd Postgres 18".
func pgadminFriendlyName(host string) string {
	parts := strings.Split(strings.TrimPrefix(host, "lerd-"), "-")
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return "Lerd " + strings.Join(parts, " ")
}

// pgadminPostgresHosts returns the postgres family members, falling back to
// the canonical lerd-postgres when discovery is empty (fresh install before
// the family registry has been populated).
func pgadminPostgresHosts() []string {
	hosts := ServicesInFamily("postgres")
	if len(hosts) == 0 {
		return []string{"lerd-postgres"}
	}
	return hosts
}

// pgadminServersJSON renders pgAdmin's servers.json with every installed
// postgres family member, so alternates like postgres-18 appear in the
// dashboard alongside the canonical postgres without manual server setup.
func pgadminServersJSON(_ *CustomService) (string, error) {
	type server struct {
		Name          string `json:"Name"`
		Group         string `json:"Group"`
		Host          string `json:"Host"`
		Port          int    `json:"Port"`
		MaintenanceDB string `json:"MaintenanceDB"`
		Username      string `json:"Username"`
		SSLMode       string `json:"SSLMode"`
		PassFile      string `json:"PassFile"`
	}
	servers := map[string]server{}
	for i, host := range pgadminPostgresHosts() {
		servers[strconv.Itoa(i+1)] = server{
			Name:          pgadminFriendlyName(host),
			Group:         "Servers",
			Host:          host,
			Port:          5432,
			MaintenanceDB: "postgres",
			Username:      "postgres",
			SSLMode:       "prefer",
			PassFile:      "/pgpass",
		}
	}
	data, err := json.MarshalIndent(map[string]any{"Servers": servers}, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data) + "\n", nil
}

// pgadminPgpass renders a libpq passfile with one line per postgres family
// member so pgAdmin's PassFile=/pgpass entry auto-logs every alternate.
func pgadminPgpass(_ *CustomService) (string, error) {
	var b strings.Builder
	for _, host := range pgadminPostgresHosts() {
		b.WriteString(host)
		b.WriteString(":5432:*:postgres:lerd\n")
	}
	return b.String(), nil
}
