<?php
// /usr/local/etc/lerd/dump-bridge.php
//
// Always-mounted auto_prepend_file. The runtime sentinel
// `/usr/local/etc/lerd/enabled.flag` controls whether this file installs
// the dump()/dd() override or short-circuits and lets Symfony's stock
// helpers stay in charge. Flipping the bridge on or off is a single
// touch/rm of that file — no FPM restart, no worker cascade.
//
// This file must never throw, never block, and never emit output.

namespace {
    // Fast no-op when the toggle file is absent. One stat() per request
    // in the disabled case; nothing else loads, no functions defined,
    // no namespaces declared further down in this scope.
    if (!@file_exists('/usr/local/etc/lerd/enabled.flag')) {
        return;
    }
}

namespace Lerd\DumpBridge {
    if (defined(__NAMESPACE__.'\\LOADED')) {
        return;
    }
    const LOADED = 1;

    function target(): string
    {
        $h = getenv('LERD_DUMP_HOST');
        if ($h !== false && $h !== '') {
            return $h;
        }
        // get_cfg_var returns user-defined ini directives even when PHP's
        // ini_get refuses to (unregistered extension keys come back as
        // false from ini_get on most builds, but get_cfg_var reads the
        // raw php.ini parse).
        $cfg = get_cfg_var('lerd.dump_host');
        if (is_string($cfg) && $cfg !== '') {
            return $cfg;
        }
        // No configured target — send() returns early on the empty string
        // rather than attempt a stale-default connection.
        return '';
    }

    // passthrough_enabled reports whether the dashboard capture should ALSO
    // emit the dump to the response via Symfony's stock VarDumper handler.
    // Default false (capture-only) — same behaviour as Herd's dumps window;
    // override per-install with `dumps.passthrough: true` in config.yaml or
    // via the LERD_DUMP_PASSTHROUGH env var.
    function passthrough_enabled(): bool
    {
        $env = getenv('LERD_DUMP_PASSTHROUGH');
        if ($env !== false && $env !== '') {
            return $env === '1' || strcasecmp($env, 'true') === 0;
        }
        $cfg = get_cfg_var('lerd.dump_passthrough');
        return is_string($cfg) && ($cfg === '1' || strcasecmp($cfg, 'true') === 0);
    }

    function send(array $payload): void
    {
        $target = target();
        if ($target === '') {
            return;
        }
        // Allow both `unix:///path/to/sock` and `tcp://host:port`. The
        // installer defaults to the unix scheme so dumps stay confined to
        // the user's home directory.
        if (strpos($target, '://') === false) {
            $target = 'tcp://'.$target;
        }
        $sock = @\stream_socket_client($target, $errno, $errstr, 0.05, \STREAM_CLIENT_CONNECT);
        if (!$sock) {
            return;
        }
        @\stream_set_blocking($sock, false);
        $line = \json_encode($payload, \JSON_UNESCAPED_SLASHES | \JSON_PARTIAL_OUTPUT_ON_ERROR);
        if ($line === false) {
            @\fclose($sock);
            return;
        }
        @\fwrite($sock, $line."\n");
        @\fclose($sock);
    }

    // Read a tagging variable lerd may set either as a fastcgi_param (which
    // lands in $_SERVER) or as a real environment variable (CLI/tinker via
    // `podman exec --env`). $_SERVER first since FPM is the common case.
    function lerd_var(string $key): string
    {
        if (!empty($_SERVER[$key])) {
            return (string) $_SERVER[$key];
        }
        $env = getenv($key);
        return $env === false ? '' : $env;
    }

    function detect_site(): string
    {
        $v = lerd_var('LERD_SITE');
        if ($v !== '') {
            return $v;
        }
        if (\PHP_SAPI === 'cli') {
            $cwd = @getcwd();
            return $cwd ? basename($cwd) : '';
        }
        if (!empty($_SERVER['DOCUMENT_ROOT'])) {
            return basename(dirname($_SERVER['DOCUMENT_ROOT']));
        }
        return '';
    }

    function detect_branch(): string
    {
        return lerd_var('LERD_BRANCH');
    }

    function ulid(): string
    {
        try {
            return bin2hex(random_bytes(12));
        } catch (\Throwable $_) {
            return (string) (microtime(true) * 1000).'-'.mt_rand();
        }
    }

