#!/usr/bin/env bash
# Lerd installer — https://github.com/geodro/lerd
# Usage:
#   Install:   curl -fsSL https://raw.githubusercontent.com/geodro/lerd/main/install.sh | bash
#      or:     wget -qO- https://raw.githubusercontent.com/geodro/lerd/main/install.sh | bash
#   Update:    lerd-installer --update
#   Uninstall: lerd-installer --uninstall

set -euo pipefail

# ── Constants ────────────────────────────────────────────────────────────────
REPO="geodro/lerd"
BINARY="lerd"
INSTALL_DIR="${LERD_INSTALL_DIR:-$HOME/.local/bin}"
LERD_CONFIG_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/lerd"
LERD_DATA_DIR="${XDG_DATA_HOME:-$HOME/.local/share}/lerd"

# ── Colors ───────────────────────────────────────────────────────────────────
if [ -t 1 ]; then
  RED='\033[0;31m'; YELLOW='\033[1;33m'; GREEN='\033[0;32m'
  CYAN='\033[0;36m'; BOLD='\033[1m'; RESET='\033[0m'
else
  RED=''; YELLOW=''; GREEN=''; CYAN=''; BOLD=''; RESET=''
fi

# ── Helpers ──────────────────────────────────────────────────────────────────
info()    { echo -e "  ${CYAN}-->${RESET} $*"; }
success() { echo -e "  ${GREEN}✓${RESET}  $*"; }
warn()    { echo -e "  ${YELLOW}!${RESET}  $*"; }
error()   { echo -e "  ${RED}✗${RESET}  $*" >&2; }
die()     { error "$*"; exit 1; }
header()  { echo -e "\n${BOLD}$*${RESET}"; }
ask()     { echo -en "  ${BOLD}?${RESET}  $* [y/N] "; read -r _ans </dev/tty 2>/dev/null || true; [[ "$_ans" =~ ^[Yy]$ ]]; }
star_note() {
  echo ""
  echo -e "  ${CYAN}★${RESET}  If lerd is useful to you, a GitHub star helps others find it:"
  echo -e "     https://github.com/${REPO}"
}

# ── Platform detection ───────────────────────────────────────────────────────
detect_os() {
  case "$(uname -s)" in
    Linux)  echo "linux" ;;
    Darwin) echo "darwin" ;;
    *) die "Unsupported OS: $(uname -s). Lerd supports Linux and macOS." ;;
  esac
}

detect_arch() {
  # macOS reports arm64; Linux reports aarch64: both map to the arm64 release.
  case "$(uname -m)" in
    x86_64)        echo "amd64" ;;
    aarch64|arm64) echo "arm64" ;;
    *) die "Unsupported architecture: $(uname -m)" ;;
  esac
}

detect_distro() {
  if [ -f /etc/os-release ]; then
    # shellcheck disable=SC1091
    . /etc/os-release
    echo "${ID:-unknown}"
  else
    echo "unknown"
  fi
}

distro_family() {
  local distro; distro="$(detect_distro)"
  case "$distro" in
    arch|manjaro|endeavouros|garuda) echo "arch" ;;
    debian|ubuntu|pop|linuxmint|elementary|zorin) echo "debian" ;;
    fedora|rhel|centos|rocky|alma) echo "fedora" ;;
    opensuse*|sles) echo "suse" ;;
    *) echo "unknown" ;;
  esac
}

# ── Prerequisite checks ──────────────────────────────────────────────────────
MISSING_PKGS=()

# DNS mode chosen by the user before prerequisites are checked. "managed" runs
# lerd's dnsmasq + mkcert so sites resolve at https://<name>.test with trusted
# certs; "localhost" skips all of that and serves http://<name>.localhost over
# plain HTTP. The choice is passed straight to `lerd install --dns` so the
# prompt isn't shown twice, and it lets us skip the HTTPS-only packages
# (certutil / nss-tools) when the user only wants .localhost.
DNS_MODE="managed"

