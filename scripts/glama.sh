#!/bin/sh
# Helper for listing on Glama (https://glama.ai, a directory that builds and
# probes MCP servers). Procedure: RELEASING.md, "Glama".
#
#   sh scripts/glama.sh form build-steps X.Y.Z   # JSON for the form field "Build steps"
#   sh scripts/glama.sh form cmd                 # JSON for "CMD arguments"
#   sh scripts/glama.sh form placeholder         # JSON for "Placeholder parameters"
#   sh scripts/glama.sh check X.Y.Z              # build the image Glama would build, probe it
#   sh scripts/glama.sh check-local BINARY       # same probe with a local linux/amd64 binary
#
# `check` needs the GitHub Release vX.Y.Z to be published (the binary comes
# from there). `check-local` works before any release exists, e.g. with
# dist/port-keeper_linux_amd64_v1/port-keeper from `goreleaser release --snapshot`.
# Requires docker (daemon running) and node.
#
# The probe goes through mcp-proxy, which validates tools/list with the official
# TypeScript SDK. That catches schema problems the Go SDK's own tests accept.
# Glama only ever sends initialize and tools/list; port-keeper cannot do real
# work in a container (it needs the host's network, processes, working
# directory and ledger), which is why no container image is distributed.
# shellcheck disable=SC2016 # the node -e programs are single-quoted on purpose
set -eu

die() { printf 'glama.sh: %s\n' "$1" >&2; exit 1; }

REPO=gridhra/port-keeper-mcp
# The base Glama generates its Dockerfile from (form defaults as of 2026-09).
# If Glama's Dockerfile preview differs, change these to match.
GLAMA_BASE_IMAGE=debian:trixie-slim
GLAMA_NODE_MAJOR=26
GLAMA_MCP_PROXY=mcp-proxy@6.4.3

check_version() {
  printf '%s' "$1" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$' || die "version must be X.Y.Z (no leading v): $1"
}

# Glama wraps each step in parentheses and joins them with && into one RUN.
# The binary comes from the GitHub Release and is verified against checksums.txt.
build_steps_json() {
  node -e '
const [v, repo] = process.argv.slice(1);
const steps = [
  "apt-get update && apt-get install -y --no-install-recommends ca-certificates curl && rm -rf /var/lib/apt/lists/*",
  "set -eu; case \"$(uname -m)\" in x86_64) a=amd64 ;; aarch64) a=arm64 ;; *) echo unsupported arch >&2; exit 1 ;; esac; " +
    `v=${v}; f=port-keeper_\${v}_linux_$a.tar.gz; mkdir -p /tmp/pk && cd /tmp/pk && ` +
    `curl -fsSLO https://github.com/${repo}/releases/download/v$v/$f && ` +
    `curl -fsSLO https://github.com/${repo}/releases/download/v$v/checksums.txt && ` +
    "grep \" $f\\$\" checksums.txt | sha256sum -c - && tar -xzf $f port-keeper && " +
    "install -m 0755 port-keeper /usr/local/bin/port-keeper && cd / && rm -rf /tmp/pk",
];
console.log(JSON.stringify(steps, null, 2));
' "$1" "$REPO"
}

cmd_json() { printf '["port-keeper", "mcp"]\n'; }
placeholder_json() { printf '{}\n'; }

