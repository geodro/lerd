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

## Other frameworks

The Selenium preset works with any browser testing framework that supports remote WebDriver, not just Laravel Dusk. For example:

- **Symfony Panther**: set `PANTHER_EXTERNAL_BASE_URI` and `PANTHER_CHROME_DRIVER_BINARY` or use the remote WebDriver directly
- **Pest with Dusk plugin**: same setup as Laravel Dusk above
- **PHPUnit + php-webdriver**: connect to `http://lerd-selenium:4444` with `RemoteWebDriver::create()`
