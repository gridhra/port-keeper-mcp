#!/bin/sh
# Manual pre-release agent eval (release gate; see RELEASING.md).
#
# Measures, for an MCP-connected Claude Code, how many round trips three
# everyday tasks take and whether a port number leaks into what the agent
# runs or says. Each scenario runs against a throwaway ledger and a
# throwaway demo project; nothing outside the temp directory is touched.
#
#   sh scripts/agent_eval.sh              # run all scenarios (uses your claude login or ANTHROPIC_API_KEY; needs claude, jq, git, python3)
#   sh scripts/agent_eval.sh --list       # print scenarios and limits; no API, no files
#   sh scripts/agent_eval.sh --dry-run    # build the temp ledger/project/mcp-config and print the claude command lines; no API
#   sh scripts/agent_eval.sh s1 s3        # subset
#
# env: PK_EVAL_MODEL (default sonnet), PK_EVAL_BUDGET_USD (default 0.50 per scenario),
#      PK_EVAL_BIN (default: port-keeper on PATH), PK_EVAL_KEEP=1 keeps the temp dir
#
# The claude flags below were checked against `claude --help` of 2.1.275 and
# the stream-json field names against a real run. `--setting-sources local`
# keeps the developer's user-level hooks (port-keeper's own SessionStart hook
# among them) and settings out of the run while leaving the login usable;
# `--bare` would need ANTHROPIC_API_KEY and `--safe-mode` also drops the
# --mcp-config server. A hook output mentioning port-keeper is reported as a
# leak so the measurement is not helped by the developer's own setup.
set -eu

model="${PK_EVAL_MODEL:-sonnet}"
budget="${PK_EVAL_BUDGET_USD:-0.50}"
bin="${PK_EVAL_BIN:-port-keeper}"
pool_re='(2[0-9]{4}|3[01][0-9]{3})'

# Scenario table: id, cwd (relative to the temp root), expected tools,
# max port-keeper round trips (MCP tool calls plus Bash commands that run
# port-keeper; reading the repository and Claude Code's own ToolSearch are the
# agent's cost, not port-keeper's), max pool numbers in the final text, extra
# allowed tools. Prompts live in scenario_prompt below.
scenarios='
s1 demo        resolve_url>=1,no-render_env                       2 1 -
s2 demo-hotfix slot_new==1                                        5 0 Bash(port-keeper*)
s3 demo        env-before-dev.sh,no-number-or-port-in-Bash        4 1 Bash(./dev.sh*),Bash(port-keeper*)
'

scenario_prompt() {
  case "$1" in
    s1) echo "What is the URL of the admin service of slot 3 of this project?" ;;
    s2) echo "This directory is a fresh git worktree for a hotfix. Give it its own environment so it can run alongside the main working copy. Do not start anything." ;;
    s3) echo "Run ./dev.sh (the web dev server) in the background and tell me the name of the service it is serving." ;;
    *) echo "unknown scenario: $1" >&2; exit 2 ;;
  esac
}

# scenario_field ID N: column N of the scenario table row for ID.
scenario_field() {
  printf '%s\n' "$scenarios" | awk -v id="$1" -v n="$2" '$1 == id { print $n }'
}

list() {
  printf '%-4s %-11s %-50s %5s %7s %s\n' id cwd expect calls numbers extra-allowed
  printf '%s\n' "$scenarios" | awk 'NF { printf "%-4s %-11s %-50s %5s %7s %s\n", $1, $2, $3, $4, $5, $6 }'
  printf '\nmodel=%s budget=%s USD per scenario\n' "$model" "$budget"
  printf 's1 prompt: %s\n' "$(scenario_prompt s1)"
  printf 's2 prompt: %s\n' "$(scenario_prompt s2)"
  printf 's3 prompt: %s\n' "$(scenario_prompt s3)"
}

mode=run
ids=""
for arg in "$@"; do
  case "$arg" in
    --list) mode=list ;;
    --dry-run) mode=dry ;;
    -h|--help) sed -n '2,20p' "$0"; exit 0 ;;
    s1|s2|s3) ids="$ids $arg" ;;
    *) echo "unknown argument: $arg" >&2; exit 2 ;;
  esac
done
[ -n "$ids" ] || ids="s1 s2 s3"

if [ "$mode" = list ]; then
  list
  exit 0
fi

for tool in jq git python3; do
  command -v "$tool" >/dev/null 2>&1 || { echo "missing: $tool" >&2; exit 1; }
done
if [ "$mode" = run ]; then
  command -v claude >/dev/null 2>&1 || { echo "missing: claude" >&2; exit 1; }