ask_dns_mode() {
  local _ans=""
  header "DNS mode"
  echo "  lerd can manage local DNS so sites resolve at https://<name>.test with"
  echo "  trusted certificates, or stay out of the way and serve them at"
  echo "  http://<name>.localhost over plain HTTP. The .localhost mode needs no"
  echo "  dnsmasq, no mkcert, and no extra packages."
  echo ""
  echo -en "  ${BOLD}?${RESET}  Let lerd manage DNS for .test sites with HTTPS? [Y/n] "
  read -r _ans </dev/tty 2>/dev/null || true
  if [[ "$_ans" =~ ^[Nn]$ ]]; then
    DNS_MODE="localhost"
    info "Using *.localhost over HTTP, no certificates or extra packages required"
  else
    DNS_MODE="managed"
  fi
}

check_cmd() {
  local cmd="$1" pkg="${2:-$1}" desc="${3:-}"
  if command -v "$cmd" &>/dev/null; then
    success "$cmd found ($(command -v "$cmd"))"
  else
    warn "$cmd not found${desc:+ — $desc}"
    MISSING_PKGS+=("$pkg")
  fi
}

check_systemd_user() {
  if systemctl --user status &>/dev/null 2>&1; then
    success "systemd user session active"
  else
    warn "systemd user session not active"
    warn "Run: loginctl enable-linger \$USER"
    warn "Then log out and back in"
    MISSING_PKGS+=("_systemd_linger")
  fi
}

check_dns_resolver() {
  if systemctl is-active --quiet NetworkManager 2>/dev/null; then
    success "NetworkManager running"
  elif systemctl is-active --quiet systemd-resolved 2>/dev/null; then
    success "systemd-resolved running"
  else
    warn "No supported DNS resolver running (need NetworkManager or systemd-resolved)"
    MISSING_PKGS+=("networkmanager")
  fi
}

check_certutil() {
  if command -v certutil &>/dev/null; then
    success "certutil found (needed for mkcert CA trust in browsers)"
    return
  fi
  local family; family="$(distro_family)"
  local pkg
  case "$family" in
    arch)   pkg="nss" ;;
    debian) pkg="libnss3-tools" ;;
    fedora) pkg="nss-tools" ;;
    suse)   pkg="mozilla-nss-tools" ;;
    *)      pkg="nss-tools" ;;
  esac
  warn "certutil not found — mkcert won't be able to trust HTTPS certs in Chrome/Firefox"
  MISSING_PKGS+=("$pkg")
}

check_podman_rootless() {
  if ! command -v podman &>/dev/null; then
    return  # already flagged by check_cmd
  fi
  if podman info --format '{{.Host.Security.Rootless}}' 2>/dev/null | grep -q true; then
    success "Podman running rootless"
  else
    warn "Podman may not be configured for rootless — check 'podman info'"
  fi
}

check_prerequisites() {
  if [ "$(detect_os)" = "darwin" ]; then
    check_prerequisites_macos
  else
    check_prerequisites_linux
  fi
}

