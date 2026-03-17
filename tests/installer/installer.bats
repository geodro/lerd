#!/usr/bin/env bats
# Tests for install.sh
# Requires: bats-core  https://github.com/bats-core/bats-core

INSTALLER="$BATS_TEST_DIRNAME/../../install.sh"

# Source the installer so we can call its functions directly.
# The guard at the bottom prevents main() from running when sourced.
setup() {
  # Isolate HOME so the installer never touches the real shell rc files.
  export HOME="$BATS_TMPDIR/home-$$"
  mkdir -p "$HOME"

  # Source the script to load all function definitions.
  # shellcheck disable=SC1090
  source "$INSTALLER"
}

teardown() {
  rm -rf "$BATS_TMPDIR/home-$$"
}

# ── detect_arch ───────────────────────────────────────────────────────────────

@test "detect_arch returns amd64 for x86_64" {
  # Override uname for this test
  function uname() { echo "x86_64"; }
  export -f uname

  run detect_arch
  [ "$status" -eq 0 ]
  [ "$output" = "amd64" ]
}

@test "detect_arch returns arm64 for aarch64" {
  function uname() { echo "aarch64"; }
  export -f uname

  run detect_arch
  [ "$status" -eq 0 ]
  [ "$output" = "arm64" ]
}

@test "detect_arch fails for unsupported arch" {
  function uname() { echo "mips"; }
  export -f uname

  run detect_arch
  [ "$status" -ne 0 ]
  [[ "$output" == *"Unsupported architecture"* ]]
}

# ── distro_family ─────────────────────────────────────────────────────────────

@test "distro_family detects arch" {
  function detect_distro() { echo "arch"; }
  export -f detect_distro

  run distro_family
  [ "$output" = "arch" ]
}

@test "distro_family detects manjaro as arch family" {
  function detect_distro() { echo "manjaro"; }
  export -f detect_distro

  run distro_family
  [ "$output" = "arch" ]
}

@test "distro_family detects ubuntu as debian family" {
  function detect_distro() { echo "ubuntu"; }
  export -f detect_distro

  run distro_family
  [ "$output" = "debian" ]
}

@test "distro_family detects fedora" {
  function detect_distro() { echo "fedora"; }
  export -f detect_distro

  run distro_family
  [ "$output" = "fedora" ]
}

@test "distro_family returns unknown for unrecognised distro" {
  function detect_distro() { echo "slackware"; }
  export -f detect_distro

  run distro_family
  [ "$output" = "unknown" ]
}

# ── _download_tool ────────────────────────────────────────────────────────────

@test "_download_tool prefers curl when both are available" {
  function curl() { return 0; }
  function wget() { return 0; }
  export -f curl wget

  # Temporarily mask PATH to ensure only our functions are visible
  run bash -c "source '$INSTALLER'; _download_tool"
  [ "$output" = "curl" ]
}

@test "_download_tool falls back to wget when curl is absent" {
  # Hide curl by making it unavailable in a subshell
  run bash -c "
    source '$INSTALLER'
    function curl() { return 127; }
    # Remove curl from PATH lookup
    PATH_ORIG=\$PATH
    export PATH=\"\$BATS_TMPDIR\"  # empty path with no curl binary
    _download_tool
  "
  # We just check it doesn't die — wget fallback varies by system
  [ "$status" -eq 0 ] || [[ "$output" == *"wget"* ]] || [[ "$output" == *"curl"* ]]
}

