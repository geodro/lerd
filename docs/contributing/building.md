# Building from Source

## Prerequisites

The tray binary requires CGO and `libayatana-appindicator`. See [System Tray: Build requirements](../features/system-tray.md#build-requirements) for per-distro package names.

Go is required to build from source. The released binary has no runtime dependencies.

**Web UI**: the `lerd-ui` dashboard is built from Svelte sources under `internal/ui/web/` and bundled into the Go binary via `//go:embed`. Node.js (20+) and npm are required to rebuild it. `make build` runs `npm install` (once) and `npm run build` automatically before the Go build, so a single `make` command still produces a self-contained binary. If you only change Go code, you can skip the JS build by running `go build` directly against a previously-built `internal/ui/web/dist/` tree.

## Build commands

```bash
make build       # → ./build/lerd  (CGO, with tray support; builds UI first)
make build-nogui # → ./build/lerd-nogui  (no CGO, no tray)
make build-ui    # rebuild only the web UI (internal/ui/web/dist/)
make install     # build + install to ~/.local/bin/lerd
make test        # go test ./...
make test-ui     # run Vitest suite for the web UI
make test-all    # test + test-ui + test-installer (bats)
make clean       # remove ./build/ and internal/ui/web/dist/
```

## Cross-compile for arm64

Without tray (no CGO required):

```bash
CGO_ENABLED=0 GOARCH=arm64 GOOS=linux go build -tags nogui -o ./build/lerd-arm64 ./cmd/lerd
```

The UI only needs to be built once per source-tree state; the emitted `dist/` is architecture-independent.

## Developing the web UI

```bash
cd internal/ui/web
npm install            # once
npm run dev            # Vite dev server at http://localhost:5173 (proxies /api/* to 7073)
npm run check          # svelte-check + tsc
npm test               # Vitest
```

The Vite dev server proxies `/api/*`, `/icons/*`, `/manifest.webmanifest`, `/sw.js`, and `/offline.html` to a running `lerd-ui` on `:7073`, so you get hot-reload on the Svelte side while the Go backend handles the data. Run `lerd start` (or `make install` once) first so the backend is up.

## Installing a local build

To test a local build end-to-end using the installer:

```bash
make build
bash install.sh --local ./build/lerd
```

This runs the full installer flow (prerequisite checks, PATH setup, `lerd install`) using your locally built binary instead of downloading from GitHub.
