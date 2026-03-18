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

.PHONY: build install install-installer test clean release release-snapshot

build:
	go build -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY) ./cmd/lerd

install: build
	install -Dm755 $(BUILD_DIR)/$(BINARY) $(INSTALL_DIR)/$(BINARY)
	@echo "Installed $(INSTALL_DIR)/$(BINARY)"
	@systemctl --user restart lerd-ui 2>/dev/null && echo "Restarted lerd-ui" || true
	@systemctl --user restart lerd-watcher 2>/dev/null && echo "Restarted lerd-watcher" || true

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
