#!/usr/bin/env bash
#
# setup-local.sh — run a full boring computers host locally on an Apple Silicon
# Mac, inside a Lima nested-virt Linux VM (which is where /dev/kvm lives).
#
# From the repo root on your Mac:
#   BORING_ANTHROPIC_KEY=sk-ant-...  ./infra/local/setup-local.sh
#
# What it does: ensures the Lima VM, cross-builds boringd for linux/arm64 on the
# Mac, ships the infra scripts + binary into the guest, runs bootstrap + the
# arm64 image builds + networking + boringd there, and forwards port 8080 back to
# the Mac. Then point apps/web/.env at http://localhost:8080 and `npm run dev`.
#
# Options (env): SKIP_DESKTOP=1 (skip the ~8-min desktop image), BORING_TOKEN,
#   BORING_S3_* (persistent volumes), VM=<lima instance name> (default: boring).
#
set -euo pipefail

VM="${VM:-boring}"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
LIMA_YAML="${REPO_ROOT}/infra/local/lima-boring.yaml"

log()  { printf '\033[1;34m[local]\033[0m %s\n' "$*"; }
die()  { printf '\033[1;31m[local:error]\033[0m %s\n' "$*" >&2; exit 1; }
invm() { limactl shell "${VM}" -- sudo bash -c "$*"; }

# --- 0. host preflight -------------------------------------------------------
[ "$(uname -s)" = "Darwin" ] || die "this script is for a Mac host; on Linux use infra/setup.sh directly"
command -v limactl >/dev/null || die "Lima not installed — run: brew install lima"
command -v go >/dev/null || die "Go not installed on the Mac (needed to cross-build boringd) — brew install go"

# --- 1. ensure the Lima nested-virt VM --------------------------------------
if ! limactl list -q 2>/dev/null | grep -qx "${VM}"; then
	log "Creating Lima VM '${VM}' (nested-virt arm64 Ubuntu)…"
	limactl start --name="${VM}" --tty=false "${LIMA_YAML}"
elif [ "$(limactl list --format '{{.Status}}' "${VM}" 2>/dev/null)" != "Running" ]; then
	log "Starting Lima VM '${VM}'…"
	limactl start "${VM}"
else
	log "Lima VM '${VM}' already running."
fi

# --- 2. make-or-break: /dev/kvm in the guest --------------------------------
log "Checking /dev/kvm inside the guest…"
invm 'test -e /dev/kvm' || die "/dev/kvm missing in the guest — nested virtualization isn't working"
GUEST_ARCH="$(limactl shell "${VM}" -- uname -m)"
log "  ok: guest is ${GUEST_ARCH} with /dev/kvm"

# --- 3. cross-build boringd for the guest arch on the Mac --------------------
log "Cross-building boringd for linux/${GUEST_ARCH}…"
GOARCH="arm64"; [ "${GUEST_ARCH}" = "x86_64" ] && GOARCH="amd64"
( cd "${REPO_ROOT}/boringd" && GOOS=linux GOARCH="${GOARCH}" CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /tmp/boringd-local . )
log "  built $(file -b /tmp/boringd-local | cut -d, -f1-2)"

# --- 4. ship infra scripts + boringd binary into the guest ------------------
log "Shipping infra scripts + boringd into the guest…"
invm 'mkdir -p /root/infra /opt/boring/bin'
tar czf - -C "${REPO_ROOT}/infra/latitude" . | limactl shell "${VM}" -- sudo tar xzf - -C /root/infra
limactl shell "${VM}" -- sudo cp /dev/stdin /usr/local/bin/boringd < /tmp/boringd-local
invm 'chmod +x /usr/local/bin/boringd'