# probe IMAGE_DIR WANT_VERSION: build the image in IMAGE_DIR, start it, send
# initialize and tools/list through mcp-proxy. WANT_VERSION "" skips the version check.
probe() {
  work=$1
  want=$2
  name="port-keeper-glama-check-$$"
  image="port-keeper-glama-check:$$"
  trap 'docker rm -f "$name" >/dev/null 2>&1 || true; docker rmi "$image" >/dev/null 2>&1 || true; rm -rf "$work"' EXIT

  echo "==> building the Glama-equivalent image (linux/amd64)"
  docker build -q --platform linux/amd64 -t "$image" "$work" >/dev/null

  echo "==> starting the server behind mcp-proxy"
  docker run -d --name "$name" --platform linux/amd64 -p 127.0.0.1::8080 "$image" >/dev/null
  port=$(docker port "$name" 8080/tcp | head -n1 | sed 's/.*://')

  node -e '
const url = process.argv[1], want = process.argv[2];
const H = { "Content-Type": "application/json", Accept: "application/json, text/event-stream" };
const parse = (t) => JSON.parse(t.split("\n").filter((l) => l.startsWith("data: ")).map((l) => l.slice(6)).pop() ?? t);
const post = (body, sid) => fetch(url, { method: "POST", headers: sid ? { ...H, "mcp-session-id": sid } : H, body: JSON.stringify(body) });
(async () => {
  let init;
  for (let i = 0; ; i++) {
    try { init = await post({ jsonrpc: "2.0", id: 1, method: "initialize", params: { protocolVersion: "2025-06-18", capabilities: {}, clientInfo: { name: "glama-check", version: "0" } } }); break; }
    catch (e) { if (i >= 30) throw e; await new Promise((r) => setTimeout(r, 1000)); }
  }
  const sid = init.headers.get("mcp-session-id");
  const info = parse(await init.text()).result.serverInfo;
  await post({ jsonrpc: "2.0", method: "notifications/initialized" }, sid);
  const list = parse(await (await post({ jsonrpc: "2.0", id: 2, method: "tools/list" }, sid)).text());
  if (list.error) { console.error("tools/list failed:", JSON.stringify(list.error).slice(0, 800)); process.exit(1); }
  const names = list.result.tools.map((t) => t.name);
  console.log(`serverInfo: ${info.name} ${info.version}`);
  console.log(`tools/list: ${names.length} tools: ${names.join(", ")}`);
  if (want && info.version !== want) { console.error(`version mismatch: expected ${want}, got ${info.version}`); process.exit(1); }
  if (names.length === 0) { console.error("no tools listed"); process.exit(1); }
  console.log("OK");
})().catch((e) => { console.error(e); process.exit(1); });
' "http://127.0.0.1:$port/mcp" "$want"
}

dockerfile_head() {
  cat <<HEAD
FROM $GLAMA_BASE_IMAGE
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl git && curl -fsSL https://deb.nodesource.com/setup_$GLAMA_NODE_MAJOR.x | bash - && apt-get install -y --no-install-recommends nodejs && npm install -g $GLAMA_MCP_PROXY && apt-get clean && rm -rf /var/lib/apt/lists/*
WORKDIR /app
HEAD
}

need_tools() {
  command -v docker >/dev/null 2>&1 || die "docker not found"
  docker info >/dev/null 2>&1 || die "docker daemon is not running"
  command -v node >/dev/null 2>&1 || die "node not found"
}

check() {
  need_tools
  work=$(mktemp -d)
  build_steps_json "$1" > "$work/steps.json"
  run=$(node -e 'console.log(JSON.parse(require("fs").readFileSync(process.argv[1], "utf8")).map((s) => `(${s})`).join(" && "))' "$work/steps.json")
  {
    dockerfile_head
    printf 'RUN %s\n' "$run"
    printf 'CMD ["mcp-proxy","--","port-keeper","mcp"]\n'
  } > "$work/Dockerfile"
  probe "$work" "$1"
}

check_local() {
  need_tools
  [ -f "$1" ] || die "no such file: $1"
  work=$(mktemp -d)
  cp "$1" "$work/port-keeper"
  {
    dockerfile_head
    printf 'COPY port-keeper /usr/local/bin/port-keeper\n'
    printf 'RUN chmod 0755 /usr/local/bin/port-keeper\n'
    printf 'CMD ["mcp-proxy","--","port-keeper","mcp"]\n'
  } > "$work/Dockerfile"
  probe "$work" ""
}

[ $# -ge 1 ] || die "usage: see the header of this script"
case "$1" in
  form)
    [ $# -ge 2 ] || die "usage: glama.sh form build-steps X.Y.Z | cmd | placeholder"
    case "$2" in
      build-steps) [ $# -eq 3 ] || die "usage: glama.sh form build-steps X.Y.Z"; check_version "$3"; build_steps_json "$3" ;;
      cmd) cmd_json ;;
      placeholder) placeholder_json ;;
      *) die "unknown form field: $2" ;;
    esac
    ;;
  check)
    [ $# -eq 2 ] || die "usage: glama.sh check X.Y.Z"
    check_version "$2"
    check "$2"
    ;;
  check-local)
    [ $# -eq 2 ] || die "usage: glama.sh check-local BINARY"
    check_local "$2"
    ;;
  *) die "unknown command: $1" ;;
esac