# macOS needs only the podman CLI on PATH; `lerd install` brings the Podman
# machine up itself, and mkcert/DNS/launchd are all handled inside the binary.
# Missing packages are installed via Homebrew (no sudo, unlike the Linux path).
check_prerequisites_macos() {
  header "Checking prerequisites"

  check_cmd podman podman "container runtime"

  if [ ${#MISSING_PKGS[@]} -eq 0 ]; then
    success "All prerequisites met"
    return
  fi

  echo ""
  warn "Missing: ${MISSING_PKGS[*]}"
  if ask "Install missing packages now (via Homebrew)?"; then
    install_packages "${MISSING_PKGS[@]}"
  fi
}

check_prerequisites_linux() {
  header "Checking prerequisites"

  check_cmd podman podman "container runtime"
  check_cmd unzip unzip "needed to extract fnm"
  check_dns_resolver
  check_systemd_user
  check_podman_rootless
  if [ "$DNS_MODE" = "managed" ]; then
    check_certutil
  fi

  if [ ${#MISSING_PKGS[@]} -eq 0 ]; then
    success "All prerequisites met"
    return
  fi

  echo ""
  warn "Missing: ${MISSING_PKGS[*]}"

  local installable=()
  for p in "${MISSING_PKGS[@]}"; do
    [[ "$p" != _* ]] && installable+=("$p")
  done

  if [ ${#installable[@]} -gt 0 ] && ask "Install missing packages now?"; then
    install_packages "${installable[@]}"
  fi

  # Handle special cases
  for p in "${MISSING_PKGS[@]}"; do
    case "$p" in
      _systemd_linger)
        if ask "Enable systemd linger for $USER now?"; then
          loginctl enable-linger "$USER"
          success "Linger enabled — please log out and back in before running 'lerd install'"
        fi
        ;;
    esac
  done
}

install_packages() {
  local pkgs=("$@")

  header "Installing: ${pkgs[*]}"

  if [ "$(detect_os)" = "darwin" ]; then
    if ! command -v brew &>/dev/null; then
      die "Homebrew not found. Install it from https://brew.sh then re-run, or install manually: ${pkgs[*]}"
    fi
    brew install "${pkgs[@]}"
    success "Packages installed"
    return
  fi

  local family; family="$(distro_family)"

  case "$family" in
    arch)
      sudo pacman -S --needed --noconfirm "${pkgs[@]}"
      ;;
    debian)
      sudo apt-get update -q
      sudo apt-get install -y "${pkgs[@]}"
      ;;
    fedora)
      sudo dnf install -y "${pkgs[@]}"
      ;;
    suse)
      sudo zypper install -y "${pkgs[@]}"
      ;;
    *)
      die "Don't know how to install packages on this distro. Install manually: ${pkgs[*]}"
      ;;
  esac

  success "Packages installed"

  # Initialize podman storage for the current user after first install.
  # This runs any pending migrations and sets up ~/.local/share/containers.
  if command -v podman &>/dev/null; then
    podman system migrate &>/dev/null || true
  fi
}

# ── Download tool ────────────────────────────────────────────────────────────
# Prefer curl; fall back to wget. Errors out if neither is available.
_download_tool() {
  if command -v curl &>/dev/null; then
    echo "curl"
  elif command -v wget &>/dev/null; then
    echo "wget"
  else
    die "Neither curl nor wget found. Install one and retry."
  fi
}

# fetch <url> <dest>  — download URL to dest file
fetch() {
  local url="$1" dest="$2"
  case "$(_download_tool)" in
    curl) curl -fsSL --progress-bar "$url" -o "$dest" ;;
    wget) wget -q --show-progress "$url" -O "$dest" ;;
  esac
}

# fetch_stdout <url>  — download URL to stdout (for piping into grep/sed)
fetch_stdout() {
  local url="$1"
  case "$(_download_tool)" in
    curl) curl -fsSL "$url" ;;
    wget) wget -qO- "$url" ;;
  esac
}

# ── GitHub release helpers ───────────────────────────────────────────────────
latest_version() {
  # Use the HTML releases/latest redirect — no API key, not rate-limited.
  # GitHub redirects to the canonical release URL whose path contains the tag.
  local url="https://github.com/${REPO}/releases/latest"
  local location
  case "$(_download_tool)" in
    curl) location="$(curl -fsSLI --stderr /dev/null \
            -H "User-Agent: lerd-installer" \
            "$url" | grep -i '^location:' | tail -1)" ;;
    wget) location="$(wget -qS --spider \
            --header "User-Agent: lerd-installer" \
            "$url" 2>&1 | grep -i 'Location:'  | tail -1)" ;;
  esac

  # location header value looks like: .../releases/tag/v0.1.33
  echo "$location" | sed -E 's|.*/releases/tag/v?([^[:space:]]+).*|\1|' | tr -d '\r'
}

