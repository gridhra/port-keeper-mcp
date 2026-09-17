#!/bin/sh
# Tests scripts/install.sh against a fake release in a temporary directory.
# No network, nothing written outside the temporary directory.
#
#   sh scripts/install_test.sh
set -eu

here="$(cd "$(dirname "$0")" && pwd)"
root="$(mktemp -d)"
trap 'rm -rf "$root"' EXIT INT TERM
failures=0

fail() { printf 'FAIL: %s\n' "$1" >&2; failures=$((failures + 1)); }
pass() { printf 'ok: %s\n' "$1"; }

case "$(uname -s)" in Darwin) goos=darwin ;; Linux) goos=linux ;; *) echo "unsupported test host" >&2; exit 1 ;; esac
case "$(uname -m)" in x86_64|amd64) goarch=amd64 ;; arm64|aarch64) goarch=arm64 ;; *) echo "unsupported test host" >&2; exit 1 ;; esac
sha() { if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | cut -d' ' -f1; else shasum -a 256 "$1" | cut -d' ' -f1; fi; }

# make_release DIR VERSION: a release directory holding one archive for this host.
make_release() {
  mkdir -p "$1/src"
  printf '#!/bin/sh\necho "port-keeper %s (fake)"\n' "$2" > "$1/src/port-keeper"
  chmod 755 "$1/src/port-keeper"
  : > "$1/src/LICENSE"
  archive="port-keeper_${2}_${goos}_${goarch}.tar.gz"
  tar -C "$1/src" -czf "$1/$archive" port-keeper LICENSE
  printf '%s  %s\n' "$(sha "$1/$archive")" "$archive" > "$1/checksums.txt"
}

run_install() { # run_install RELEASE_DIR VERSION INSTALL_DIR
  PORT_KEEPER_DOWNLOAD_BASE="$1" PORT_KEEPER_VERSION="$2" PORT_KEEPER_INSTALL_DIR="$3" \
    sh "$here/install.sh" > "$root/out" 2>&1
}

# 1. A fresh install puts a runnable binary in place and mentions PATH.
make_release "$root/r1" 9.9.1
if run_install "$root/r1" 9.9.1 "$root/bin" && [ "$("$root/bin/port-keeper")" = "port-keeper 9.9.1 (fake)" ] && grep -q "not on your PATH" "$root/out"; then
  pass "fresh install"
else
  fail "fresh install"; cat "$root/out" >&2
fi

# 2. Re-running replaces the binary; a leading v in the version is accepted.
make_release "$root/r2" 9.9.2
if run_install "$root/r2" v9.9.2 "$root/bin" && [ "$("$root/bin/port-keeper")" = "port-keeper 9.9.2 (fake)" ]; then
  pass "update in place"
else
  fail "update in place"; cat "$root/out" >&2
fi

# 3. A checksum mismatch installs nothing and keeps the existing binary.
make_release "$root/r3" 9.9.3
printf '%064d  port-keeper_9.9.3_%s_%s.tar.gz\n' 0 "$goos" "$goarch" > "$root/r3/checksums.txt"
if run_install "$root/r3" 9.9.3 "$root/bin"; then
  fail "checksum mismatch was accepted"
elif [ "$("$root/bin/port-keeper")" != "port-keeper 9.9.2 (fake)" ]; then
  fail "checksum mismatch damaged the existing binary"
elif ! grep -q "checksum mismatch" "$root/out"; then
  fail "checksum mismatch not reported"; cat "$root/out" >&2
elif [ -n "$(find "$root/bin" -name '.port-keeper.*')" ]; then
  fail "checksum mismatch left a staged file behind"
else
  pass "checksum mismatch"
fi

# 4. An archive missing from checksums.txt installs nothing.
make_release "$root/r4" 9.9.4
: > "$root/r4/checksums.txt"
if run_install "$root/r4" 9.9.4 "$root/bin4" || [ -e "$root/bin4/port-keeper" ]; then
  fail "unlisted archive was installed"
else
  pass "unlisted archive"
fi

# 5. An unsupported machine gets the go install hint.
mkdir -p "$root/fakebin"
# shellcheck disable=SC2016 # the fake uname must receive a literal $1
printf '#!/bin/sh\ncase "$1" in -s) echo Plan9 ;; -m) echo mips ;; esac\n' > "$root/fakebin/uname"
chmod 755 "$root/fakebin/uname"
# (A subshell: in POSIX sh an assignment in front of a function call outlives the call.)
if (PATH="$root/fakebin:$PATH"; run_install "$root/r1" 9.9.1 "$root/bin5") || [ -e "$root/bin5/port-keeper" ]; then
  fail "unsupported platform was installed"
elif ! grep -q "Plan9/mips" "$root/out" || ! grep -q "go install" "$root/out"; then
  fail "unsupported platform message"; cat "$root/out" >&2
else
  pass "unsupported platform"
fi

# 6. A malformed version is refused before anything is fetched.
if run_install "$root/r1" '1.0; rm -rf /' "$root/bin6" || ! grep -q "invalid version" "$root/out"; then
  fail "malformed version"
else
  pass "malformed version"
fi

[ "$failures" -eq 0 ] || { printf '%s test(s) failed\n' "$failures" >&2; exit 1; }
echo "all install.sh tests passed"
