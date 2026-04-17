# Building from Source

## Prerequisites

The tray binary requires CGO and `libayatana-appindicator`. See [System Tray: Build requirements](../features/system-tray.md#build-requirements) for per-distro package names.

Go is required to build from source. The released binary has no runtime dependencies.

## Build commands

```bash
make build       # → ./build/lerd  (CGO, with tray support)
make build-nogui # → ./build/lerd-nogui  (no CGO, no tray)
make install     # build + install to ~/.local/bin/lerd
make test        # go test ./...
make clean       # remove ./build/
```

## Cross-compile for arm64

Without tray (no CGO required):

```bash
CGO_ENABLED=0 GOARCH=arm64 GOOS=linux go build -tags nogui -o ./build/lerd-arm64 ./cmd/lerd
```

## Installing a local build

To test a local build end-to-end using the installer:

```bash
make build
bash install.sh --local ./build/lerd
```

This runs the full installer flow (prerequisite checks, PATH setup, `lerd install`) using your locally built binary instead of downloading from GitHub.
