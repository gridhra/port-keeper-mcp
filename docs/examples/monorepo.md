# Monorepos

Assumes `port-keeper env` has been run in this working copy, so `.env.local`
contains the slot's variables, and `.env.local` is gitignored. The shared
manifest is in [`README.md`](README.md); this page is about where that
manifest lives when one repository holds several apps.

## `init --here`

`port-keeper init` writes `port-keeper.toml` at the git top level. In a
monorepo you may want it deeper: `port-keeper init --here --name shop-web`
writes it into the current directory instead. Every command then finds the
manifest by walking up from the working directory, and `.env.local` is
written next to the manifest, so `apps/web/.env.local` belongs to
`apps/web/port-keeper.toml`.

```sh
cd apps/web && port-keeper init --here --name shop-web
cd ../api  && port-keeper init --here --name shop-api
```

## One manifest per app, or one at the root?

**Per app** (`shop-web`, `shop-api`, each with its own services and slots):
apps are leased independently, and a working copy can be bound to a slot of
each project at the same time, because bindings are per project. Choose this
when the apps are really separate products that happen to share a repo.

**One root manifest** (recommended when apps share `db`): a single `shop`
project lists `web`, `admin`, `api` and `db`. One `slot new` leases all of
them together, `${url.api}` is available to `web`'s derives, and there is
exactly one `.env.local` for tools like mise, direnv and Compose to read.
Two manifests cannot reference each other's services, so a shared database
is the deciding factor.

## Sharing infrastructure between slots

With the root manifest, tag the shared services and create later slots from
the first one. Services tagged `tier = "infra"` resolve to the source slot's
ports; everything else gets its own:

```sh
port-keeper slot new 2 --infra-from 1   # slot 2 reuses slot 1's db
```

`DB_PORT` in slot 2 then equals slot 1's, while `WEB_PORT`, `ADMIN_PORT`,
`API_PORT` and every `${url.*}` derive are slot 2's own. The `${slot.infra}`
template variable names the slot that infrastructure came from, which is
handy for a derive such as a database name.
