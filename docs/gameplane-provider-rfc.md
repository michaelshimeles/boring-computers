# RFC: boring-computers as Gameplane's sole game runtime provider

Status: analysis (no implementation yet) · Branch: `explore/gameplane-provider`
Basis: three-lens research pass (2026 GPU/VMM state of the art, OCI+UDP state of the
art, gap-by-gap codebase mapping with file:line) — sources cited inline.

## Verdict up front

**Achievable — with one honest asterisk.** boring-computers can be the *sole
provider* Gameplane talks to, but not a *sole-substrate* one:

- **G3 (GPU) cannot be done on Firecracker. Full stop.** VFIO/GPU passthrough was
  never merged; the community PCIe effort was formally paused Feb 2025 for lack of
  resources ([discussion #4845](https://github.com/firecracker-microvm/firecracker/discussions/4845),
  tracking [#1179](https://github.com/firecracker-microvm/firecracker/issues/1179)).
  Only the emulated virtio-**pci transport** landed (v1.13+, and it shipped
  CVE-2026-5747). Building on the abandoned `poc/pcie` branch = maintaining a fork
  of a dead POC.
- The right GPU architecture is **not** a heroic VMM swap either. The 2026 consensus
  (Modal, Beam; fly.io by hard experience) is: a passed-through GPU has **DMA access
  to host memory — the VM boundary doesn't survive it anyway**. Fly used Cloud
  Hypervisor for GPU machines, isolated them on dedicated hosts, found them
  under-utilized, and **deprecated the product**
  ([fly.io/blog/wrong-about-gpu](https://fly.io/blog/wrong-about-gpu/)).
- Therefore: **"sole provider" is a control-plane claim, not a hypervisor claim.**
  Gameplane targets only boringd; boringd schedules two workload classes:
  - CPU games → Firecracker microVMs (unchanged posture: jailer, egress firewall,
    snapshot/fork).
  - GPU streaming → a **GPU sled**: dedicated GPU node where boringd launches the
    workload as an NVIDIA-container (nvidia-container-toolkit, optionally
    gVisor+nvproxy for untrusted tenants — arguably *stronger* GPU isolation than a
    VFIO microVM). Cloud Hypervisor + VFIO remains the documented fallback if a VM
    line-item is ever mandatory (weeks of VFIO/IOMMU plumbing; loses snapshot/fork +
    memory overcommit — GPU VFIO drivers don't support migration v2).

If Gameplane's definition of done insists GPU workloads sit inside a microVM, that
is the one requirement this substrate cannot meet without adopting Cloud Hypervisor —
say so now rather than discovering it in week six.

## The three "hard" blockers, ranked easiest-first (opposite of the brief's fear)

### G2 — public TCP/UDP ingress: EASY here (days→week)
Guests already sit on a bridge with per-VM taps and resolvable IPs
(`network.go`, `preview.go:51`). Kernel **DNAT is the *preferred* design**, not a
compromise: netfilter adds tens of µs; a userspace proxy adds 0.1–3ms and jitter —
the wrong direction for game RTT. fly-proxy exists for anycast/multi-region/backhaul
problems a single-box runner doesn't have. Three specific traps the research caught:

1. **FORWARD hole**: `BORING_FWD` does `! -s $CIDR -j RETURN` (net-setup.sh:74) — a
   DNATed inbound packet falls through to the main FORWARD policy. Needs an explicit
   `-d <guestIP> -p tcp/udp --dport <p> -m conntrack --ctstate NEW -j ACCEPT` per
   published port.
2. **Source-port preservation** (the silent killer): FiveM/SA-MP/Source servers
   heartbeat master lists from the same UDP port they serve. Generic MASQUERADE may
   rewrite the source port → server registers wrong/unlisted. Fix: per-VM 1:1 NAT
   (NETMAP) or explicit port-preserving SNAT paired with the DNAT.
3. **conntrack sizing**: server-browser scrapes churn short UDP flows;
   `nf_conntrack_max ≈ 1M`, monitor `conntrack -S`.

Prefer allocating the game's *native* port (25565/7777/30120) when free so
server-list entries match. Port allocator + per-machine port-map on the Machine
record + rule lifecycle in create/teardown.

### G1 — arbitrary OCI images: the fly.io model (weeks)
Reuses ~80% of existing machinery (`build-rootfs.sh` already mkfs+unpacks;
`copyReflink` already gives per-VM CoW):

- **Pull** with skopeo/containerd content store (docker-config auth for private
  registries), cache by digest.
- **Assemble** layers → ext4 honoring whiteouts/opaque dirs (use umoci/containerd
  snapshotter — don't hand-roll tar). Cache the ext4 by digest; reflink per VM →
  subsequent boots as fast as today's templates.
- **Guest pid1** (~200-line static Go binary) replacing busybox inittab: mount
  proc/sys/dev/shm (shmSizeMb) → resolv.conf → setrlimit (ulimits) → setuid (User)
  → chdir (WorkingDir) → print `BORING_READY` (keeps the boot timer) → exec
  Entrypoint+Cmd with merged Env → reap zombies → **translate shutdown into the
  image's StopSignal and wait**.
- Alternatives rejected: firecracker-containerd and kata+fc are control-plane
  *replacements* (they own the VMM lifecycle; we'd discard fcDriver/jailer/snapshot
  code); dockerd-in-guest is a viable escape hatch (hideable behind snapshot/restore)
  but pays a fat-guest boot + per-VM image cache as the primary path.
- Acceptance detail that matters: `itzg/minecraft-server` traps SIGTERM to run an
  RCON `stop` (world save). **Today's `Close()` sends one CtrlAltDel then immediately
  SIGKILLs** (firecracker.go:596-625) — without the pid1 stop-signal path the world
  corrupts. G1 and G4 are one work item.

### G3 — GPU: covered above. Container-on-GPU-sled: days-weeks. CH+VFIO: months.

## The sleeper: G6 is not what it looks like

boringd is a single-process in-memory Manager (`machine.go:96`) — building a
multi-node scheduler inside it is months. **Don't.** The brief itself says Gameplane's
control plane already resolves placement and runners **register with region +
heartbeats**. So invert it: each boringd host is *one runner*. Needed on our side:
node-id + region config, a real capacity endpoint (free slots = MaxMachines − len,
free MB via `availableMemoryMB`, GPU inventory), and the G8 adapter speaking
Gameplane's runner protocol. Placement, quotas, and multi-region stay in Gameplane,
where they already live. Months → days.

## Gap-by-gap effort map (from the codebase lens, file:line verified)

| Gap | Today | Work | Size |
|---|---|---|---|
| G1 OCI | prebuilt ext4 + snapshot templates; zero OCI code | pull/assemble/cache + guest pid1 | **weeks** |
| G2 tcp/udp ingress | HTTP-only preview proxy (preview.go); no PREROUTING rules | DNAT + FORWARD hole + 1:1 NAT + allocator | **days–week** |
| G3 GPU | impossible on FC | GPU sled (container driver) / CH+VFIO fallback | **weeks / months** |
| G4 long-lived + restart | Persistent exists (machine.go:39); **no exit-watcher; Destroy ≈ SIGKILL** | supervisor goroutine + restart policy + graceful stop w/ stopSignal + timeout | **weeks** |
| G5 live volumes | S3 tar snapshots into /root — not live disks | 2nd ext4 drive via `PUT /drives/data` + guest mount + survive-recreate lifecycle (+ `backup` → S3 sync) | **weeks** |
| G6 multi-node | in-memory, /healthz count only | runner registration + capacity endpoint (placement stays in Gameplane) | **days** |
| G7 healthchecks | none; primitives exist (dialGuest files.go:44, exec, DialVsock) | spec + runner + status + restart wiring; UDP needs exec-probe | **weeks** |
| G8 provider API | REST exists, wrong shape | RuntimeRecipe/PortDefinition adapter + heartbeat | **week** |
| G9 presets | cpu/mem/pids are real caps (cgroups.go:74); disk unbounded; gpus n/a | per-machine overlay sizing (days); gpus via G3 driver | **days** |
| G10 env/files/secrets | nothing at create; exec/upload post-boot | pre-boot overlay write (or MMDS — note egress fw blocks 169.254/16 for guests, separable); restore-aware injection | **days–weeks** |
| G11 logs/metrics | 256KB in-memory serial scrollback; cgroup stats unread | metrics endpoint (days); durable log sink (weeks) | **weeks** |
| G12 isolation | jailer + egress fw + guest↔guest DROP — strong | scope DNAT per port, inbound rate caps, port-exhaustion guard | **days** |
| Runtime-driver abstraction | fcDriver concrete at ~17 sites; readdressFork pokes ip/tap/console | interface extraction mechanical (days); genuinely swappable (untangle fork/NIC model) | **weeks** |

## Recommended phasing

- **Phase A — prove the marquee demo (~2–3 wks):** G2 DNAT (+G12 hardening) → G1
  OCI conversion + pid1 → G4 graceful-stop/supervision → G5 live data volume.
  *Acceptance: unmodified `itzg/minecraft-server`, joinable at tcp/25565, world on a
  volume, survives stop/recreate, clean world-save on stop.*
- **Phase B — the provider contract (~2–3 wks):** G7 healthchecks, G9 disk presets,
  G10 injection, G11 logs/metrics, G8 adapter + runner/capacity registration (G6-lite).
  *Acceptance: SA-MP/FiveM/Among Us joinable from real clients (UDP), Gameplane
  creates/stops/recreates via the adapter with zero game-definition changes.*
- **Phase C — GPU sled (~1–2 wks container path):** runtime-driver interface, GPU
  node driver (nvidia-container-toolkit; gVisor+nvproxy for untrusted), Selkies +
  self-hosted coturn (open UDP/TCP 49152–65535; TURN REST credentials).
  *Acceptance: Wine game streams to a browser at playable latency.*

Total: a ~6–8 week program. Dominated by OCI correctness and lifecycle (G4/G5), not
by the things the brief feared most (G2 is easy; G3 is a decision, not a mystery).

## Strategic opinion (the part that isn't engineering)

1. **This is fly.io's shape.** OCI→microVM, ports, volumes, regions — boring-computers
   would become a small self-hosted Fly Machines. Coherent with the substrate; but it
   is a *second product* sharing ~70% of the code with the AI-computers product. The
   risk is focus (solo maintainer), not feasibility.
2. **The two reframes that make it tractable:** (a) sole *provider* ≠ sole
   *substrate* — GPU rides a sled; (b) boringd is a *runner*, not a scheduler —
   placement stays in Gameplane. Accept both and the brief is realistic; insist on
   either purist reading and it isn't.
3. **What would kill it:** GPU-in-microVM as a hard requirement (adopt CH or concede);
   or multi-node scheduling inside boringd (don't).
4. **Keep the products from tangling:** the provider surface should be additive
   (new endpoints + a workload kind), never bending the AI-computer semantics
   (TTL default, warm pool, fork-for-agents stay untouched).
5. Un-researched flag: FiveM/Cfx.re licensing (keymaster) is an operational
   requirement on Gameplane's side, not a runtime gap — but verify before promising
   FiveM in the definition of done.