# download_binary <version> <arch> <destdir>
# Downloads and extracts the release archive into <destdir>.
# The extracted binary will be at <destdir>/lerd.
# All output goes to stderr — nothing is printed to stdout.
download_binary() {
  local version="$1" arch="$2" destdir="$3"
  local os; os="$(detect_os)"
  local filename="lerd_${version}_${os}_${arch}.tar.gz"
  local url="https://github.com/${REPO}/releases/download/v${version}/${filename}"

  info "Downloading lerd v${version} (${arch}) via $(_download_tool) ..."
  if ! fetch "$url" "${destdir}/${filename}"; then
    die "Download failed (HTTP 404).\nNo release v${version} found at:\n  ${url}\n\nIf you built lerd locally, use:\n  bash install.sh --local ./build/lerd"
  fi

  if ! tar -xzf "${destdir}/${filename}" -C "$destdir" 2>&1; then
    die "Failed to extract archive: ${filename}"
  fi
}

installed_version() {
  if command -v lerd &>/dev/null; then
    lerd --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1 || echo "unknown"
  else
    echo ""
  fi
}

# Full version token including any git-describe suffix (e.g.
# 1.25.0-6-g7d030096-dirty). installed_version() collapses that to the bare
# 1.25.0, which is what version_is_dev needs the suffix from.
installed_version_raw() {
  if command -v lerd &>/dev/null; then
    lerd --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+[A-Za-z0-9.-]*' | head -1 || echo ""
  else
    echo ""
  fi
}

# True when the version token carries a git-describe suffix. Release binaries
# report a clean X.Y.Z, so a suffix means an ahead-of-release local build that
# the installer must not silently overwrite with the matching base release.
version_is_dev() {
  [[ "$1" =~ ^[0-9]+\.[0-9]+\.[0-9]+- ]]
}

# Guards an overwrite of a local development build. Prints a warning and asks
# before replacing it; keeps the build and exits 0 when declined or when no tty
# is available to confirm. $1 is the release version about to be installed.
guard_dev_build() {
  local target="$1" current_raw; current_raw="$(installed_version_raw)"
  version_is_dev "$current_raw" || return 0
  warn "A local development build (v${current_raw}) is installed."
  if [ -r /dev/tty ] && ask "Replace it with release v${target}?"; then
    return 0
  fi
  info "Keeping your local build. Reinstall a dev build with: install.sh --local <path>"
  exit 0
}

# ── Shell integration ────────────────────────────────────────────────────────
SHELL_MARKER="# Added by Lerd installer"

detect_shell_rc() {
  local shell; shell="$(basename "${SHELL:-bash}")"
  case "$shell" in
    fish) echo "$HOME/.config/fish/conf.d/lerd.fish" ;;
    zsh)  echo "$HOME/.zshrc" ;;
    *)
      # macOS Terminal launches bash as a login shell, which reads
      # .bash_profile, not .bashrc; Linux interactive bash reads .bashrc.
      if [ "$(detect_os)" = "darwin" ]; then
        echo "$HOME/.bash_profile"
      else
        echo "$HOME/.bashrc"
      fi
      ;;
  esac
}

add_to_path() {
  local shell; shell="$(basename "${SHELL:-bash}")"
  local rc; rc="$(detect_shell_rc)"

  # Check if already in current PATH
  if [[ ":$PATH:" == *":$INSTALL_DIR:"* ]]; then
    success "$INSTALL_DIR is already in PATH"
    return
  fi

  # Don't add if already present in rc file
  if grep -q "$SHELL_MARKER" "$rc" 2>/dev/null; then
    success "PATH already configured in $rc"
    return
  fi

  case "$shell" in
    fish)
      mkdir -p "$(dirname "$rc")"
      printf '\n%s\nfish_add_path %s\n' "$SHELL_MARKER" "$INSTALL_DIR" >> "$rc"
      ;;
    *)
      printf '\n%s\nexport PATH="%s:$PATH"\n' "$SHELL_MARKER" "$INSTALL_DIR" >> "$rc"
      ;;
  esac
  success "Added $INSTALL_DIR to PATH in $rc"
  warn "Reload your shell or run: source $rc"
}

