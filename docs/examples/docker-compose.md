# Docker Compose

Assumes `port-keeper env` has been run in this working copy, so `.env.local`
contains the slot's variables, and `.env.local` is gitignored. The shared
manifest is in [`README.md`](README.md), plus one extra derive shown below.

## Publish the database on the slot's port

The host side of `ports:` comes from `DB_PORT`. The container side stays at
PostgreSQL's fixed internal port; that number lives inside the container's
own network namespace, not on the host, so it is identical in every slot and
never collides with anything.

```yaml
# compose.yaml (committed; the only number is container-internal)
services:
  db:
    image: postgres:16
    ports:
      - "${DB_PORT}:5432"
    environment:
      POSTGRES_PASSWORD: dev   # local only; port-keeper keeps no secrets
```

`${DB_PORT}` in `compose.yaml` is interpolated from the shell environment or
from `--env-file`, **not** from a service's `env_file:` (that one only feeds
the container). So either point Compose at the rendered file, or run it in a
shell where mise or direnv already loaded `.env.local`:

- `docker compose --env-file .env.local up`
- `COMPOSE_ENV_FILES=.env.local docker compose up`
- `mise exec -- docker compose up`, or plain `docker compose up` inside a
  direnv-enabled shell (see [`mise.md`](mise.md), [`direnv.md`](direnv.md))

## One Compose project per slot

Compose names containers after its project name, which defaults to the
directory name. A second slot checked out under the same directory name would
reuse, and fight over, the first slot's `db` container. Derive the project
name from the slot instead:

```toml
[[derive]]
env = "COMPOSE_PROJECT_NAME"
value = "${project}-${slot}"
```

Slot `1` now runs `shop-1-db-1` and slot `hotfix` runs `shop-hotfix-db-1`,
each published on its own `DB_PORT`. If the hotfix slot was created with
`--infra-from 1`, skip `docker compose up` there: its `DB_PORT` already
points at slot 1's database.
