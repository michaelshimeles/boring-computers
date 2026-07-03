package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log"
	"net"
	"path/filepath"
	"sync"
	"time"
)

// Sentinel errors used by the Manager and surfaced as HTTP statuses.
var (
	ErrNotFound            = errors.New("machine not found")
	ErrTooManyMachines     = errors.New("machine capacity reached")
	ErrSnapshotUnavailable = errors.New("snapshot unavailable")
	ErrRateLimited         = errors.New("rate limit exceeded for your address")
)

// Machine is a single running microVM plus the bookkeeping boringd needs to
// manage its lifecycle. The exported time/id fields are stable; runtime handles
// (console, driver) are internal.
type Machine struct {
	ID        string
	Status    string
	Mode      string
	BootMS    int64
	Template  string
	Display   bool
	CreatedAt time.Time
	ExpiresAt time.Time

	// creatorIP holds the limiter slot to release when the machine dies.
	creatorIP string

	// driver owns the firecracker child process, stdio console and API socket.
	driver *fcDriver

	// timer fires at ExpiresAt to reap the machine.
	timer *time.Timer
}

// machineView is the JSON-serialisable public shape from the contract.
type machineView struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	Mode      string `json:"mode"`
	BootMS    int64  `json:"boot_ms"`
	Template  string `json:"template"`
	Display   bool   `json:"display"`
	CreatedAt string `json:"created_at"`
	ExpiresAt string `json:"expires_at"`
}

// View returns the JSON view of the machine.
func (m *Machine) View() machineView {
	return machineView{
		ID:        m.ID,
		Status:    m.Status,
		Mode:      m.Mode,
		BootMS:    m.BootMS,
		Template:  m.Template,
		Display:   m.Display,
		CreatedAt: m.CreatedAt.UTC().Format(time.RFC3339),
		ExpiresAt: m.ExpiresAt.UTC().Format(time.RFC3339),
	}
}

// Manager is the thread-safe machine registry and lifecycle owner.
type Manager struct {
	cfg      Config
	limiter  *Limiter
	cgroups  *Cgroups
	mu       sync.Mutex
	machines map[string]*Machine
	stopCh   chan struct{}
}

// NewManager constructs an empty Manager with per-IP limiting and cgroup caps.
func NewManager(cfg Config) *Manager {
	return &Manager{
		cfg:      cfg,
		limiter:  NewLimiter(cfg.PerIPMax, cfg.CreateRatePerMin),
		cgroups:  NewCgroups(cfg),
		machines: make(map[string]*Machine),
		stopCh:   make(chan struct{}),
	}
}

// Count returns the number of live machines.
func (mgr *Manager) Count() int {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	return len(mgr.machines)
}

// Get returns a JSON view of a machine by id, built under the lock so it never
// races with Create/Branch mutating the machine's fields mid-boot.
func (mgr *Manager) Get(id string) (machineView, bool) {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	m, ok := mgr.machines[id]
	if !ok {
		return machineView{}, false
	}
	return m.View(), true
}

// Console returns the live console for a machine's guest serial, under the lock
// so the driver field read never races with Create/Branch setting it.
func (mgr *Manager) Console(id string) (*Console, bool) {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	m, ok := mgr.machines[id]
	if !ok || m.driver == nil {
		return nil, false
	}
	return m.driver.Console(), true
}

// DialVsock opens a stream to a guest vsock port for the machine (used by the
// /vnc bridge). Returns ErrNotFound if the machine is gone.
func (mgr *Manager) DialVsock(id string, port int) (net.Conn, error) {
	mgr.mu.Lock()
	m, ok := mgr.machines[id]
	drv := (*fcDriver)(nil)
	if ok {
		drv = m.driver
	}
	mgr.mu.Unlock()
	if !ok || drv == nil {
		return nil, ErrNotFound
	}
	return drv.DialVsock(port)
}

// List returns JSON views of all machines.
func (mgr *Manager) List() []machineView {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	out := make([]machineView, 0, len(mgr.machines))
	for _, m := range mgr.machines {
		out = append(out, m.View())
	}
	return out
}

