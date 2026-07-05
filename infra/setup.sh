#!/usr/bin/env bash
#
# setup.sh — turn a fresh Ubuntu 24.04 x86_64 box with /dev/kvm into a running
# boringd (the boring computers control plane), end to end, from your laptop.
#
# Provider-agnostic: works on any such box you can root-SSH into (Latitude,
# Hetzner, a bare-metal, a nested-virt VM, …). Idempotent — safe to re-run.
#
#   BORING_ANTHROPIC_KEY=sk-ant-... ./infra/setup.sh root@YOUR_BOX_IP
#
# Options (env vars):
#   BORING_ANTHROPIC_KEY   powers the AI agents + inference gateway (recommended)
#   BORING_TOKEN           require this bearer token on /v1/* (recommended if the
#                          endpoint is public; omit only behind an SSH tunnel)
#   BORING_S3_ENDPOINT     S3 host for persistent volumes (+ _KEY/_SECRET/_BUCKET/
#                          _REGION/_SSL) — optional; omit to disable storage
#   SKIP_DESKTOP=1         skip the ~8-min desktop image build (browser + agents)
#   BIND_LOCALHOST=1       bind boringd to 127.0.0.1 (reach it only via SSH tunnel)
#
set -euo pipefail

TARGET="${1:-}"
if [[ -z "${TARGET}" || "${TARGET}" == "-h" || "${TARGET}" == "--help" ]]; then
	grep -E '^#( |$)' "$0" | sed 's/^# \{0,1\}//'
	exit 0
fi

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SSH=(ssh -o ConnectTimeout=20 -o StrictHostKeyChecking=accept-new "${TARGET}")
GO_VERSION="1.25.0"

log() { printf '\033[1;34m[setup]\033[0m %s\n' "$*"; }
die() { printf '\033[1;31m[setup:error]\033[0m %s\n' "$*" >&2; exit 1; }

# --- 0. preflight ------------------------------------------------------------
log "Preflight on ${TARGET}…"
"${SSH[@]}" 'true' || die "can't SSH to ${TARGET}"
eval "$("${SSH[@]}" 'echo ARCH=$(uname -m) KVM=$([ -e /dev/kvm ] && echo yes || echo no) ID=$(. /etc/os-release; echo $VERSION_ID)')"
[[ "${ARCH}" == "x86_64" ]] || die "box arch is ${ARCH}; boringd needs x86_64"
[[ "${KVM}" == "yes" ]] || die "/dev/kvm missing — the box needs hardware/nested virtualization"
log "  ok: Ubuntu ${ID:-?} x86_64 with /dev/kvm"

# --- 1. ship infra scripts + boringd source ----------------------------------
log "Copying infra scripts + boringd source…"
"${SSH[@]}" 'mkdir -p /root/infra /opt/boring/src'
scp -q -o StrictHostKeyChecking=accept-new \
	"${REPO_ROOT}"/infra/latitude/*.sh "${REPO_ROOT}"/infra/latitude/*.service \
	"${REPO_ROOT}"/infra/latitude/Caddyfile "${TARGET}:/root/infra/"
rsync -az --delete -e "ssh -o StrictHostKeyChecking=accept-new" \
	--exclude '*_test.go' "${REPO_ROOT}/boringd/" "${TARGET}:/opt/boring/src/"

# --- 2. install Go (matching go.mod) -----------------------------------------
log "Ensuring Go ${GO_VERSION}…"
"${SSH[@]}" bash -euo pipefail <<EOF
if ! /usr/local/go/bin/go version 2>/dev/null | grep -q "go${GO_VERSION}"; then
  curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" -o /tmp/go.tgz
  rm -rf /usr/local/go && tar -C /usr/local -xzf /tmp/go.tgz && rm -f /tmp/go.tgz
fi
/usr/local/go/bin/go version
EOF

# --- 3. bootstrap: firecracker, jailer, kernel, base rootfs ------------------
log "Bootstrap (firecracker + jailer + kernel + base rootfs)…"
"${SSH[@]}" 'bash /root/infra/bootstrap.sh'

# --- 4. build the guest images + snapshot ------------------------------------
log "Building base rootfs (python + node + claude)…"
"${SSH[@]}" 'bash /root/infra/build-rootfs.sh'
log "Building the python snapshot template (~3ms restore)…"
"${SSH[@]}" 'bash /root/infra/build-template.sh python'
if [[ "${SKIP_DESKTOP:-}" == "1" ]]; then
	log "Skipping the desktop image (SKIP_DESKTOP=1)."
else
	log "Building the desktop image (chromium + node + coding agents) — a few minutes…"
	"${SSH[@]}" 'bash /root/infra/build-desktop-rootfs.sh'
fi

# --- 5. guest networking (bridge + NAT + egress firewall) --------------------
log "Setting up guest networking…"
"${SSH[@]}" bash -euo pipefail <<'EOF'
install -m0755 /root/infra/net-setup.sh /opt/boring/bin/net-setup.sh
bash /opt/boring/bin/net-setup.sh
cp /root/infra/boring-net.service /etc/systemd/system/ 2>/dev/null || true
systemctl daemon-reload && systemctl enable boring-net.service 2>/dev/null || true
EOF

# --- 6. build + install boringd, write env, start ----------------------------
log "Building + installing boringd…"
"${SSH[@]}" bash -euo pipefail <<'EOF'
cd /opt/boring/src
CGO_ENABLED=0 /usr/local/go/bin/go build -trimpath -ldflags="-s -w" -o /usr/local/bin/boringd .
cp /root/infra/boringd.service /etc/systemd/system/boringd.service
EOF

log "Writing config (secrets not printed)…"
ADDR="0.0.0.0:8080"; [[ "${BIND_LOCALHOST:-}" == "1" ]] && ADDR="127.0.0.1:8080"
"${SSH[@]}" "install -d -m0755 /etc/boring && umask 077 && cat > /etc/boring/boringd.env" <<EOF
BORING_ADDR=${ADDR}
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

"${SSH[@]}" 'systemctl daemon-reload && systemctl enable --now boringd && sleep 2 && systemctl is-active boringd'

# --- 7. verify ---------------------------------------------------------------
log "Health check…"
HEALTH="$("${SSH[@]}" 'curl -s --max-time 8 http://127.0.0.1:8080/healthz' || true)"
echo "  ${HEALTH}"
echo "${HEALTH}" | grep -q '"ok":true' || die "boringd is up but /healthz didn't return ok — check: ssh ${TARGET} journalctl -u boringd"

log "Done. boringd is running on ${TARGET}."
if [[ "${BIND_LOCALHOST:-}" == "1" ]]; then
	echo "  It's bound to localhost — reach it with:  ssh -N -L 8080:localhost:8080 ${TARGET}"
	echo "  then set apps/web/.env: BORING_URL=http://localhost:8080 (+ BORING_TOKEN) and npm run dev -w web"
else
	echo "  Point the site at it: PUBLIC_BORING_URL=http://<box-ip>:8080 (put it behind TLS for production)."
fi
