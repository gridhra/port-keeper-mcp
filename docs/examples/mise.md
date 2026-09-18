# mise

Assumes `port-keeper env` has been run in this working copy, so `.env.local`
contains the slot's variables, and `.env.local` is gitignored. The shared
manifest is in [`README.md`](README.md).

## Committed `mise.toml`

mise loads a dotenv file with `_.file` under `[env]` (see the mise
documentation on environment variables). Commit this file: it names the file,
never a number, so every slot and every teammate uses the same `mise.toml`.

```toml
[env]
_.file = ".env.local"

[tasks.dev]
run = "vite"        # no --port flag: Vite reads WEB_PORT (see vite.md)

[tasks.api]
run = "go run ./cmd/api"   # the server reads API_PORT from its environment
```

`mise run dev` now starts the storefront on the slot's `WEB_PORT`. After
`port-keeper slot new hotfix` in another working copy, the same `mise.toml`
there resolves to that slot's block, because only `.env.local` differs.

## Alternative: a generated `mise.local.toml`

If you would rather not depend on `.env.local`, render the `mise` format
directly. The output is a `[env]` table with one line per variable, and it
contains the numbers, so it must be gitignored (mise ignores
`mise.local.toml` by convention; add it to `.gitignore` yourself).

```sh
port-keeper env --format mise > mise.local.toml
```

Re-run it after `port-keeper reassign` or after switching slots. The
`.env.local` route needs no such step, because `port-keeper env` rewrites the
marker block in place and mise reads the file on every shell prompt.