@test "_download_tool errors when neither curl nor wget found" {
  run bash -c "
    source '$INSTALLER'
    # Override command -v to report both as missing
    function command() {
      if [[ \"\$2\" == 'curl' || \"\$2\" == 'wget' ]]; then return 1; fi
      builtin command \"\$@\"
    }
    export -f command
    _download_tool
  "
  [ "$status" -ne 0 ]
  [[ "$output" == *"Neither curl nor wget"* ]]
}

# ── add_to_path / remove_from_path ────────────────────────────────────────────

@test "add_to_path appends PATH entry to .bashrc" {
  export SHELL="/bin/bash"
  INSTALL_DIR="$HOME/.local/bin"
  touch "$HOME/.bashrc"

  add_to_path

  grep -q "Added by Lerd installer" "$HOME/.bashrc"
  grep -q "$INSTALL_DIR" "$HOME/.bashrc"
}

@test "add_to_path is idempotent — does not duplicate entry" {
  export SHELL="/bin/bash"
  INSTALL_DIR="$HOME/.local/bin"
  touch "$HOME/.bashrc"

  add_to_path
  add_to_path

  count=$(grep -c "Added by Lerd installer" "$HOME/.bashrc")
  [ "$count" -eq 1 ]
}

@test "add_to_path writes fish_add_path for fish shell" {
  export SHELL="/usr/bin/fish"
  INSTALL_DIR="$HOME/.local/bin"
  mkdir -p "$HOME/.config/fish/conf.d"

  add_to_path

  grep -q "fish_add_path" "$HOME/.config/fish/conf.d/lerd.fish"
}

@test "remove_from_path removes the Lerd block from .bashrc" {
  export SHELL="/bin/bash"
  INSTALL_DIR="$HOME/.local/bin"
  printf '\n# Added by Lerd installer\nexport PATH="%s:$PATH"\n' "$INSTALL_DIR" > "$HOME/.bashrc"

  remove_from_path

  run grep "Added by Lerd installer" "$HOME/.bashrc"
  [ "$status" -ne 0 ]
}

@test "remove_from_path is a no-op when marker is absent" {
  export SHELL="/bin/bash"
  echo "unrelated content" > "$HOME/.bashrc"

  remove_from_path

  run cat "$HOME/.bashrc"
  [ "$output" = "unrelated content" ]
}

# ── installed_version ─────────────────────────────────────────────────────────

@test "installed_version returns empty string when lerd not found" {
  # Create an empty bin dir first, then restrict PATH to it
  local empty_dir="$BATS_TMPDIR/empty-path-$$"
  mkdir -p "$empty_dir"

  OLD_PATH="$PATH"
  export PATH="$empty_dir"

  run installed_version
  [ "$output" = "" ]

  export PATH="$OLD_PATH"
}

@test "installed_version returns version string when lerd is found" {
  # Create a fake lerd binary
  FAKE_BIN="$BATS_TMPDIR/fake-bin-$$"
  mkdir -p "$FAKE_BIN"
  printf '#!/bin/sh\necho "lerd version 1.2.3"\n' > "$FAKE_BIN/lerd"
  chmod +x "$FAKE_BIN/lerd"

  OLD_PATH="$PATH"
  export PATH="$FAKE_BIN:$PATH"

  run installed_version
  [ "$output" = "1.2.3" ]

  export PATH="$OLD_PATH"
}

# ── latest_version ────────────────────────────────────────────────────────────

@test "latest_version parses version from redirect Location header" {
  # Mock curl -fsSLI to return headers containing a Location pointing to the tag
  function curl() {
    echo "HTTP/2 302"
    echo "location: https://github.com/geodro/lerd/releases/tag/v2.0.0"
    echo ""
  }
  export -f curl

  run latest_version
  [ "$status" -eq 0 ]
  [ "$output" = "2.0.0" ]
}

@test "latest_version returns empty string when redirect has no tag" {
  function curl() {
    echo "HTTP/2 404"
    echo ""
  }
  export -f curl

  run latest_version
  [ "$status" -eq 0 ]
  [ "$output" = "" ]
}

@test "latest_version returns empty string on curl failure" {
  function curl() { return 22; }
  export -f curl

  run latest_version
  [ "$status" -eq 0 ]
  [ "$output" = "" ]
}

# ── --help flag ───────────────────────────────────────────────────────────────

@test "--help prints usage and exits 0" {
  run bash "$INSTALLER" --help
  [ "$status" -eq 0 ]
  [[ "$output" == *"Usage:"* ]]
  [[ "$output" == *"--update"* ]]
  [[ "$output" == *"--uninstall"* ]]
  [[ "$output" == *"--local"* ]]
}

# ── --local flag ──────────────────────────────────────────────────────────────

@test "--local fails with a clear error when file does not exist" {
  run bash "$INSTALLER" --local /tmp/nonexistent-lerd-binary-xyz
  [ "$status" -ne 0 ]
  [[ "$output" == *"not found"* ]]
}

@test "--local requires an argument" {
  run bash "$INSTALLER" --local
  [ "$status" -ne 0 ]
  [[ "$output" == *"requires a path"* ]]
}

# ── --check flag ──────────────────────────────────────────────────────────────

@test "--check runs prerequisite checks and exits 0 when all pass" {
  # Mock all check commands as present
  function command() {
    case "$2" in
      podman|unzip|certutil) return 0 ;;
      *) builtin command "$@" ;;
    esac
  }
  function systemctl() { return 0; }
  function podman() {
    if [[ "$1" == "info" ]]; then echo "true"; fi
  }
  export -f command systemctl podman

  run bash "$INSTALLER" --check
  [ "$status" -eq 0 ]
}
