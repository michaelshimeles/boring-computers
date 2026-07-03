package main

import (
	"os"
	"strconv"
)

// Config holds all runtime configuration for boringd. Values come from env with
// the fixed defaults described in the boring-computers contract.
type Config struct {
	// Listen address for the HTTP/WS server.
	Addr string

	// Token, if non-empty, requires "Authorization: Bearer <token>" on /v1/*
	// routes (and ?token= on the WebSocket route). /healthz is always open.
	Token string

	// CORSOrigin is sent as Access-Control-Allow-Origin so a browser on another
	// origin (the deployed site) can call the public endpoint. "" disables CORS.
	CORSOrigin string

	// MaxMachines caps the number of live machines; creation returns 429 when full.
	MaxMachines int

	// Fixed host paths (created by bootstrap).
	FirecrackerBin string // /opt/boring/bin/firecracker
	KernelPath     string // /opt/boring/kernel/vmlinux
	BaseRootfs     string // /opt/boring/rootfs/rootfs.ext4
	DesktopRootfs  string // /opt/boring/rootfs/desktop.ext4
	TemplatesDir   string // /opt/boring/templates
	RunDir         string // /opt/boring/run

	// TTL clamp bounds (seconds) and default.
	DefaultTTL int
	MinTTL     int
	MaxTTL     int

	// Guest machine sizing.
	VCPUs     int
	MemSizeMB int

	// Public-facing abuse controls.
	PerIPMax         int  // max concurrent machines per client IP
	CreateRatePerMin int  // max creations per minute per client IP
	TrustProxy       bool // read client IP from X-Forwarded-For (behind Caddy)

	// Per-VM cgroup v2 caps (0 disables that limit).
	CgroupEnable  bool
	CPUMaxPercent int // host CPU % cap per VM (e.g. 100 = 1 core)
	PidsMax       int // max host-visible pids for the firecracker child

	// Guest internet: attach a NIC per cold-booted VM, NAT out via the host. The
	// host side (bridge, dnsmasq, egress firewall) is set up by net-setup.sh.
	NetEnable bool   // BORING_NET=="1"
	NetBridge string // bridge to attach taps to (default boring0)

	// Inference gateway: an OpenAI-compatible /v1/chat/completions that routes
	// Claude models to Anthropic natively and everything else to OpenRouter.
	// Enabled when either key is set. Both may be set at once.
	OpenRouterKey       string // BORING_OPENROUTER_KEY
	InferenceMaxTokens  int    // hard cap on max_tokens per request (cost guard)
	InferenceRatePerMin int    // per-IP requests/min (cost guard)

	// Computer-use agent: an AI driving the GUI desktop, streamed to the browser.
	// AnthropicKey also backs the gateway's Claude path.
	AnthropicKey       string // BORING_ANTHROPIC_KEY; empty disables the agent
	AgentModel         string // model id (default claude-opus-4-8)
	AgentMaxSteps      int    // hard cap on model turns per run (cost guard)
	AgentMaxConcurrent int    // hard cap on simultaneous agent runs (cost guard)

	// Jailer: run firecracker chrooted + unprivileged (defense-in-depth).
	JailerEnable bool
	JailerBin    string // /opt/boring/bin/jailer
	JailerUID    int
	JailerGID    int
	ChrootBase   string // /srv/jailer
}

// LoadConfig builds a Config from the environment, applying the fixed defaults.
func LoadConfig() Config {
	c := Config{
		Addr:                envStr("BORING_ADDR", "0.0.0.0:8080"),
		Token:               os.Getenv("BORING_TOKEN"),
		CORSOrigin:          envStr("BORING_CORS_ORIGIN", "*"),
		MaxMachines:         envInt("BORING_MAX", 20),
		FirecrackerBin:      envStr("BORING_FIRECRACKER_BIN", "/opt/boring/bin/firecracker"),
		KernelPath:          envStr("BORING_KERNEL", "/opt/boring/kernel/vmlinux"),
		BaseRootfs:          envStr("BORING_ROOTFS", "/opt/boring/rootfs/rootfs.ext4"),
		DesktopRootfs:       envStr("BORING_DESKTOP_ROOTFS", "/opt/boring/rootfs/desktop.ext4"),
		TemplatesDir:        envStr("BORING_TEMPLATES", "/opt/boring/templates"),
		RunDir:              envStr("BORING_RUN", "/opt/boring/run"),
		DefaultTTL:          120,
		MinTTL:              15,
		MaxTTL:              900,
		VCPUs:               1,
		MemSizeMB:           256,
		PerIPMax:            envInt("BORING_PER_IP_MAX", 2),
		CreateRatePerMin:    envInt("BORING_CREATE_RATE", 8),
		TrustProxy:          os.Getenv("BORING_TRUST_PROXY") == "1",
		CgroupEnable:        os.Getenv("BORING_CGROUP") != "0",
		CPUMaxPercent:       envInt("BORING_CPU_MAX_PCT", 150),
		PidsMax:             envInt("BORING_PIDS_MAX", 512),
		NetEnable:           os.Getenv("BORING_NET") == "1",
		NetBridge:           envStr("BORING_NET_BRIDGE", "boring0"),
		OpenRouterKey:       os.Getenv("BORING_OPENROUTER_KEY"),
		InferenceMaxTokens:  envInt("BORING_INFER_MAX_TOKENS", 1024),
		InferenceRatePerMin: envInt("BORING_INFER_RATE", 20),
		AnthropicKey:        os.Getenv("BORING_ANTHROPIC_KEY"),
		AgentModel:          envStr("BORING_AGENT_MODEL", "claude-opus-4-8"),
		AgentMaxSteps:       envInt("BORING_AGENT_MAX_STEPS", 12),
		AgentMaxConcurrent:  envInt("BORING_AGENT_MAX_CONCURRENT", 2),
		JailerEnable:        os.Getenv("BORING_JAILER") == "1",
		JailerBin:           envStr("BORING_JAILER_BIN", "/opt/boring/bin/jailer"),
		JailerUID:           envInt("BORING_JAILER_UID", 30000),
		JailerGID:           envInt("BORING_JAILER_GID", 991),
		ChrootBase:          envStr("BORING_CHROOT_BASE", "/srv/jailer"),
	}
	if c.MaxMachines < 1 {
		c.MaxMachines = 1
	}
	return c
}

// ClampTTL applies the default when ttl <= 0 and clamps into [MinTTL, MaxTTL].
func (c Config) ClampTTL(ttl int) int {
	if ttl <= 0 {
		ttl = c.DefaultTTL
	}
	if ttl < c.MinTTL {
		ttl = c.MinTTL
	}
	if ttl > c.MaxTTL {
		ttl = c.MaxTTL
	}
	return ttl
}

func envStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
