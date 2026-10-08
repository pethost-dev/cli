#!/bin/sh
# Installs pethost, Pethost's command-line tool, on macOS and Linux:
#
#   curl -fsSL https://pethost.dev/install.sh | sh
#
# It finds the system and the CPU, downloads the release's binary from GitHub, checks it against
# the release's checksums and puts it in ~/.local/bin, without sudo. It touches nothing else:
# when that folder is not on PATH it says so, and changes no file of the shell's.
#
#   PETHOST_VERSION        the release to install, as 0.4.0; without it, the newest
#   PETHOST_RELEASES_URL   where the releases are, in place of GitHub
#
# What a release is made of is defined by .github/workflows/release.yml; `pethost update`
# (internal/release) and install.ps1 read the same three addresses.
set -eu

RELEASES=${PETHOST_RELEASES_URL:-https://github.com/pethost-dev/cli/releases}
RELEASES=${RELEASES%/}
DIR=$HOME/.local/bin

die() {
  echo "$*" >&2
  exit 1
}

# The whole script is one function, called on its last line: a download that was cut short
# runs nothing.
main() {
  command -v curl >/dev/null 2>&1 || command -v wget >/dev/null 2>&1 ||
    die "Installing pethost needs curl or wget, and this computer has neither."

  # One a line: a failure inside a command substitution ends the script only when the
  # substitution is all of an assignment.
  system=$(system)
  cpu=$(cpu)
  version=$(release_version)
  binary=pethost-$system-$cpu
  release=$RELEASES/download/v$version

  work=$(mktemp -d)
  trap 'rm -rf "$work"' EXIT
  fetch "$release/$binary" >"$work/pethost" ||
    die "pethost $version could not be downloaded for this computer ($release/$binary)."
  fetch "$release/checksums.txt" >"$work/checksums.txt" ||
    die "The checksums of pethost $version could not be downloaded: nothing was installed."
  verify "$work/pethost" "$work/checksums.txt" "$binary"

  # Into the folder first, then renamed into place: a copy from another disk that is cut short
  # leaves the pethost that was there whole. What a killed run leaves under this name, pethost
  # itself deletes later (internal/release, Tidy).
  mkdir -p "$DIR"
  chmod 755 "$work/pethost"
  mv "$work/pethost" "$DIR/.pethost-new"
  mv "$DIR/.pethost-new" "$DIR/pethost"
  say_done "$version"
}

system() {
  case $(uname -s) in
    Darwin) echo darwin ;;
    Linux) echo linux ;;
    MINGW* | MSYS* | CYGWIN*) die "On Windows, install pethost from PowerShell:   irm https://pethost.dev/install.ps1 | iex" ;;
    *) die "pethost has releases for macOS, Linux and Windows, and none for $(uname -s)." ;;
  esac
}

cpu() {
  case $(uname -m) in
    x86_64 | amd64) echo amd64 ;;
    arm64 | aarch64) echo arm64 ;;
    *) die "pethost has releases for x86-64 and ARM64 CPUs, and none for $(uname -m)." ;;
  esac
}

# The release to install: the one PETHOST_VERSION names, else the newest. Only digits and dots
# make a version: it becomes part of an address.
release_version() {
  version=${PETHOST_VERSION:-}
  if [ -z "$version" ]; then
    version=$(fetch "$RELEASES/latest/download/version") ||
      die "The newest release of pethost could not be read from $RELEASES."
  fi
  version=${version#v}
  case $version in
    '' | *[!0-9.]*) die "\"$version\" is not a release of pethost: a version reads as 0.4.0." ;;
  esac
  echo "$version"
}

# fetch writes what is at an address to stdout; it fails when the address does not answer 200.
fetch() {
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$1"
  else
    wget -qO- "$1"
  fi
}

sha256() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | cut -d' ' -f1
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | cut -d' ' -f1
  else
    die "Checking the download needs sha256sum or shasum, and this computer has neither: nothing was installed."
  fi
}

# verify compares a file's SHA-256 with its line of the release's checksums, which are
# sha256sum's: the sum, two spaces, the name.
verify() {
  expected=
  while read -r sum name; do
    if [ "$name" = "$3" ]; then
      expected=$sum
    fi
  done <"$2"
  [ -n "$expected" ] || die "The release's checksums do not list $3: nothing was installed."

  actual=$(sha256 "$1")
  [ "$actual" = "$expected" ] ||
    die "The download is not what the release published: its checksum differs. Nothing was installed."
}

say_done() {
  shown=$DIR
  case $DIR in
    "$HOME"/*) shown="~${DIR#"$HOME"}" ;;
  esac
  echo "pethost $1 is in $shown (checksum verified)."

  case :$PATH: in
    *:"$DIR":*)
      first=$(command -v pethost || true)
      if [ "$first" != "$DIR/pethost" ]; then
        echo "Another pethost comes before it on your PATH, and is the one that runs: $first"
      fi
      echo "Next: cd into your app, and run   pethost deploy"
      ;;
    *)
      echo "$shown is not on your PATH. Add it:   $(path_line "$shown")"
      echo "Next: open a new terminal, cd into your app, and run   pethost deploy"
      ;;
  esac
}

# path_line is the command that puts the folder on PATH for the person's shell, for them to run:
# the installer changes no file of theirs.
path_line() {
  shell=${SHELL:-}
  case ${shell##*/} in
    fish) echo "fish_add_path $1" ;;
    zsh) echo "echo 'export PATH=\"\$HOME/.local/bin:\$PATH\"' >> ~/.zshrc" ;;
    bash) echo "echo 'export PATH=\"\$HOME/.local/bin:\$PATH\"' >> ~/.bashrc" ;;
    *) echo "echo 'export PATH=\"\$HOME/.local/bin:\$PATH\"' >> ~/.profile" ;;
  esac
}

main "$@"
