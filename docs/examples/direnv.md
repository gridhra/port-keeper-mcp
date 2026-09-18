# direnv

Assumes `port-keeper env` has been run in this working copy, so `.env.local`
contains the slot's variables, and `.env.local` is gitignored. The shared
manifest is in [`README.md`](README.md).

## `.envrc` that reads `.env.local`

`dotenv_if_exists` loads the file when it is there and stays silent when it
is not (a fresh clone before the first `port-keeper env`). `watch_file` makes
direnv re-evaluate as soon as `port-keeper env` or `port-keeper reassign`
rewrites the file, so the shell never keeps a stale number.

```sh
# .envrc (committed; contains no numbers)
dotenv_if_exists .env.local
watch_file .env.local
```

Run `direnv allow` once after creating or editing `.envrc`. From then on,
entering the directory exports `WEB_PORT`, `ADMIN_PORT`, `API_PORT`,
`DB_PORT`, `VITE_API_BASE` and `ALLOWED_ORIGINS`; leaving it unsets them.

## Alternative: ask port-keeper directly

If you do not want a `.env.local` at all, evaluate the `export` format
inside `.envrc`. `--if-present` keeps the command silent (and successful)
outside a port-keeper project, so the same `.envrc` snippet can live in a
global `~/.envrc` or a shared template.

```sh
# .envrc
eval "$(port-keeper env --format export --if-present)"
```

Note the difference: this route resolves the slot every time direnv
evaluates (working-copy binding, then `PORT_KEEPER_SLOT`), while the
`.env.local` route reflects whatever the last `port-keeper env` wrote. Both
give identical values for a bound working copy; pick the first when other
tools (Compose, Vite) also read `.env.local`.