# --- 5. build the stack in the guest (arch-adapted scripts auto-detect) ------
log "bootstrap (firecracker + jailer + kernel + base rootfs)…"
invm 'bash /root/infra/bootstrap.sh'
# (bootstrap.sh already calls build-rootfs.sh — no separate invocation needed)
log "python snapshot template (~3ms restore; non-fatal — cold boot works without it)…"
invm 'bash /root/infra/build-template.sh python' || log "  snapshot template unavailable on ${GUEST_ARCH} — python will cold-boot instead"
if [ "${SKIP_DESKTOP:-}" = "1" ]; then
	log "skipping desktop image (SKIP_DESKTOP=1)"
else
	log "desktop image (chromium + node + agents) — a few minutes…"
	invm 'bash /root/infra/build-desktop-rootfs.sh' || log "  desktop image build had issues (python shell still works)"
fi
log "guest networking (bridge + NAT + egress firewall)…"
invm 'install -m0755 /root/infra/net-setup.sh /opt/boring/bin/net-setup.sh && bash /opt/boring/bin/net-setup.sh && cp /root/infra/boring-net.service /etc/systemd/system/ && systemctl daemon-reload && systemctl enable boring-net.service || true'

# --- 6. boringd config + service (bind 0.0.0.0 so Lima can forward it) -------
log "installing boringd service…"
invm "cp /root/infra/boringd.service /etc/systemd/system/boringd.service"
limactl shell "${VM}" -- sudo bash -c "install -d -m0755 /etc/boring && umask 077 && cat > /etc/boring/boringd.env" <<EOF
BORING_ADDR=0.0.0.0:8080
BORING_ALLOW_PERSISTENT=1
BORING_JAILER=1
BORING_NET=1
BORING_TOKEN=${BORING_TOKEN:-}
BORING_ANTHROPIC_KEY=${BORING_ANTHROPIC_KEY:-}
BORING_OPENROUTER_KEY=${BORING_OPENROUTER_KEY:-}
BORING_S3_ENDPOINT=${BORING_S3_ENDPOINT:-}
BORING_S3_KEY=${BORING_S3_KEY:-}
BORING_S3_SECRET=${BORING_S3_SECRET:-}
BORING_S3_BUCKET=${BORING_S3_BUCKET:-boring-volumes}
BORING_S3_REGION=${BORING_S3_REGION:-}
BORING_S3_SSL=${BORING_S3_SSL:-}
EOF
invm 'systemctl daemon-reload && systemctl enable --now boringd && sleep 2 && systemctl is-active boringd'

# --- 7. verify -------------------------------------------------------------
# Read the forwarded host port from the Lima config (single source of truth) so
# the health check always matches the actual forward. Change the port by editing
# hostPort in lima-boring.yaml, not a separate override.
HOST_PORT="$(grep -oE 'hostPort:[[:space:]]*[0-9]+' "${LIMA_YAML}" | grep -oE '[0-9]+' | head -1)"
HOST_PORT="${HOST_PORT:-8088}"
log "Health check (Lima forwards guest :8080 → Mac 127.0.0.1:${HOST_PORT})…"
sleep 2
HEALTH="$(curl -s --max-time 8 http://127.0.0.1:${HOST_PORT}/healthz || true)"
echo "  ${HEALTH}"
# Verify it's actually boringd (its healthz carries "kvm"), not some other local
# service that happens to answer on this port.
echo "${HEALTH}" | grep -q '"kvm"' || die "port ${HOST_PORT} on the Mac isn't boringd (got: ${HEALTH:-nothing}) — is something else bound to it? change hostPort in ${LIMA_YAML} to a free port, then 'limactl stop ${VM}' and re-run."

log "Done. A boring computers host is running on your Mac (in the '${VM}' Lima VM)."
echo "  Point apps/web/.env at it:  BORING_URL=http://localhost:${HOST_PORT} $( [ -n "${BORING_TOKEN:-}" ] && echo "(+ BORING_TOKEN)" )"
echo "  Then:  npm run dev -w web   → http://localhost:5173"
echo "  Stop the VM (frees RAM):  limactl stop ${VM}"
