# Examples: wiring port-keeper into everyday tools

Every example assumes the same thing: `port-keeper env` has been run in the
working copy, so `.env.local` holds the current slot's variables, and
`.env.local` is gitignored. The tools below only read that file, or the
`export` / `json` output of the same command.

Public documentation is English; the three READMEs are the only translated
documents, so these examples are not mirrored.

Rule for this directory: no example ever contains a port number. Names and
env variables only, so the same file works in every slot.

| File | Tool | What it shows |
|---|---|---|
| [`mise.md`](mise.md) | mise | Load `.env.local` from a committed `mise.toml`; a `dev` task with no port flag |
| [`direnv.md`](direnv.md) | direnv | `.envrc` that reads `.env.local` and re-evaluates when it changes |
| [`docker-compose.md`](docker-compose.md) | Docker Compose | Publish the database on the slot's port; one Compose project per slot |
| [`vite.md`](vite.md) | Vite | Dev server on `WEB_PORT` with `strictPort`; proxy to `VITE_API_BASE` |
| [`playwright.md`](playwright.md) | Playwright | `baseURL` and `webServer` from a derived `WEB_URL`; two slots, two suites |
| [`reverse-proxy.md`](reverse-proxy.md) | localias, portless | Feed `env --format json` to a proxy that gives you named hosts |
| [`monorepo.md`](monorepo.md) | port-keeper itself | `init --here`, one manifest vs. one per app, sharing `db` across slots |

## The manifest every example uses

The `shop` project from the main README (`[project]` and `[render]` omitted):
three http services, one tcp service tagged as infrastructure, two derives.

```toml
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
tier = "infra"          # shareable via `slot new --infra-from`

[[derive]]
env = "VITE_API_BASE"
value = "${url.api}"
[[derive]]
env = "ALLOWED_ORIGINS"
value = "${url.web},${url.admin}"
```

Some examples add one more `[[derive]]` entry; each file shows its own.