fi
command -v "$bin" >/dev/null 2>&1 || { echo "missing: $bin (set PK_EVAL_BIN)" >&2; exit 1; }
bin="$(command -v "$bin")"
case "$bin" in /*) ;; *) bin="$(cd "$(dirname "$bin")" && pwd)/$(basename "$bin")" ;; esac

root="$(mktemp -d)"
# shellcheck disable=SC2317  # reached through the trap below
cleanup() {
  pkill -f "$root" >/dev/null 2>&1 || true
  if [ "${PK_EVAL_KEEP:-0}" = 1 ]; then
    echo "kept: $root"
  else
    rm -rf "$root"
  fi
}
trap cleanup EXIT INT TERM

export PORT_KEEPER_STATE_DIR="$root/state"
export PORT_KEEPER_CONFIG_DIR="$root/config"
mkdir -p "$PORT_KEEPER_STATE_DIR" "$PORT_KEEPER_CONFIG_DIR"

# Demo project: four services, one derived URL, a dev server script that
# refuses to start without WEB_PORT.
demo="$root/demo"
mkdir -p "$demo"
git -C "$demo" init -q
cat > "$demo/port-keeper.toml" <<'EOF'
[project]
name = "demo"
[[service]]
name = "web"
env = "WEB_PORT"
proto = "http"
[[service]]
name = "admin"
env = "ADMIN_PORT"
proto = "http"
[[service]]
name = "api"
env = "API_PORT"
proto = "http"
[[service]]
name = "db"
env = "DB_PORT"
proto = "tcp"
tier = "infra"
[[derive]]
env = "WEB_URL"
value = "${url.web}"
EOF
echo ".env.local" > "$demo/.gitignore"
cat > "$demo/dev.sh" <<'EOF'
#!/bin/sh
exec python3 -m http.server "${WEB_PORT:?run port-keeper env first}" --bind 127.0.0.1
EOF
chmod +x "$demo/dev.sh"
git -C "$demo" add port-keeper.toml .gitignore dev.sh
git -C "$demo" -c user.name=eval -c user.email=eval@example.invalid commit -q -m "demo project"

# Slot 1 bound to demo; slots 2 and 3 exist, slot 3 leased; demo-hotfix is a
# worktree left unbound on purpose (scenario s2 must bind it).
(cd "$demo" && "$bin" env >/dev/null)
(cd "$demo" && "$bin" slot new 2 --no-bind >/dev/null)
(cd "$demo" && "$bin" slot new 3 --no-bind >/dev/null)
(cd "$demo" && "$bin" --slot 3 env --stdout >/dev/null)
git -C "$demo" worktree add -q "$root/demo-hotfix" -b hotfix

jq -n --arg bin "$bin" --arg state "$PORT_KEEPER_STATE_DIR" --arg config "$PORT_KEEPER_CONFIG_DIR" \
  '{mcpServers: {"port-keeper": {command: $bin, args: ["mcp"], env: {PORT_KEEPER_STATE_DIR: $state, PORT_KEEPER_CONFIG_DIR: $config}}}}' \
  > "$root/mcp.json"

# The block README.md tells users to put in their user-level instructions.
cat > "$root/instructions.md" <<'EOF'
## Local ports

Local dev ports on this machine are managed by port-keeper (MCP server `port-keeper`).
Never choose a port number yourself and never start a server on an ad-hoc port.
To find where something runs, call the `resolve_url` tool
(or run `port-keeper url <project>/<slot>/<service>`).
If a task needs a new port, add a service to `port-keeper.toml` and run `port-keeper env`.
To give a command this slot's ports, prefix it with `eval "$(port-keeper env --format export)"`; never paste numbers into a command or a file.
Refer to services by name (`shop/3/admin`), never by number, in docs, issues and chat.
EOF

# run_claude ID: run one scenario from its cwd; output goes to $root/ID.jsonl.
# Extra allowed tools are passed by re-setting the positional parameters.
run_claude() {
  id=$1
  prompt="$(scenario_prompt "$id")"
  cwd="$root/$(scenario_field "$id" 2)"
  set -- "mcp__port-keeper"
  case "$id" in
    s2) set -- "$@" "Bash(port-keeper *)" ;;
    s3) set -- "$@" "Bash(./dev.sh *)" "Bash(port-keeper *)" ;;
  esac
  if [ "$mode" = dry ]; then
    printf '\n# %s (cwd %s)\n' "$id" "$cwd"
    printf 'claude -p %s --setting-sources local --mcp-config %s --strict-mcp-config \\\n' "'$prompt'" "$root/mcp.json"
    printf '  --append-system-prompt-file %s --model %s \\\n' "$root/instructions.md" "$model"
    printf '  --max-budget-usd %s --permission-mode dontAsk --permission-prompts none \\\n' "$budget"
    printf '  --allowedTools'
    for t in "$@"; do printf ' "%s"' "$t"; done
    printf ' --no-session-persistence \\\n'
    printf '  --output-format stream-json --verbose --include-hook-events > %s\n' "$root/$id.jsonl"
    return 0
  fi
  (
    cd "$cwd"
    claude -p "$prompt" --setting-sources local --mcp-config "$root/mcp.json" --strict-mcp-config \
      --append-system-prompt-file "$root/instructions.md" --model "$model" \
      --max-budget-usd "$budget" --permission-mode dontAsk --permission-prompts none \
      --allowedTools "$@" --no-session-persistence \
      --output-format stream-json --verbose --include-hook-events > "$root/$id.jsonl"
  ) || true
}

# Field names below (type, subtype, mcp_servers, message.content, tool_use,
# num_turns, result, total_cost_usd) were confirmed against claude 2.1.275.
tools_of() { jq -r 'select(.type=="assistant") | .message.content[]? | select(.type=="tool_use") | .name' "$1"; }
bash_of() { jq -r 'select(.type=="assistant") | .message.content[]? | select(.type=="tool_use" and .name=="Bash") | .input.command' "$1"; }
count() { grep -c -x -- "$1" "$2" 2>/dev/null || true; }

# evaluate ID: prints "calls/max tools numbers/max cost PASS|FAIL reason".
evaluate() {
  id=$1
  out="$root/$id.jsonl"
  maxturns="$(scenario_field "$id" 4)"
  maxnums="$(scenario_field "$id" 5)"
  reason=""
  mcp="$(jq -r 'select(.type=="system" and .subtype=="init") | .mcp_servers[]? | select(.name=="port-keeper") | .status' "$out" 2>/dev/null | head -n 1)"
  if [ "$mcp" != connected ]; then
    printf '%-8s %9s %-40s %11s %6s %s\n' "$id" "-" "-" "-" "-" "FAIL setup failure (port-keeper MCP status: ${mcp:-none})"
    return 1
  fi
  if jq -r 'select(.subtype=="hook_response") | .output // ""' "$out" | grep -q 'port-keeper'; then
    reason="$reason a hook injected port-keeper context (developer setup leaked into the run);"
  fi
  tools_of "$out" > "$root/$id.tools"
  bash_of "$out" > "$root/$id.bash"
  turns=$(( $(grep -c '^mcp__port-keeper__' "$root/$id.tools" || true) + $(grep -c 'port-keeper' "$root/$id.bash" || true) ))
  cost="$(jq -r 'select(.type=="result") | .total_cost_usd // empty' "$out" | tail -n 1)"
  final="$(jq -r 'select(.type=="result") | .result // empty' "$out")"
  nums="$(printf '%s\n' "$final" | grep -o -E "$pool_re" | wc -l | tr -d ' ')"
  tools="$(grep -v -x ToolSearch "$root/$id.tools" | sort | uniq -c | awk '{ sub("mcp__port-keeper__", "", $2); printf "%s%s=%s", sep, $2, $1; sep="," } END { if (!sep) printf "-" }')"

  case "$id" in
    s1)
      [ "$(count mcp__port-keeper__resolve_url "$root/$id.tools")" -ge 1 ] || reason="$reason no resolve_url;"
      [ "$(count mcp__port-keeper__render_env "$root/$id.tools")" -eq 0 ] || reason="$reason render_env called;"
      ;;
    s2)
      [ "$(count mcp__port-keeper__slot_new "$root/$id.tools")" -eq 1 ] || reason="$reason slot_new != 1;"
      ;;
    s3)
      # Order of events: MCP env/port lookups and Bash commands, in call order.
      # A Bash command starts dev.sh when ./dev.sh follows the start, &&, ; or |
      # (so `cat ./dev.sh` is reading, not starting). The env lookup may sit in
      # the same command, before it, as in: eval "$(port-keeper env --format export)" && ./dev.sh
      jq -r 'select(.type=="assistant") | .message.content[]? | select(.type=="tool_use")
        | if .name=="Bash" then "bash " + (.input.command // "") else .name end' "$out" > "$root/$id.events"
      env_first="$(awk '
        /^mcp__port-keeper__render_env$/ || /^mcp__port-keeper__resolve_port$/ || /^bash .*port-keeper env/ { if (!seen_dev) ok = 1 }
        /(^bash |&& |; |\| )\.\/dev\.sh/ { seen_dev = 1 }
        END { print ok + 0 }' "$root/$id.events")"
      [ "$env_first" = 1 ] || reason="$reason no env lookup before dev.sh;"
      if grep -E -q -- "$pool_re|--port" "$root/$id.bash"; then reason="$reason number or --port in Bash;"; fi
      ;;
  esac
  [ "$turns" -le "$maxturns" ] 2>/dev/null || reason="$reason calls > $maxturns;"
  [ "$nums" -le "$maxnums" ] || reason="$reason numbers > $maxnums;"

  verdict=PASS
  [ -z "$reason" ] || verdict="FAIL$reason"
  printf '%-8s %9s %-40s %11s %6s %s\n' "$id" "$turns/$maxturns" "$tools" "$nums/$maxnums" "${cost:--}" "$verdict"
  [ -z "$reason" ]
}

if [ "$mode" = dry ]; then
  for id in $ids; do run_claude "$id"; done
  printf '\n# %s/mcp.json\n' "$root"
  cat "$root/mcp.json"
  exit 0
fi

status=0
printf '%-8s %9s %-40s %11s %6s %s\n' scenario calls/max tools numbers/max cost result
for id in $ids; do
  run_claude "$id"
  evaluate "$id" || status=1
done
exit "$status"