remove_from_path() {
  local rc; rc="$(detect_shell_rc)"
  if [ ! -f "$rc" ]; then return; fi

  # Remove the block: marker line + the next line
  if grep -q "$SHELL_MARKER" "$rc" 2>/dev/null; then
    # portable: works on both GNU and BSD sed
    sed -i.bak "/^${SHELL_MARKER}/,+1d" "$rc" && rm -f "${rc}.bak"
    # also remove blank line before marker if present
    info "Removed PATH entry from $rc"
  fi
}

# ── Install ──────────────────────────────────────────────────────────────────
cmd_install() {
  local local_binary="${1:-}"
  header "Installing Lerd"

  # Validate local binary path before running any checks so the error is clear.
  if [ -n "$local_binary" ]; then
    [ -f "$local_binary" ] || die "File not found: $local_binary"
  fi

  ask_dns_mode
  check_prerequisites

  if ! command -v podman &>/dev/null; then
    die "podman is required but not installed. Install it and re-run this script."
  fi

  mkdir -p "$INSTALL_DIR"

  if [ -n "$local_binary" ]; then
    # ── Local binary path supplied (e.g. ./build/lerd) ──
    [ -f "$local_binary" ] || die "File not found: $local_binary"
    install -m 755 "$local_binary" "${INSTALL_DIR}/${BINARY}"
    local version; version="$("${INSTALL_DIR}/${BINARY}" --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1 || echo "dev")"
    success "Installed lerd ${version} (local) → ${INSTALL_DIR}/${BINARY}"
  else
    # ── Download from GitHub releases ──
    local arch; arch="$(detect_arch)"
    local version; version="$(latest_version)"
    if [ -z "$version" ]; then
      die "No releases found at https://github.com/${REPO}/releases\n\nIf you built lerd locally, install with:\n  bash install.sh --local ./build/lerd"
    fi

    local current; current="$(installed_version)"
    if [ -n "$current" ] && [ "$current" = "$version" ]; then
      success "Lerd v${version} is already installed and up to date"
      exit 0
    fi
    guard_dev_build "$version"

    local tmpdir; tmpdir="$(mktemp -d)"
    download_binary "$version" "$arch" "$tmpdir"
    install -m 755 "${tmpdir}/lerd" "${INSTALL_DIR}/${BINARY}"
    [ -f "${tmpdir}/lerd-tray" ] && install -m 755 "${tmpdir}/lerd-tray" "${INSTALL_DIR}/lerd-tray"
    rm -rf "$tmpdir"
    success "Installed lerd v${version} → ${INSTALL_DIR}/${BINARY}"
  fi

  add_to_path

  echo ""
  info "Running 'lerd install' to complete setup ..."
  echo ""
  # When this script is piped through `curl|bash`, our own stdin is the pipe
  # and lerd's prompts would silently hit EOF. Hand it /dev/tty when one is
  # available so [Y/n] questions reach the user.
  if [ -r /dev/tty ]; then
    "${INSTALL_DIR}/${BINARY}" install --dns "$DNS_MODE" </dev/tty
  else
    "${INSTALL_DIR}/${BINARY}" install --dns "$DNS_MODE"
  fi
  star_note
}

# ── Update ───────────────────────────────────────────────────────────────────
cmd_update() {
  header "Updating Lerd"

  local arch; arch="$(detect_arch)"
  local latest; latest="$(latest_version)"
  [ -n "$latest" ] || die "Could not fetch latest version."

  local current; current="$(installed_version)"

  if [ "$current" = "$latest" ]; then
    success "Already on latest: v${latest}"
    exit 0
  fi
  guard_dev_build "$latest"

  info "Updating v${current:-unknown} → v${latest}"
  local tmpdir; tmpdir="$(mktemp -d)"
  download_binary "$latest" "$arch" "$tmpdir"
  install -m 755 "${tmpdir}/lerd" "${INSTALL_DIR}/${BINARY}"
  [ -f "${tmpdir}/lerd-tray" ] && install -m 755 "${tmpdir}/lerd-tray" "${INSTALL_DIR}/lerd-tray"
  rm -rf "$tmpdir"
  success "Updated to lerd v${latest}"
  star_note
}

