# Vite

Assumes `port-keeper env` has been run in this working copy, so `.env.local`
contains the slot's variables, and `.env.local` is gitignored. The shared
manifest is in [`README.md`](README.md).

## `vite.config.ts`

Vite reads `.env.local` on its own, but only exposes `VITE_`-prefixed
variables to the config by default. `loadEnv` with an empty prefix returns
every variable, so the config can see `WEB_PORT` while the browser bundle
still sees only `VITE_API_BASE`.

```ts
import { defineConfig, loadEnv } from "vite";

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), "");
  return {
    server: {
      port: Number(env.WEB_PORT),
      strictPort: true, // fail loudly instead of falling back to a guessed port
      proxy: {
        "/api": { target: env.VITE_API_BASE, changeOrigin: true },
      },
    },
  };
});
```

`strictPort: true` matters: Vite's default is to try the next free number
when the requested one is busy, which silently puts the storefront on an
ad-hoc port that no other slot, teammate or agent can predict. With
`strictPort`, a busy `WEB_PORT` is an error, and `port-keeper status` tells
you who is listening there.

## Client code

The browser bundle reads the derived API base; it never sees `API_PORT`.

```ts
const res = await fetch(`${import.meta.env.VITE_API_BASE}/health`);
```

Because `VITE_API_BASE` is derived from `${url.api}` in the manifest, the
same source file works in every slot: slot 1's bundle talks to slot 1's API,
the hotfix slot's bundle to the hotfix API.
