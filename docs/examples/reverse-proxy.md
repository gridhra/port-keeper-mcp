# Named hosts through an external proxy

Assumes `port-keeper env` has been run in this working copy, so `.env.local`
contains the slot's variables, and `.env.local` is gitignored. The shared
manifest is in [`README.md`](README.md).

port-keeper deliberately has no reverse proxy and will not grow one; see
[No reverse proxy / named hosts](../../README.md#no-reverse-proxy--named-hosts-httpadminshoplocalhost)
in the main README. If you want `admin.shop-3.test` anyway, feed the `json`
output to a tool that already does this well. The proxy's config contains
numbers, so it is generated and gitignored, never committed.

## localias

localias reads a `.localias.yaml` map of `alias: port`. Generate one entry
per http service (tcp services have no `url` and are skipped), then run the
proxy from the same directory:

```sh
port-keeper env --format json | jq -r '
  .project as $p | .slot as $s
  | .services | to_entries[]
  | select(.value.url)
  | "\(.key).\($p)-\($s).test: \(.value.port)"' > .localias.yaml
localias run
```

The alias embeds the slot name, so slot 1's admin and slot 3's admin stay
different origins. For one alias without a file, load the env first:
`localias set admin.shop-3.test "$ADMIN_PORT"`.

## portless

After loading the env (mise, direnv, or `eval "$(port-keeper env --format
export)"`), alias a running service, or let portless start the process while
telling it which port the app listens on:

```sh
portless alias "admin-shop-3" "$ADMIN_PORT"
portless run --app-port "$WEB_PORT" -- vite
```

`--app-port` matters: without it portless assigns its own random port, which
is exactly the ad-hoc port port-keeper exists to avoid.

## Any other proxy

Print `service`, `port` and `url` (empty for tcp) as tab-separated lines and
map them into whatever format your proxy wants:
`port-keeper env --format json | jq -r '.services | to_entries[] |
"\(.key)\t\(.value.port)\t\(.value.url // "")"'`.
