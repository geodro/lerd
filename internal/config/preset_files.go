package config

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
			Content: `[mysqld]
character-set-server=utf8mb4
collation-server=utf8mb4_unicode_ci
innodb_large_prefix=ON
innodb_file_format=Barracuda
innodb_file_per_table=ON
innodb_strict_mode=OFF
loose-innodb_default_row_format=DYNAMIC
`,
		},
	},
	"pgadmin": {
		{
			Target: "/pgadmin4/servers.json",
			Content: `{
  "Servers": {
    "1": {
      "Name": "Lerd Postgres",
      "Group": "Servers",
      "Host": "lerd-postgres",
      "Port": 5432,
      "MaintenanceDB": "postgres",
      "Username": "postgres",
      "SSLMode": "prefer",
      "PassFile": "/pgpass"
    }
  }
}
`,
		},
		{
			Target:  "/pgpass",
			Mode:    "0600",
			Chown:   true,
			Content: "lerd-postgres:5432:*:postgres:lerd\n",
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
