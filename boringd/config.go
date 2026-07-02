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
}

// LoadConfig builds a Config from the environment, applying the fixed defaults.
func LoadConfig() Config {
	c := Config{
		Addr:             "0.0.0.0:8080",
		Token:            os.Getenv("BORING_TOKEN"),
		MaxMachines:      envInt("BORING_MAX", 20),
		FirecrackerBin:   envStr("BORING_FIRECRACKER_BIN", "/opt/boring/bin/firecracker"),
		KernelPath:       envStr("BORING_KERNEL", "/opt/boring/kernel/vmlinux"),
		BaseRootfs:       envStr("BORING_ROOTFS", "/opt/boring/rootfs/rootfs.ext4"),
		DesktopRootfs:    envStr("BORING_DESKTOP_ROOTFS", "/opt/boring/rootfs/desktop.ext4"),
		TemplatesDir:     envStr("BORING_TEMPLATES", "/opt/boring/templates"),
		RunDir:           envStr("BORING_RUN", "/opt/boring/run"),
		DefaultTTL:       120,
		MinTTL:           15,
		MaxTTL:           900,
		VCPUs:            1,
		MemSizeMB:        256,
		PerIPMax:         envInt("BORING_PER_IP_MAX", 2),
		CreateRatePerMin: envInt("BORING_CREATE_RATE", 8),
		TrustProxy:       os.Getenv("BORING_TRUST_PROXY") == "1",
		CgroupEnable:     os.Getenv("BORING_CGROUP") != "0",
		CPUMaxPercent:    envInt("BORING_CPU_MAX_PCT", 150),
		PidsMax:          envInt("BORING_PIDS_MAX", 512),
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
