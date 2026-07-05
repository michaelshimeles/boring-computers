# boring computers

**On-demand Linux computers you can hand to an AI.**

Each one is a real [Firecracker](https://firecracker-microvm.github.io/) microVM —
a full machine with its own kernel — that boots in milliseconds, does its thing,
and self-destructs. **Open source (Apache-2.0), self-hosted with your own keys.**
[boringcomputers.com](https://boringcomputers.com) is a showcase; you run the real
thing yourself.

![A boring computer: a live desktop with a browser and calculator, a terminal with claude/codex/cursor/pi preinstalled, and an AI build box](docs/hero.png)

> One machine, everything on it — a live desktop with a real browser, a terminal
> with coding agents preinstalled, and an AI you can hand the whole thing to.
> Type _"build a snake game"_ and it writes it, runs it, and gives you a link to play.

## What you get

- **A computer** — a full Linux desktop (browser, terminal, apps) over VNC, or a
  fast headless shell.
- **Coding agents preinstalled** — `claude`, `codex`, `cursor`, `pi`, plus node,
  python, git and internet.
- **An AI that drives it** — say what you want. It either uses the screen
  (clicks, browses) or writes + runs code and hands you a **live URL**.
- **Files & ports** — drag files in and out; open any port through the daemon.
- **Fork** — clone a running computer, exact live state and all, in ~35 ms.
- **Storage** — persistent volumes (S3-backed) that outlive a machine.

## Run your own

You need a machine that can run Firecracker: **Ubuntu 24.04 x86_64 with
`/dev/kvm`** (a bare-metal box, or a VM with nested virtualization) that you can
root-SSH into. One command turns it into a running boringd:

```sh
git clone https://github.com/michaelshimeles/boring-computers
cd boring-computers && npm install

# set it up on your box (installs Firecracker, builds the images, runs boringd)
BORING_ANTHROPIC_KEY=sk-ant-...  ./infra/setup.sh root@YOUR_BOX_IP
```

Don't have a box? If you use [Latitude.sh](https://latitude.sh),
[`infra/latitude/provision.sh`](infra/latitude/provision.sh) creates one for you
first. Any other provider works too — just point `setup.sh` at it.

Then run the site against it:

```sh
# apps/web/.env
PUBLIC_BORING_URL=http://YOUR_BOX_IP:8080   # or a tunnel — see apps/web/.env.example
npm run dev -w web
```

`setup.sh` options (env): `BORING_TOKEN` (require auth), `BORING_S3_*`
(persistent volumes), `BIND_LOCALHOST=1` (reach it only via SSH tunnel — most
private), `SKIP_DESKTOP=1` (skip the ~8-min desktop image). Full REST + WebSocket
API in the [docs](https://boringcomputers.com/docs).

**From any AI** — an MCP server ([`packages/mcp`](packages/mcp)) lets Claude
Desktop, Cursor, and other agents spin up and drive your computers as a tool.
There's also a small Effect-native TypeScript client
([`packages/sdk`](packages/sdk)). Both run from source (not on npm).

## How it works

Real hardware-virtualized isolation — a kernel per machine, not a shared
container. Each VM is jailed and resource-capped, restored from a memory
snapshot in ~3 ms, and self-destructs on a TTL. Guests are network-isolated
behind an egress firewall. The control plane is [`boringd/`](boringd) (Go); host
setup is one command ([`infra/setup.sh`](infra/setup.sh)).

## Repo

A [Turborepo](https://turbo.build/repo) monorepo (npm workspaces):

```
apps/web/          the site — SvelteKit
boringd/           the control plane — Go, runs the microVMs
packages/sdk/      @boring/sdk — Effect-native TypeScript client
packages/mcp/      @boring/mcp — MCP server
infra/setup.sh     one-command host setup (any Ubuntu + KVM box)
infra/latitude/    rootfs/kernel/image builds, networking, Caddy, Latitude helpers
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