# ── Uninstall ────────────────────────────────────────────────────────────────
cmd_uninstall() {
  if [ "$(detect_os)" = "darwin" ]; then
    cmd_uninstall_macos
  else
    cmd_uninstall_linux
  fi
}

# macOS teardown: boot out and remove the user LaunchAgents, stop any detached
# lerd-* podman containers, then drop the binary. The DNS resolver file in
# /etc/resolver and the Podman machine are left to `lerd uninstall` (run before
# the binary is removed) since removing the resolver needs sudo.
cmd_uninstall_macos() {
  header "Uninstalling Lerd"

  # Only `lerd uninstall` drops the DNS resolver (/etc/resolver/test, sudo) and
  # the Podman machine, and it's gone once we delete the binary below. Surface
  # the two-step order while the binary is here so the resolver isn't orphaned.
  if command -v lerd &>/dev/null; then
    warn "This script does not remove the DNS resolver (/etc/resolver/test) or the Podman machine."
    info "Those are torn down by 'lerd uninstall' (needs sudo), which is unavailable once the binary is gone."
    if [ -r /dev/tty ] && ! ask "Continue and remove the lerd binary now?"; then
      info "Aborted. Run 'lerd uninstall' first, then re-run this uninstaller."
      exit 0
    fi
  fi

  local domain="gui/$(id -u)"
  local agents_dir="$HOME/Library/LaunchAgents"

  # lerd's launch agents are named lerd-*.plist on disk, but their launchctl
  # label is the `Label` key inside the plist (com.lerd.<name>), not the
  # filename — read it back before booting the service out.
  if [ -d "$agents_dir" ]; then
    for f in "$agents_dir"/lerd-*.plist; do
      [ -f "$f" ] || continue
      local label; label="$(defaults read "$f" Label 2>/dev/null || true)"
      [ -n "$label" ] && launchctl bootout "$domain/$label" 2>/dev/null || true
      rm -f "$f"
    done
    info "Removed launchd agents from $agents_dir"
  fi

  # Detached `podman run -d` containers outlive their plists, so remove them
  # too. Capture with `|| true` first: under `set -o pipefail` a no-match grep
  # would otherwise abort the whole uninstall before the binary is removed.
  if command -v podman &>/dev/null; then
    local containers
    containers="$(podman ps -a --format '{{.Names}}' 2>/dev/null | grep '^lerd-' || true)"
    for c in $containers; do
      podman rm -f "$c" 2>/dev/null || true
    done
  fi

  rm -rf "$HOME/Library/Logs/lerd"

  # Remove binaries
  for b in "$BINARY" lerd-tray; do
    if [ -f "${INSTALL_DIR}/${b}" ]; then
      rm -f "${INSTALL_DIR}/${b}"
      success "Removed ${INSTALL_DIR}/${b}"
    fi
  done

  remove_from_path

  if ask "Remove all Lerd data and config? (~/.config/lerd, ~/.local/share/lerd)"; then
    rm -rf "$LERD_CONFIG_DIR"
    rm -rf "$LERD_DATA_DIR"
    success "Removed config and data directories"
  else
    info "Config kept at $LERD_CONFIG_DIR"
    info "Data kept at $LERD_DATA_DIR"
  fi

  if [ -f /etc/resolver/test ]; then
    warn "DNS resolver /etc/resolver/test is still present (not removed here)."
    info "Remove it with: sudo rm -f /etc/resolver/test"
    info "Remove the Podman machine with: podman machine rm <name>  (see 'podman machine ls')"
  fi

  success "Lerd uninstalled"
}

