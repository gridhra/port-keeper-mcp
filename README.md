# port-keeper-mcp

**English** | [日本語](README.ja.md) | [简体中文](README.zh-CN.md)

A local ledger for development ports, with an MCP server on top, written in Go.

A project declares its services in a small manifest (names and env-var names,
never numbers). port-keeper hands each *slot* (a parallel copy of the project:
one per working copy, whether that is a clone or a git worktree) a block of
ports from a pool, writes them into your `.env`, and answers "what is the URL
of `shop/5/admin`?" from the CLI or over MCP. Neither you nor your coding agent
has to remember a port number again. It runs only when called, writes nothing
outside the ledger except the files you point it at inside your project, and
never opens a network listener.

See [docs/DESIGN.md](docs/DESIGN.md) for the full design (Japanese).

## Quickstart

```sh
go install github.com/gridhra/port-keeper-mcp/cmd/port-keeper@latest   # until the first release

cd your-project
port-keeper init          # writes port-keeper.toml (names only) and gitignores .env.local
$EDITOR port-keeper.toml  # one [[service]] per port your project needs
port-keeper env           # leases a block, writes the managed section of .env.local
port-keeper url web       # http://localhost:20000  (add --open to launch the browser)
```

A second working copy of the same project gets its own slot:

```sh
git worktree add ../your-project-hotfix -b hotfix && cd ../your-project-hotfix
port-keeper slot new      # leases a fresh block for this working copy
port-keeper env           # writes its .env.local; nothing collides with the first copy
```

Give your coding agent the same view:

```sh
claude mcp add --scope user port-keeper -- port-keeper mcp
```

