# Playwright

Assumes `port-keeper env` has been run in this working copy, so `.env.local`
contains the slot's variables, and `.env.local` is gitignored. The shared
manifest is in [`README.md`](README.md), plus one extra derive: the
storefront URL, so the test config never assembles `http://localhost:` + a
number by hand.

```toml
[[derive]]
env = "WEB_URL"
value = "${url.web}"
```

## `playwright.config.ts`

`baseURL` makes `page.goto("/")` relative to the slot's storefront.
`webServer` starts the dev server if nothing answers at `WEB_URL` yet, and
`reuseExistingServer` skips the start when it is already running (for
example under `mise run dev` in another terminal).

```ts
import { defineConfig } from "@playwright/test";

export default defineConfig({
  use: { baseURL: process.env.WEB_URL },
  webServer: {
    command: "npm run dev",          // Vite reads WEB_PORT; see vite.md
    url: process.env.WEB_URL,
    reuseExistingServer: true,
  },
});
```

## Getting the variables into Playwright

Playwright does not read `.env.local` itself. Run it in a shell where the
variables are already exported: under mise or direnv (see
[`mise.md`](mise.md), [`direnv.md`](direnv.md)), or load them inline for a
one-off run with `eval "$(port-keeper env --format export)" && npx playwright
test`. If `WEB_URL` is unset, `baseURL` is `undefined` and relative `goto`
calls fail; that is the intended failure, not a fallback.

## Two slots, two suites, at the same time

Each working copy is bound to its own slot, so slot 1 and the hotfix slot
have different `WEB_URL`. Running `npx playwright test` in both directories
at once starts two dev servers and drives two independent browsers; neither
suite can reach the other's storefront, because the origins differ by port.