// Create boots a new microVM from the given template with the (clamped) TTL.
// creatorIP is used for per-IP rate/concurrency limiting on the public endpoint.
func (mgr *Manager) Create(template string, ttlSeconds int, net bool, creatorIP string) (*Machine, error) {
	ttl := mgr.cfg.ClampTTL(ttlSeconds)

	// Per-IP rate + concurrency cap (released in teardown).
	if err := mgr.limiter.Acquire(creatorIP); err != nil {
		return nil, err
	}

	// Reserve a slot + id under the lock, but perform the (slow) boot outside it.
	mgr.mu.Lock()
	if len(mgr.machines) >= mgr.cfg.MaxMachines {
		mgr.mu.Unlock()
		mgr.limiter.Release(creatorIP)
		return nil, ErrTooManyMachines
	}
	tpl := mgr.cfg.Template(template)
	id := mgr.newID()
	now := time.Now()
	m := &Machine{
		ID:        id,
		Status:    "booting",
		Template:  tpl.Name,
		Display:   tpl.Display,
		creatorIP: creatorIP,
		CreatedAt: now,
		ExpiresAt: now.Add(time.Duration(ttl) * time.Second),
	}
	// Insert a placeholder so the slot is held and the id is unique.
	mgr.machines[id] = m
	mgr.mu.Unlock()

	// Use the template's prebuilt snapshot for a fast restore when eligible and
	// present; bootMachine falls back to a cold boot if the restore fails.
	// `net` forces a cold boot (the snapshot has no NIC) so the VM gets internet.
	snapDir := ""
	if tpl.Snapshot && !(net && mgr.cfg.NetEnable) {
		cand := filepath.Join(mgr.cfg.TemplatesDir, tpl.Name)
		if fileExists(filepath.Join(cand, "snapshot_file")) && fileExists(filepath.Join(cand, "mem_file")) {
			snapDir = cand
		}
	}

	drv, mode, bootMS, err := bootMachine(mgr.cfg, id, tpl, snapDir)
	if err != nil {
		// Roll back the reservation (and the per-IP slot).
		mgr.mu.Lock()
		delete(mgr.machines, id)
		mgr.mu.Unlock()
		mgr.limiter.Release(creatorIP)
		return nil, err
	}

	// Cap the guest's host resources (CPU/memory/pids). In jailed mode the jailer
	// already created a capped cgroup for the VM, so we don't place it again.
	if !mgr.cfg.JailerEnable {
		mgr.cgroups.Place(drv.PID(), id, tpl)
	}

	mgr.mu.Lock()
	m.driver = drv
	m.Mode = mode
	m.BootMS = bootMS
	m.Status = "running"
	m.timer = time.AfterFunc(time.Until(m.ExpiresAt), func() { mgr.reap(id) })
	mgr.mu.Unlock()

	log.Printf("machine %s created (mode=%s boot_ms=%d ttl=%ds ip=%s)", id, mode, bootMS, ttl, creatorIP)
	return m, nil
}

// Branch forks a new machine from the source machine's live snapshot. Best
// effort: returns ErrSnapshotUnavailable (mapped to 501) if snapshotting fails.
func (mgr *Manager) Branch(id, creatorIP string) (*Machine, error) {
	mgr.mu.Lock()
	src, ok := mgr.machines[id]
	if !ok {
		mgr.mu.Unlock()
		return nil, ErrNotFound
	}
	if len(mgr.machines) >= mgr.cfg.MaxMachines {
		mgr.mu.Unlock()
		return nil, ErrTooManyMachines
	}
	srcDriver := src.driver
	newID := mgr.newID()
	now := time.Now()
	ttl := mgr.cfg.ClampTTL(int(time.Until(src.ExpiresAt).Seconds()))
	mgr.mu.Unlock()

	// A fork counts against the caller's per-IP budget (released in teardown).
	if err := mgr.limiter.Acquire(creatorIP); err != nil {
		return nil, err
	}

	mgr.mu.Lock()
	child := &Machine{
		ID:        newID,
		Status:    "booting",
		Template:  src.Template,
		creatorIP: creatorIP,
		CreatedAt: now,
		ExpiresAt: now.Add(time.Duration(ttl) * time.Second),
	}
	mgr.machines[newID] = child
	mgr.mu.Unlock()

	if srcDriver == nil {
		mgr.rollback(newID)
		return nil, ErrSnapshotUnavailable
	}

	// Snapshot the source VM to a fresh directory.
	snapDir, err := srcDriver.CreateSnapshot(newID)
	if err != nil {
		mgr.rollback(newID)
		log.Printf("branch %s: snapshot failed: %v", id, err)
		return nil, ErrSnapshotUnavailable
	}

	drv, mode, bootMS, err := bootMachine(mgr.cfg, newID, mgr.cfg.Template(src.Template), snapDir)
	if err != nil {
		mgr.rollback(newID)
		log.Printf("branch %s: restore failed: %v", id, err)
		return nil, ErrSnapshotUnavailable
	}

	if !mgr.cfg.JailerEnable {
		mgr.cgroups.Place(drv.PID(), newID, mgr.cfg.Template(src.Template))
	}

	mgr.mu.Lock()
	child.driver = drv
	child.Mode = mode
	child.BootMS = bootMS
	child.Status = "running"
	child.timer = time.AfterFunc(time.Until(child.ExpiresAt), func() { mgr.reap(newID) })
	mgr.mu.Unlock()

	log.Printf("machine %s branched from %s (mode=%s boot_ms=%d)", newID, id, mode, bootMS)
	return child, nil
}

