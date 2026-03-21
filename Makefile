BINARY      = lerd
VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "0.1.0")
COMMIT     ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE       ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
BUILD_DIR   = ./build
INSTALL_DIR = $(HOME)/.local/bin

PKG        = github.com/geodro/lerd/internal/version
LDFLAGS    = -s -w \
             -X $(PKG).Version=$(VERSION) \
             -X $(PKG).Commit=$(COMMIT) \
             -X $(PKG).Date=$(DATE)

.PHONY: build build-tray install install-installer test clean release release-snapshot

build:
	CGO_ENABLED=0 go build -tags nogui -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY) ./cmd/lerd

build-tray:
	CGO_ENABLED=1 go build -tags legacy_appindicator -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/lerd-tray ./cmd/lerd-tray

install: build build-tray
	install -Dm755 $(BUILD_DIR)/$(BINARY) $(INSTALL_DIR)/$(BINARY)
	install -Dm755 $(BUILD_DIR)/lerd-tray $(INSTALL_DIR)/lerd-tray
	@echo "Installed $(INSTALL_DIR)/$(BINARY) and $(INSTALL_DIR)/lerd-tray"
	@systemctl --user is-active --quiet lerd-ui 2>/dev/null && systemctl --user restart lerd-ui && echo "Restarted lerd-ui" || true
	@systemctl --user is-active --quiet lerd-watcher 2>/dev/null && systemctl --user restart lerd-watcher && echo "Restarted lerd-watcher" || true
	@systemctl --user is-active --quiet lerd-tray 2>/dev/null && systemctl --user restart lerd-tray || true

# Install the installer script as 'lerd-installer' so users can run
# lerd-installer --update  or  lerd-installer --uninstall
install-installer:
	install -Dm755 install.sh $(INSTALL_DIR)/lerd-installer
	@echo "Installed $(INSTALL_DIR)/lerd-installer"

test:
	go test ./...

test-installer:
	bats tests/installer/installer.bats

test-all: test test-installer

clean:
	rm -rf $(BUILD_DIR)

# Requires goreleaser: https://goreleaser.com/install/
release:
	goreleaser release --clean

release-snapshot:
	goreleaser release --snapshot --clean
