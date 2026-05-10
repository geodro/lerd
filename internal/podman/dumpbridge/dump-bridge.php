<?php
// /usr/local/etc/lerd/dump-bridge.php
//
// Auto-prepended PHP file installed by `lerd dump on`. Hooks Symfony's
// VarDumper so calls to dump()/dd() ship JSON payloads to lerd-ui's TCP
// receiver instead of (only) emitting to the response. See
// docs/features/dumps.md for the wire protocol.
//
// This file must never throw, never block, and never emit output.
//
// PHP requires all namespace declarations in a single file to use the
// same bracketed-or-not form, hence the explicit `namespace Lerd\DumpBridge`
// block for helpers and the trailing `namespace {}` block for the global
// dump() / dd() overrides.

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
        // Conservative default — the lerd installer rewrites the ini with the
        // real Unix socket path; this string only kicks in if the ini is
        // missing for some reason.
        return 'tcp://host.containers.internal:9913';
    }

    function send(array $payload): void
    {
        $target = target();
        // Allow both `unix:///path/to/sock` and `tcp://host:port`. The
        // installer defaults to the unix scheme so dumps stay confined to
        // the user's home directory.
        if (!str_contains($target, '://')) {
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

    function detect_site(): string
    {
        $env = getenv('LERD_SITE');
        if ($env !== false && $env !== '') {
            return $env;
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
        foreach ($bt as $f) {
            if (!isset($f['file'])) {
                continue;
            }
            $file = $f['file'];
            if (str_contains($file, 'symfony/var-dumper')) {
                continue;
            }
            if (str_ends_with($file, 'dump-bridge.php')) {
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
        function dump(mixed ...$vars): mixed
        {
            foreach ($vars as $label => $var) {
                \Lerd\DumpBridge\emit($var, is_string($label) ? $label : null);
                if (class_exists(\Symfony\Component\VarDumper\VarDumper::class)) {
                    \Symfony\Component\VarDumper\VarDumper::dump($var);
                }
            }
            return match (true) {
                count($vars) === 0 => null,
                count($vars) === 1 => $vars[array_key_first($vars)],
                default            => $vars,
            };
        }
    }
    if (!function_exists('dd')) {
        function dd(mixed ...$vars): never
        {
            dump(...$vars);
            exit(1);
        }
    }
}
