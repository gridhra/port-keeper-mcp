# Security Policy

## Supported Versions

port-keeper-mcp is pre-1.0. Only the **latest published 0.x release** is
supported. Fixes ship in a new patch or minor release; there are no backports.

| Version           | Supported |
| ----------------- | --------- |
| latest 0.x        | Yes       |
| any older release | No        |

Check your version with `port-keeper --version`.

## Reporting a Vulnerability

**Please do not open a public issue for a security problem.**

Report privately through GitHub Private Vulnerability Reporting:

1. Go to <https://github.com/gridhra/port-keeper-mcp/security/advisories/new>
   (or: repository → **Security** tab → **Report a vulnerability**).
2. Include the port-keeper version, install channel, OS and architecture,
   plus the minimal manifest (`port-keeper.toml`) and command sequence that
   reproduce the problem. Do **not** attach your real ledger file; describe
   its relevant rows instead.

### Response expectation

Single-developer project, best effort: an acknowledgement within about
7 days, a first assessment within about 30 days, a fix as soon as practical
after confirmation, with credit in the advisory unless you ask otherwise.
These are goals, not guarantees; nudge in the advisory thread after 30 days.

### Coordinated disclosure

Please allow a reasonable window before public disclosure; 90 days is the
default assumption. A GitHub Security Advisory and a release note accompany
the fix.

## Scope

port-keeper is a local, non-network tool. It reads and writes one SQLite
file under the user's state directory, renders environment files inside a
repository the user points it at, probes loopback ports by attempting to
bind them, and speaks MCP over stdio. It makes no outbound network requests
and opens no listener.

### In scope

- **Ledger confidentiality.** Any code path that creates the ledger
  directory or file with permissions wider than 0700/0600, leaves them wide
  after `doctor --fix`, or writes ledger contents (port numbers, slot names,
  worktree paths) anywhere other than the ledger: logs, temp files, crash
  output, MCP responses where they were not requested.
- **Disclosure beyond the current project.** An MCP tool returning another
  project's ports, paths or slot names without an explicit `project`
  argument, or `current_context` / `list_all_projects` returning numbers.
- **Render path handling.** Any way for `port-keeper env` to write outside
  the repository root, through a symlink, or into a git-tracked file.
- **Unexpected listener.** Any build or command of port-keeper that opens a
  TCP or Unix socket listener (the bind probe must close immediately and
  never accept).
- **Pool and arbitration bypass.** Allocation outside the configured pool
  without `pin`, or two live leases (pooled or pinned, any project) sharing a
  port.
- **Listener command names.** `status` reports the command name of the
  process found on a port. That name is chosen by the process (`argv[0]`),
  so it is capped and returned only as a labelled field; any path by which it
  reaches an agent unlabelled or uncapped is in scope.
- **Manifest as an injection vector.** A `port-keeper.toml` from a cloned
  repository whose contents reach an agent as anything other than
  length-limited field values (for example, a `label` that escapes its JSON
  field in a tool result).
- **Process interference.** Any code path that signals, kills or otherwise
  interferes with a process that owns a port.

### Out of scope

- Anything requiring the attacker to already run code as the same user, or
  to have write access to the ledger directory or the repository. Such an
  attacker can run `lsof -i`; port-keeper deliberately stores nothing beyond
  what `lsof` would reveal, plus names.
- Vulnerabilities in an MCP client, an LLM's choice of tool arguments, or
  prompt injection that leads a model to call `slot_new` or `slot_release`
  when the user did not intend it. port-keeper limits the blast radius (no
  kill, no out-of-pool allocation, destructive tools unregistered by default,
  `confirm:true` required), but cannot decide what the client asks.
- Port collisions with processes that are not in the ledger (Docker, an IDE,
  the OS). These are correctness issues and are reported as `hijacked`; file
  them as normal bugs.
- Denial of service through very large manifests or ledgers. Report them as
  performance issues.
- Advisories in dependencies not reachable from port-keeper's code paths.
  These are tracked in CI (`govulncheck`) and updated on the normal cadence.
