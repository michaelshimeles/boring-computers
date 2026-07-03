package main

import (
	"crypto/sha1"
	"fmt"
	"os/exec"
)

// tapName derives a stable host tap device name for a machine. Kept short
// (≤15 chars): "bt" + 8 hex = 10.
func tapName(id string) string {
	h := sha1.Sum([]byte(id))
	return fmt.Sprintf("bt%x", h[:4])
}

// guestMAC derives a stable locally-administered MAC for a machine.
func guestMAC(id string) string {
	h := sha1.Sum([]byte(id))
	return fmt.Sprintf("06:00:%02x:%02x:%02x:%02x", h[0], h[1], h[2], h[3])
}

// createTap makes a tap owned by uid (the jailed firecracker uid, so it can open
// the device), attaches it to the bridge, and brings it up.
func createTap(name string, uid int, bridge string) error {
	add := []string{"tuntap", "add", name, "mode", "tap"}
	if uid > 0 {
		add = append(add, "user", fmt.Sprint(uid))
	}
	if out, err := exec.Command("ip", add...).CombinedOutput(); err != nil {
		return fmt.Errorf("tap add: %v: %s", err, out)
	}
	if out, err := exec.Command("ip", "link", "set", name, "master", bridge).CombinedOutput(); err != nil {
		exec.Command("ip", "link", "del", name).Run()
		return fmt.Errorf("tap bridge: %v: %s", err, out)
	}
	if out, err := exec.Command("ip", "link", "set", name, "up").CombinedOutput(); err != nil {
		exec.Command("ip", "link", "del", name).Run()
		return fmt.Errorf("tap up: %v: %s", err, out)
	}
	return nil
}

// teardownTap removes a tap device (best-effort).
func teardownTap(name string) {
	if name != "" {
		exec.Command("ip", "link", "del", name).Run()
	}
}