    function ts(): string
    {
        $now = microtime(true);
        $ms = (int) (($now - floor($now)) * 1000);
        return gmdate('Y-m-d\TH:i:s.', (int) $now).sprintf('%03dZ', $ms);
    }

    function source_frame(): array
    {
        $bt = debug_backtrace(\DEBUG_BACKTRACE_IGNORE_ARGS, 12);
        $self = 'dump-bridge.php';
        $selfLen = strlen($self);
        foreach ($bt as $f) {
            if (!isset($f['file'])) {
                continue;
            }
            $file = $f['file'];
            if (strpos($file, 'symfony/var-dumper') !== false) {
                continue;
            }
            if (substr($file, -$selfLen) === $self) {
                continue;
            }
            return ['file' => $file, 'line' => $f['line'] ?? 0];
        }
        return ['file' => '', 'line' => 0];
    }

    function context(): array
    {
        return [
            'type'    => \PHP_SAPI === 'cli' ? 'cli' : 'fpm',
            'site'    => detect_site(),
            'branch'  => detect_branch(),
            'domain'  => isset($_SERVER['HTTP_HOST']) ? (string) $_SERVER['HTTP_HOST'] : '',
            'request' => isset($_SERVER['REQUEST_METHOD'])
                ? $_SERVER['REQUEST_METHOD'].' '.($_SERVER['REQUEST_URI'] ?? '')
                : '',
            'pid'     => getmypid() ?: 0,
        ];
    }

    function emit($var, ?string $label = null): void
    {
        try {
            if (!class_exists(\Symfony\Component\VarDumper\Cloner\VarCloner::class, true)
                || !class_exists(\Symfony\Component\VarDumper\Dumper\CliDumper::class, true)) {
                send([
                    'v'     => 1,
                    'id'    => ulid(),
                    'ts'    => ts(),
                    'kind'  => 'dump',
                    'ctx'   => context(),
                    'src'   => source_frame(),
                    'label' => $label,
                    'text'  => is_scalar($var) ? (string) $var : print_r($var, true),
                ]);
                return;
            }
            $cloner = new \Symfony\Component\VarDumper\Cloner\VarCloner();
            $maxItems = (int) (getenv('LERD_DUMP_MAX_ITEMS') ?: 2500);
            $cloner->setMaxItems($maxItems > 0 ? $maxItems : 2500);
            $cloner->setMaxString(4096);
            $data = $cloner->cloneVar($var);
            $dumper = new \Symfony\Component\VarDumper\Dumper\CliDumper();
            $dumper->setColors(false);
            $text = $dumper->dump($data, true);
            send([
                'v'     => 1,
                'id'    => ulid(),
                'ts'    => ts(),
                'kind'  => 'dump',
                'ctx'   => context(),
                'src'   => source_frame(),
                'label' => $label,
                'text'  => is_string($text) ? $text : '',
            ]);
        } catch (\Throwable $_) {
            // never throw out of a dump bridge
        }
    }
}

namespace {
    // Define dump()/dd() in auto_prepend_file before composer's
    // var-dumper functions.php gets a chance to. Both Symfony's helpers
    // are gated on `if (!function_exists(...))`, so ours wins. We forward
    // through Symfony's VarDumper when it's available so existing display
    // pipelines (Whoops, Ignition) keep working in the response.
    if (!function_exists('dump')) {
        // No `mixed`/`never` type hints, `match`, or `array_key_first`: this
        // file is an auto_prepend_file for every PHP lerd builds, down to the
        // 7.2 legacy tier, and must parse and run on all of them.
        function dump(...$vars)
        {
            $passthrough = \Lerd\DumpBridge\passthrough_enabled();
            foreach ($vars as $label => $var) {
                \Lerd\DumpBridge\emit($var, is_string($label) ? $label : null);
                if ($passthrough && class_exists(\Symfony\Component\VarDumper\VarDumper::class)) {
                    \Symfony\Component\VarDumper\VarDumper::dump($var);
                }
            }
            if (count($vars) === 0) {
                return null;
            }
            if (count($vars) === 1) {
                return reset($vars);
            }
            return $vars;
        }
    }
    if (!function_exists('dd')) {
        function dd(...$vars)
        {
            dump(...$vars);
            exit(1);
        }
    }
}