cmd_uninstall_linux() {
  header "Uninstalling Lerd"

  # Stop and remove systemd units — discover from quadlet files on disk
  local quadlet_dir="${XDG_CONFIG_HOME:-$HOME/.config}/containers/systemd"
  local systemd_user_dir="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"

  if [ -d "$quadlet_dir" ]; then
    for f in "$quadlet_dir"/lerd-*.container; do
      [ -f "$f" ] || continue
      local unit; unit="$(basename "$f" .container)"
      if systemctl --user is-active --quiet "$unit" 2>/dev/null; then
        info "Stopping $unit ..."
        systemctl --user stop "$unit" 2>/dev/null || true
      fi
      systemctl --user disable "$unit" 2>/dev/null || true
    done
    rm -f "$quadlet_dir"/lerd-*.container
    info "Removed Quadlet units from $quadlet_dir"
  fi

  # Stop and remove user service unit files
  for svc in lerd-watcher lerd-ui; do
    if systemctl --user is-active --quiet "$svc" 2>/dev/null; then
      systemctl --user stop "$svc" 2>/dev/null || true
    fi
    systemctl --user disable "$svc" 2>/dev/null || true
    rm -f "$systemd_user_dir/${svc}.service"
  done

  systemctl --user daemon-reload 2>/dev/null || true

  # Remove binary
  if [ -f "${INSTALL_DIR}/${BINARY}" ]; then
    rm -f "${INSTALL_DIR}/${BINARY}"
    success "Removed ${INSTALL_DIR}/${BINARY}"
  fi

  # Remove PATH entry from shell rc
  remove_from_path

  # Optionally remove data
  if ask "Remove all Lerd data and config? (~/.config/lerd, ~/.local/share/lerd)"; then
    rm -rf "$LERD_CONFIG_DIR"
    rm -rf "$LERD_DATA_DIR"
    success "Removed config and data directories"
  else
    info "Config kept at $LERD_CONFIG_DIR"
    info "Data kept at $LERD_DATA_DIR"
  fi

  success "Lerd uninstalled"
}

# ── Entry point ──────────────────────────────────────────────────────────────
main() {
  echo -e "${BOLD}"
  echo "  ██╗     ███████╗██████╗ ██████╗ "
  echo "  ██║     ██╔════╝██╔══██╗██╔══██╗"
  echo "  ██║     █████╗  ██████╔╝██║  ██║"
  echo "  ██║     ██╔══╝  ██╔══██╗██║  ██║"
  echo "  ███████╗███████╗██║  ██║██████╔╝"
  echo "  ╚══════╝╚══════╝╚═╝  ╚═╝╚═════╝ "
  echo -e "${RESET}"
  echo "  Lerd — Podman-powered local PHP dev environment for Linux and macOS"
  echo "  https://github.com/${REPO}"
  echo ""

  case "${1:-install}" in
    --update|-u|update)     cmd_update ;;
    --uninstall|uninstall)  cmd_uninstall ;;
    --check|check)
      # Non-interactive: report the full requirement set (managed DNS), so the
      # check never blocks on a prompt. Picking .localhost at real install time
      # is what skips the HTTPS-only packages.
      MISSING_PKGS=()
      check_prerequisites
      ;;
    --local)
      [ -n "${2:-}" ] || die "--local requires a path argument, e.g: --local ./build/lerd"
      cmd_install "$2"
      ;;
    --help|-h)
      echo "Usage: $0 [--update | --uninstall | --check | --local <path>]"
      echo ""
      echo "  (no args)       Install Lerd from latest GitHub release"
      echo "  --local <path>  Install from a locally built binary"
      echo "  --update        Update to the latest release"
      echo "  --uninstall     Remove Lerd and optionally its data"
      echo "  --check         Check prerequisites only"
      ;;
    --install|install|"") cmd_install ;;
    *) die "Unknown option: $1. Run with --help for usage." ;;
  esac
}

# Only run main when executed directly or piped to bash, not when sourced.
# BASH_SOURCE may be an unset array when piped to bash (curl|bash / wget|bash),
# which triggers set -u on some bash versions even with the :- operator.
# Suspend nounset briefly to read it safely.
set +u
_lerd_src="${BASH_SOURCE[0]:-}"
set -u
if [[ -z "$_lerd_src" || "$_lerd_src" == "$0" ]]; then
  main "$@"
fi
unset _lerd_src
