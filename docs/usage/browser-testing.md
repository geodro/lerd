# Browser Testing

Lerd ships a **Selenium** service preset that runs Chromium inside a container on the same Podman network as nginx and PHP-FPM. This lets browser testing frameworks like [Laravel Dusk](https://laravel.com/docs/dusk) drive a real browser against your `.test` sites without installing Chrome or ChromeDriver on your host machine.

---

## Quick start (Laravel Dusk)

### 1. Install the Selenium preset

```bash
lerd service preset selenium
lerd service start selenium
```

This starts a `selenium/standalone-chromium` container with:
- **WebDriver** on port 4444
- **noVNC dashboard** on port 7900: open `http://localhost:7900` to watch tests run in the browser

The container automatically resolves `.test` domains to the nginx container so Chromium can load your sites over HTTP and HTTPS.

### 2. Install Dusk

```bash
lerd composer require --dev laravel/dusk
lerd artisan dusk:install
```

### 3. Run `lerd env`

```bash
lerd env
```

When `lerd env` detects `laravel/dusk` in `composer.json` and the Selenium preset is installed, it automatically:

- Adds `DUSK_DRIVER_URL=http://lerd-selenium:4444` to `.env`
- Patches `tests/DuskTestCase.php` to skip starting a local ChromeDriver when `DUSK_DRIVER_URL` is set
- Adds `--ignore-certificate-errors` to Chrome options so Chromium accepts lerd's mkcert certificates

These changes are compatible with Sail and other environments; when `DUSK_DRIVER_URL` is not set, the default local ChromeDriver behaviour kicks in as usual.

### 4. Run tests

```bash
lerd artisan dusk
lerd artisan dusk --filter=homepage
```

---

## Watching tests

Open the noVNC dashboard at `http://localhost:7900` to see the Chromium browser in real time. This is useful for debugging failing tests or understanding what the browser sees.

---

## How it works

The Selenium container joins the `lerd` Podman network and mounts a hosts file that maps all `.test` domains to the nginx container's internal IP. When Dusk tells Chromium to visit `https://myapp.test`, the browser resolves the domain inside the container, connects to nginx over the Podman network, and nginx proxies to PHP-FPM as usual.

```
Dusk (PHP-FPM) → WebDriver API → Selenium container → Chromium
                                                        ↓
                                            https://myapp.test
                                                        ↓
                                                lerd-nginx → PHP-FPM
```

---

## Managing the service

The Selenium preset is a regular lerd custom service:

```bash
lerd service start selenium
lerd service stop selenium
lerd service restart selenium
lerd service remove selenium    # uninstall
```

---

## Pest Browser Testing (Playwright)

Pest v4's native [browser testing](https://pestphp.com/docs/browser-testing) (`pestphp/pest-plugin-browser`) does **not** use Selenium or WebDriver. It drives [Playwright](https://playwright.dev) locally, in the same place the test process runs, and Playwright always launches its own browser there, so the Selenium preset above does not apply.

In lerd your tests run inside the PHP-FPM container, so the browser has to live there too. Two facts make that tricky: Playwright's own Chromium is a glibc binary that cannot run on the container's musl libc, and Pest exposes no hook to point Playwright at a remote browser. lerd solves both by baking Alpine's musl-native Chromium into the FPM image and transparently shimming Playwright's browser to it.

### 1. Install the Pest browser plugin

```bash
lerd composer require --dev pestphp/pest-plugin-browser
lerd npm install playwright
```

### 2. Run the lerd setup

```bash
lerd pest:browser install
```

This adds `chromium` to that PHP version's shared FPM image (the same package mechanism as `lerd php:pkg`, so the container is not split off onto its own image), rebuilds it, downloads the Playwright browser registry into a persistent volume, and shims Playwright's glibc browser to the musl Chromium with `--no-sandbox`. It is safe to re-run, and you should re-run it after bumping the Playwright version in your project.

Check the setup at any time with `lerd pest:browser doctor`, and tear it back down with `lerd pest:browser remove` (un-bakes chromium and rebuilds; the cache volume is left intact):

```bash
lerd pest:browser doctor
lerd pest:browser remove
```

### 3. Run tests

Browser tests run through your normal test command inside the FPM container:

```bash
lerd test
lerd pest
```

Pest serves your app itself and drives the in-container Chromium headlessly. Because the browser is the system musl build, only **Chromium** is supported (Alpine does not ship musl Firefox or WebKit builds). This requires a current PHP version; the legacy 7.4/8.0 tier ships an older Node and is not supported for browser testing.

---

## Other frameworks

The Selenium preset works with any browser testing framework that supports remote WebDriver:

- **Symfony Panther**: set `PANTHER_EXTERNAL_BASE_URI` and `PANTHER_CHROME_DRIVER_BINARY` or use the remote WebDriver directly
- **Pest with the Dusk plugin**: the Dusk-based Pest plugin uses Selenium, so the Dusk setup above applies (this is different from Pest's native Playwright browser testing covered earlier)
- **PHPUnit + php-webdriver**: connect to `http://lerd-selenium:4444` with `RemoteWebDriver::create()`
