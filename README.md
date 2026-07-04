# boring computers

**On-demand Linux computers you can hand to an AI.**

Each one is a real [Firecracker](https://firecracker-microvm.github.io/) microVM —
a full machine with its own kernel — that boots in milliseconds, does its thing,
and self-destructs. Live at **[boringcomputers.com](https://boringcomputers.com)**.

## What you get

- **A computer** — a full Linux desktop (browser, terminal, apps) streamed to
  your browser, or a fast headless shell.
- **Coding agents preinstalled** — `claude`, `codex`, `cursor`, `pi`, plus node,
  python, git and internet.
- **An AI that drives it** — say what you want. It either uses the screen
  (clicks, browses) or writes + runs code and hands you a **live URL**.
- **Files & ports** — drag files in and out; expose any port at a public HTTPS
  URL.
- **Fork** — clone a running computer, exact live state and all, in ~35 ms.

## Use it

**In the browser** — [boringcomputers.com](https://boringcomputers.com): press
_Launch_ and you have a computer.

**From the API** — a plain REST + WebSocket endpoint, no key required:

```sh
curl -X POST https://162-43-188-89.sslip.io/v1/machines \
  -H 'content-type: application/json' \
  -d '{"template":"python","ttl_seconds":60}'
```

Full REST + WebSocket API in the [docs](https://boringcomputers.com/docs).

**From any AI** — an MCP server ([`packages/mcp`](packages/mcp)) lets Claude
Desktop, Cursor, and other agents spin up and drive computers as a tool. Run it
from source (see its readme).

> A TypeScript client ([`packages/sdk`](packages/sdk)) and the MCP server live in
> this repo but aren't published to npm yet — use them from source.

## How it works

Real hardware-virtualized isolation — a kernel per machine, not a shared
container. Each VM is jailed and resource-capped, restored from a memory
snapshot in ~3 ms, and self-destructs on a TTL. Guests are network-isolated
behind an egress firewall. The control plane is [`boringd/`](boringd) (Go); the
host setup lives in [`infra/latitude/`](infra/latitude).

## Repo

A [Turborepo](https://turbo.build/repo) monorepo (npm workspaces):

```
apps/web/          the site — SvelteKit
boringd/           the control plane — Go, runs the microVMs
packages/sdk/      @boring/sdk — TypeScript client
packages/mcp/      @boring/mcp — MCP server
infra/latitude/    host setup — rootfs, kernel, networking, Caddy
```

```sh
npm install      # all workspaces
npm run dev      # the site
npm run build    # production build
npm run check    # type-check
npm run lint     # prettier + eslint
```

## Contributing & license

Contributions welcome — see [CONTRIBUTING.md](CONTRIBUTING.md). Licensed under
[Apache 2.0](LICENSE).