// Destroy tears down a machine by id, returning false if it does not exist.
func (mgr *Manager) Destroy(id string) bool {
	mgr.mu.Lock()
	m, ok := mgr.machines[id]
	if !ok {
		mgr.mu.Unlock()
		return false
	}
	delete(mgr.machines, id)
	mgr.mu.Unlock()

	mgr.teardown(m)
	log.Printf("machine %s destroyed", id)
	return true
}

// reap is invoked by the TTL timer.
func (mgr *Manager) reap(id string) {
	mgr.mu.Lock()
	m, ok := mgr.machines[id]
	if !ok {
		mgr.mu.Unlock()
		return
	}
	delete(mgr.machines, id)
	mgr.mu.Unlock()

	mgr.teardown(m)
	log.Printf("machine %s expired (ttl)", id)
}

// rollback removes a reserved slot and is used when boot/branch fails.
func (mgr *Manager) rollback(id string) {
	mgr.mu.Lock()
	m := mgr.machines[id]
	delete(mgr.machines, id)
	mgr.mu.Unlock()
	if m != nil {
		mgr.teardown(m)
	}
}

// teardown stops timers and kills the driver. Safe to call with a partially
// constructed machine.
func (mgr *Manager) teardown(m *Machine) {
	if m == nil {
		return
	}
	if m.timer != nil {
		m.timer.Stop()
	}
	if m.driver != nil {
		m.driver.Close()
	}
	mgr.cgroups.Remove(m.ID)
	if m.creatorIP != "" {
		mgr.limiter.Release(m.creatorIP)
	}
}

// StartReaper starts a background sweep as a safety net for any timers that were
// missed (the per-machine AfterFunc is the primary mechanism).
func (mgr *Manager) StartReaper() {
	go func() {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-mgr.stopCh:
				return
			case <-t.C:
				now := time.Now()
				var expired []string
				mgr.mu.Lock()
				for id, m := range mgr.machines {
					if now.After(m.ExpiresAt) {
						expired = append(expired, id)
					}
				}
				mgr.mu.Unlock()
				for _, id := range expired {
					mgr.reap(id)
				}
			}
		}
	}()
}

// Shutdown stops the reaper and tears down all live machines.
func (mgr *Manager) Shutdown() {
	close(mgr.stopCh)
	mgr.mu.Lock()
	all := make([]*Machine, 0, len(mgr.machines))
	for id, m := range mgr.machines {
		all = append(all, m)
		delete(mgr.machines, id)
	}
	mgr.mu.Unlock()
	for _, m := range all {
		mgr.teardown(m)
	}
}

// newID returns a fresh "m-<8 hex>" id. Caller must hold mgr.mu.
func (mgr *Manager) newID() string {
	for {
		var b [4]byte
		_, _ = rand.Read(b[:])
		id := "m-" + hex.EncodeToString(b[:])
		if _, exists := mgr.machines[id]; !exists {
			return id
		}
	}
}
