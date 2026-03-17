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
ask()     { echo -en "  ${BOLD}?${RESET}  $* [y/N] "; read -r _ans; [[ "$_ans" =~ ^[Yy]$ ]]; }

# ── Platform detection ───────────────────────────────────────────────────────
detect_arch() {
  case "$(uname -m)" in
    x86_64)  echo "amd64" ;;
    aarch64) echo "arm64" ;;
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

check_nm() {
  if systemctl is-active --quiet NetworkManager 2>/dev/null; then
    success "NetworkManager running"
  else
    warn "NetworkManager not running — required for .test DNS"
    MISSING_PKGS+=("networkmanager")
  fi
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
  header "Checking prerequisites"

  check_cmd podman podman "container runtime"
  check_cmd unzip unzip "needed to extract fnm"
  check_nm
  check_systemd_user
  check_podman_rootless

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
  local family; family="$(distro_family)"

  header "Installing: ${pkgs[*]}"

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
  local body
  # Suppress curl/wget error output — we handle failures ourselves
  case "$(_download_tool)" in
    curl) body="$(curl -fsSL --stderr /dev/null "https://api.github.com/repos/${REPO}/releases/latest" || true)" ;;
    wget) body="$(wget -qO- "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null || true)" ;;
  esac

  # GitHub returns {"message":"Not Found"} when there are no releases yet
  if echo "$body" | grep -q '"message"'; then
    echo ""
    return
  fi

  echo "$body" | grep '"tag_name"' | sed -E 's/.*"tag_name": *"v?([^"]+)".*/\1/' || true
}

# download_binary <version> <arch> <destdir>
# Downloads and extracts the release archive into <destdir>.
# The extracted binary will be at <destdir>/lerd.
# All output goes to stderr — nothing is printed to stdout.
download_binary() {
  local version="$1" arch="$2" destdir="$3"
  local filename="lerd_${version}_linux_${arch}.tar.gz"
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

# ── Shell integration ────────────────────────────────────────────────────────
SHELL_MARKER="# Added by Lerd installer"

detect_shell_rc() {
  local shell; shell="$(basename "${SHELL:-bash}")"
  case "$shell" in
    fish) echo "$HOME/.config/fish/conf.d/lerd.fish" ;;
    zsh)  echo "$HOME/.zshrc" ;;
    *)    echo "$HOME/.bashrc" ;;
  esac
}

add_to_path() {
  local shell; shell="$(basename "${SHELL:-bash}")"
  local rc; rc="$(detect_shell_rc)"

  # Don't add if already present
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

  check_prerequisites

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

    local tmpdir; tmpdir="$(mktemp -d)"
    download_binary "$version" "$arch" "$tmpdir"
    install -m 755 "${tmpdir}/lerd" "${INSTALL_DIR}/${BINARY}"
    rm -rf "$tmpdir"
    success "Installed lerd v${version} → ${INSTALL_DIR}/${BINARY}"
  fi

  add_to_path

  echo ""
  info "Running 'lerd install' to complete setup ..."
  echo ""
  "${INSTALL_DIR}/${BINARY}" install
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

  info "Updating v${current:-unknown} → v${latest}"
  local tmpdir; tmpdir="$(mktemp -d)"
  download_binary "$latest" "$arch" "$tmpdir"
  install -m 755 "${tmpdir}/lerd" "${INSTALL_DIR}/${BINARY}"
  rm -rf "$tmpdir"
  success "Updated to lerd v${latest}"
}

# ── Uninstall ────────────────────────────────────────────────────────────────
cmd_uninstall() {
  header "Uninstalling Lerd"

  # Stop and remove systemd units
  for unit in lerd-nginx lerd-dns lerd-watcher lerd-php81-fpm lerd-php82-fpm lerd-php83-fpm lerd-php84-fpm lerd-mysql lerd-redis lerd-postgres lerd-meilisearch lerd-minio; do
    if systemctl --user is-active --quiet "$unit" 2>/dev/null; then
      info "Stopping $unit ..."
      systemctl --user stop "$unit" 2>/dev/null || true
    fi
    if systemctl --user is-enabled --quiet "$unit" 2>/dev/null; then
      systemctl --user disable "$unit" 2>/dev/null || true
    fi
  done

  # Remove Quadlet files
  local quadlet_dir="${XDG_CONFIG_HOME:-$HOME/.config}/containers/systemd"
  if [ -d "$quadlet_dir" ]; then
    rm -f "$quadlet_dir"/lerd-*.container
    info "Removed Quadlet units from $quadlet_dir"
  fi

  # Remove watcher service
  local systemd_user_dir="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
  rm -f "$systemd_user_dir/lerd-watcher.service"

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
  echo "  Laravel Herd for Linux — Podman-native dev environment"
  echo "  https://github.com/${REPO}"
  echo ""

  case "${1:-install}" in
    --update|-u|update)     cmd_update ;;
    --uninstall|uninstall)  cmd_uninstall ;;
    --check|check)
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