Then add the hook and the two-line instruction from
[Working with coding agents](#working-with-coding-agents).

Planned install channels (see `docs/ROADMAP.md`): prebuilt binaries via
GoReleaser, a Homebrew tap, and an npm wrapper so that `npx port-keeper-mcp`
works from any MCP client's config. `server.json` is the draft MCP-registry
manifest for that wrapper; it is not published yet.

## Why

- **Offset formulas break.** "Base port + (slot − 1) × 1000" works until two
  services sit exactly 4000 apart; then slot 5 collides with slot 1 and the
  project is stuck at four environments with thousands of ports unused.
- **Cross-project collisions are nobody's job.** Every project on a laptop
  carves its own thousand-range, and the OS grabs a few conventional ports on
  top (on macOS, the AirPlay receiver in Control Center owns 5000 and 7000).
  Whoever starts second loses.
- **Numbers leak.** People paste `localhost:3001` into chat, docs and issues
  because they had to memorise it. A name (`shop/3/admin`) leaks nothing and
  resolves on every machine.
- **Agents guess.** A coding agent that needs a server picks 8000 or 3000 and
  tramples whatever was there. Give it a tool to ask instead.

## Use cases

1. **Fifth working copy, no arithmetic**
   > "Spin up another copy of this project for the hotfix branch."
   `port-keeper slot new hotfix` leases a fresh block, and `port-keeper env`
   writes the `.env.local` section; your usual `mise run dev` starts
   everything. No collisions with the other four slots or with any other
   project on the box.

2. **Open the right admin without remembering anything**
   > "Open the admin screen of slot 3."
   `port-keeper url shop/3/admin --open`, or the `resolve_url` MCP tool from
   your agent. One round trip, one URL.

3. **Share a database between two app environments**
   > "Slot 2 should use slot 1's MySQL and mail catcher."
   `port-keeper slot new 2 --infra-from 1`. Services tagged `tier = "infra"`
   resolve to slot 1's ports; everything else gets its own.

4. **Find out who is squatting on your port**
   > "The API says address already in use."
   `port-keeper status` compares the ledger with what is actually listening
   and names the PID. When the listener's working directory is outside this
   working copy (and it is not a container runtime), the lease is marked
   `hijacked`; otherwise it is simply `active`. It never kills anything;
   `port-keeper reassign <service>` moves that service to a free port instead.

5. **Migrate an existing project without breaking bookmarks**
   Keep the main environment on its historic numbers with
   `port-keeper pin web=3000 admin=3001 api=8080 --reason "docs and bookmarks"`.
   Pins are arbitrated across every project in the ledger, so two projects
   cannot both claim 3000. Every other slot moves to the pool (update those
   bookmarks with `port-keeper url`), and `doctor` reminds you to `unpin` once
   the docs say `port-keeper url` instead.

## Working with coding agents

port-keeper knows nothing about any particular agent. The contract every
client uses is two commands:

- `port-keeper context --json`: where am I (project, slot, whether this
  working copy is ready to use), and what to do next. No port numbers.
- `port-keeper env --format export --if-present`: the environment for the
  current slot as `export` lines; silent outside a project.

Everything client-specific is an adapter on top of those two. `port-keeper
hook claude` is the adapter for Claude Code's hook protocol; it lives in one
file and other agents can get their own without touching the core.

### Claude Code

Register the MCP server once, user-wide (there are no numbers in the config,
only the command):

```sh
claude mcp add --scope user port-keeper -- port-keeper mcp
```

Add the hook in `~/.claude/settings.json`. On `SessionStart` it exports this
slot's ports into the agent's shell and adds the project and slot to Claude's
context; on `CwdChanged` (Claude `cd`s into another working copy) it swaps the
exported ports for that working copy's slot. Outside a project the command
prints nothing and exits 0, so the hook never fails a session.

```json
{
  "hooks": {
    "SessionStart": [
      { "hooks": [ { "type": "command", "command": "port-keeper hook claude" } ] }
    ],
    "CwdChanged": [
      { "hooks": [ { "type": "command", "command": "port-keeper hook claude" } ] }
    ]
  }
}
```

### Tell the agent the tool exists

The tool only helps if the agent knows it exists. Agents left to themselves
start `python -m http.server 8000` or `vite --port 3000` and trample whatever
was there. Two lines in your **user-level** instructions file (for Claude Code,
`~/.claude/CLAUDE.md`; other agents have an equivalent) are enough to stop
that, because they apply to every project on the machine:

```markdown
## Local ports

Local dev ports on this machine are managed by port-keeper (MCP server `port-keeper`).
Never choose a port number yourself and never start a server on an ad-hoc port.
To find where something runs, call the `resolve_url` tool
(or run `port-keeper url <project>/<slot>/<service>`).
If a task needs a new port, add a service to `port-keeper.toml` and run `port-keeper env`.
Refer to services by name (`shop/3/admin`), never by number, in docs, issues and chat.
```

Why user-level and not per-project: the servers that cause trouble are the
ad-hoc ones an agent starts in a scratch directory or in a project that has
no manifest yet. A project-level file never reaches those.

If you run several agents (Claude Code, Codex, Cursor, …), put the same block
in each one's global instructions. The MCP registration is per client; the
ledger is shared.

## Guarantees

- **No two leases share a port**, across every project and slot on the
  machine, including pinned legacy ports. Enforced by a UNIQUE constraint in
  the ledger and a write transaction around every allocation.
- **Pool only.** Allocation never leaves the configured pool (default
  20000–31999), which avoids the conventional ports, the ports macOS services
  take, and the OS ephemeral ranges. The only way out of the pool is an
  explicit, reasoned `pin`.
- **Stable.** A slot keeps its block until you release it, across reboots.
- **One slot per working copy.** A working copy that has no slot of its own
  is refused the default slot's ports: `env`, `url`, `status` and the MCP
  tools ask you to run `slot new` first (or to pass `--slot 1` if sharing the
  main slot is what you want). That is what keeps a fresh `git worktree add`
  from starting servers on the main working copy's ports.
- **No listener, no daemon.** Every command opens the ledger, does its work,
  and exits. Nothing of port-keeper's is ever reachable over the network.
- **No secrets.** The ledger has no column for passwords, tokens or
  connection strings, and `.env` rendering never writes into a tracked file.

## Non-goals

These are deliberate. Please read this section before opening a feature
request for one of them; the reasoning is the answer, and a request that
argues with the reasoning is far more useful than one that restates the
feature.

### No reverse proxy / named hosts (`http://admin.shop.localhost`)

port-keeper's goal is that you do not have to *think about* port numbers, not
that you never *see* one. Once `port-keeper url shop/5/admin` (or the
`resolve_url` tool) gives you `http://localhost:23417`, the job is done;
`--open` even opens it for you.

A proxy would add a resident process that every slot depends on. When it
stops, every environment becomes unreachable at once, which is a worse failure
mode than any port collision. It needs a privileged port (80/443) on macOS,
drags TLS and WebSocket forwarding into scope, and contradicts the rule that
port-keeper never listens. Finally, browsers scope cookies and local storage
by origin *including the port*, so distinct ports are exactly what keep your
slots' sessions apart; hiding them behind one hostname would remove that
isolation. If you want pretty hostnames anyway, feed
`port-keeper env --format json` to a proxy that already does this well
(portless, localias, devenv). port-keeper will not grow one.

### No process management (start / stop / restart / kill)

port-keeper reserves a number and tells you what it is. Who starts the server
on that number, when, and under which supervisor is the job of your task
runner (`mise`, `just`, `direnv`, `docker compose`, an IDE). port-keeper will
report that a port is in use and by which PID, but it will never kill a
process: a tool that an AI agent can call should not be able to take down
another environment's dev server by mistake or through prompt injection.

### No daemon

Every command opens the SQLite ledger, works inside a write transaction, and
exits. Concurrent CLI or MCP processes cannot double-allocate because SQLite
serialises the writes and the `port` column is UNIQUE. A daemon would buy
pub/sub and TTL leases at the cost of a second access path to the ledger
(socket or HTTP) and one more thing to keep alive. The requirements do not
need it.

### No shared or synced ledger

The ledger lists which services listen on which ports on *your* machine. That
is exactly what an attacker enumerates first, so it stays local, mode 0600,
and is never synced, committed or uploaded. Teams share the manifest, which
has names and templates but no numbers.

### No secrets in the ledger

There is no column for passwords, tokens or connection strings, and there
will not be one. If a feature seems to need a secret, it belongs in your
secret manager.

### No allocation outside the pool

`pin` exists for migrating an existing project and nothing else. It requires a
`--reason`, is limited to one slot per project, and is arbitrated across every
project in the ledger; `doctor` nags you about it until you `unpin`. New
projects should never pin.

### If you still want one of these

Open an issue that starts from the reasoning above and says which part of it
does not hold in your case. "It would be convenient" is already accounted
for; what changes the answer is a failure mode we did not consider, or a case
where the non-goal blocks the actual goal (not thinking about port numbers).

## Security model

port-keeper is a local, non-network tool. The ledger is a map of what listens
where on your machine; it is stored under `~/.local/state/port-keeper/` with
mode 0600 and is never transmitted. Against another user on the same machine
that is sufficient. Against a process running as *you* it is not, and no
local tool can be: such a process can already run `lsof -i`. What port-keeper
does about that is refuse to be a richer map than `lsof` (no secrets, no
tenant names, no descriptions beyond a short label) and refuse to disclose
more than the current project to an agent unless asked explicitly.
`port-keeper doctor` checks the permissions, the `.gitignore`, and that
nothing of port-keeper's is listening. See [SECURITY.md](SECURITY.md) for
the reporting policy and what is in scope.

## Reference

### Manifest (`port-keeper.toml`)

Lives at the repository root and is meant to be committed. It contains names
and templates; the numbers live only in your local ledger.

```toml
[project]
name = "shop"
block_size = 32          # ports per slot; grows to a second block if exceeded
slot_default = "1"       # the slot a fresh checkout resolves to; the only slot that may pin

[[service]]
name = "web"
env = "WEB_PORT"
proto = "http"
label = "Storefront"     # optional, short; cut to 64 chars before it reaches an agent

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
proto = "tcp"            # no URL for tcp services
tier = "infra"           # shareable via `slot new --infra-from`

[[derive]]               # values built from ports, never typed by hand
env = "VITE_API_BASE"
value = "${url.api}"

[[derive]]
env = "ALLOWED_ORIGINS"
value = "${url.web},${url.admin}"

[render]
dotenv_path = ".env.local"   # must be gitignored; `port-keeper env` refuses tracked files
dotenv_marker = "port-keeper" # marker text of the managed block
host = "localhost"           # host used in every URL
```

Template variables: `${port.<service>}`, `${url.<service>}` (http/https
services only), `${slot}`, `${slot.infra}`, `${project}`, `${block.base}`.

### CLI

| Command | Role |
|---|---|
| `init [--name] [--here]` | Write the manifest skeleton and the `.gitignore` entry. `--here` writes into the current directory instead of the git top level (monorepos) |
| `slot new [name] [--infra-from s] [--no-bind]` / `slot ls [--pins]` / `slot rm name [--force] [--cascade]` | Create, list, release slots. `new` binds the current working copy to the slot unless it is already bound to another one |
| `env [--format f] [--stdout] [--if-present]` | Render the current slot; `dotenv` (default) rewrites the marker block in `.env.local`. Formats: `dotenv`, `export`, `json`, `mise`, `direnv`, `claude-env` (alias of `export`) |
| `url <service>` / `url <project>/<slot>/<service>` `[--open]` | Print (or open) one URL |
| `status` | Ledger vs. what is listening |
| `context [--json] [--if-present]` | Project, slot, readiness and guidance for this working copy. No numbers. The client-agnostic contract for agents and shells |
| `gc [--yes]` | List (or release) slots idle for longer than `stale_days`, and slots whose working copy is gone |
| `doctor [--fix]` | Permissions, `.gitignore`, pool sanity, stale slots, pins, no listener of our own |
| `pin <service> <port> --reason t [--force]` or `pin web=3001 api=3002 --reason t` / `unpin <service>…` or `unpin --all` | Migration aid; see Guarantees. The batch form pins a whole legacy layout in one command |
| `reassign <service>` | Move a service to another pooled port (after `status` reports `hijacked`) |
| `mcp` | Serve MCP over stdio |
| `hook claude` | Claude Code adapter for `SessionStart` and `CwdChanged`: `context` + `env` translated into `$CLAUDE_ENV_FILE` and hook JSON; silent outside a project |
| `version` (or `--version`) | Print the version |
| `--slot <name>` | Global flag: act on that slot instead of the resolved one |

A slot is resolved from `--slot`, then the slot bound to the current working
copy, then `PORT_KEEPER_SLOT`, then the manifest's `slot_default`. When a
stale `PORT_KEEPER_SLOT` in the shell disagrees with the binding, the binding
wins and a warning says so.

### MCP tools (8)

| Tool | Role |
|---|---|
| `current_context` | The project and slot resolved from the working directory, and the service names. No numbers (read-only) |
| `resolve_url` | Full URL for one service, e.g. `http://localhost:23417`. Other projects require an explicit `project` argument (read-only) |
| `resolve_port` | The bare port number for one service (read-only) |
| `render_env` | Every env var for the current slot, in the requested format. Leases a port for any service that has none yet (idempotent) |
| `status` | Ledger vs. reality for the current slot: `leased` (nothing listening), `active`, `stale`, or `hijacked`, with the owning PID when known (read-only) |
| `slot_new` | Lease a block for a new slot; returns the existing slot if the name is taken or the working copy is already bound (idempotent) |
| `slot_release` | Release a slot. Refuses while anything is listening. Requires `confirm:true`. **Not registered unless enabled in config** |
| `list_all_projects` | Names of every project and slot in the ledger, no numbers. **Not registered unless enabled in config** |

Every tool accepts an optional `cwd` so a session that moves between working
copies keeps getting the right slot. Only `resolve_url`, `resolve_port` and
`render_env` carry port numbers, and only for what was asked; the other tools
speak in names, so that numbers do not accumulate in agent transcripts.

### Configuration (`~/.config/port-keeper/config.toml`)

All optional:

```toml
stale_days = 30             # top-level key; must come before any [table]

[pool]
ranges = [[20000, 31999]]
deny_ports = [27017, 28015, 29092]

[mcp]
enable_release = false      # set to true to register slot_release
enable_list_all = false     # set to true to register list_all_projects
```

Unknown keys are an error, so a misplaced setting cannot silently do nothing.

## Development

Go 1.25 or newer (the SQLite driver and the MCP SDK require it; `go` downloads
the toolchain automatically when `GOTOOLCHAIN` is left at its default).

```sh
go test ./...                    # unit, property and in-process MCP tests
go vet ./... && gofmt -l .
```

The project uses OpenSpec (`openspec/`) for change proposals; run
`openspec list` to see them.

Package layout: `internal/config` (pool, paths) / `internal/manifest` /
`internal/ledger` (SQLite, allocation) / `internal/probe` (bind checks,
listener discovery) / `internal/gitx` / `internal/render` (dotenv, export,
json, mise, direnv, claude-env) / `internal/app` (resolution, sync, status,
pins) / `internal/cli` (commands; `hook_claude.go` is the Claude Code
adapter) / `internal/mcpserver` (stdio server) / `cmd/port-keeper`.

## Name

A *keeper* holds the keys and the ledger and tells you which door is which;
it does not build the doors or open them for you. The `-mcp` suffix follows
the naming of MCP servers; the binary is just `port-keeper`.

## License

MIT. See [LICENSE](LICENSE).
