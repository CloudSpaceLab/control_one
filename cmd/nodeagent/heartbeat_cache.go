package main

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type inventorySnapshot struct {
	pkgs      []PackageInfo
	hash      string
	kernel    string
	osVersion string
	purposes  []ServerPurpose
	collected time.Time
	signature string
}

var heartbeatInventorySnapshot = struct {
	mu   sync.Mutex
	data inventorySnapshot
}{}

func cachedInventorySnapshot() ([]PackageInfo, string, string, string, []ServerPurpose, error) {
	now := time.Now().UTC()
	refresh := heartbeatInventoryRefresh()
	if refresh <= 0 {
		refresh = 12 * time.Hour
	}
	signature := packageStoreSignature()

	heartbeatInventorySnapshot.mu.Lock()
	cached := heartbeatInventorySnapshot.data
	if !cached.collected.IsZero() &&
		now.Sub(cached.collected) < refresh &&
		(signature == "" || signature == cached.signature) {
		pkgs := append([]PackageInfo(nil), cached.pkgs...)
		purposes := append([]ServerPurpose(nil), cached.purposes...)
		heartbeatInventorySnapshot.mu.Unlock()
		return pkgs, cached.hash, cached.kernel, cached.osVersion, purposes, nil
	}
	heartbeatInventorySnapshot.mu.Unlock()

	pkgs, hash, err := collectInventory()
	if err != nil {
		return pkgs, hash, "", "", nil, err
	}
	if hash == "" {
		heartbeatInventorySnapshot.mu.Lock()
		heartbeatInventorySnapshot.data = inventorySnapshot{collected: now, signature: signature}
		heartbeatInventorySnapshot.mu.Unlock()
		return pkgs, hash, "", "", nil, nil
	}
	snap := inventorySnapshot{
		pkgs:      append([]PackageInfo(nil), pkgs...),
		hash:      hash,
		kernel:    kernelVersion(),
		osVersion: osVersion(),
		purposes:  inferServerPurposesFromPackages(pkgs),
		collected: now,
		signature: signature,
	}

	heartbeatInventorySnapshot.mu.Lock()
	heartbeatInventorySnapshot.data = snap
	heartbeatInventorySnapshot.mu.Unlock()

	return append([]PackageInfo(nil), snap.pkgs...), snap.hash, snap.kernel, snap.osVersion, append([]ServerPurpose(nil), snap.purposes...), nil
}

type firewallSnapshot struct {
	state       FirewallState
	hash        string
	collected   time.Time
	invalidated bool
}

var heartbeatFirewallSnapshot = struct {
	mu   sync.Mutex
	data firewallSnapshot
}{}

func cachedFirewallSnapshot() FirewallState {
	now := time.Now().UTC()
	refresh := heartbeatFirewallRefresh()

	heartbeatFirewallSnapshot.mu.Lock()
	cached := heartbeatFirewallSnapshot.data
	if cached.hash != "" && !cached.invalidated && (refresh == 0 || now.Sub(cached.collected) < refresh) {
		heartbeatFirewallSnapshot.mu.Unlock()
		return cached.state
	}
	heartbeatFirewallSnapshot.mu.Unlock()

	st := collectFirewall()
	heartbeatFirewallSnapshot.mu.Lock()
	heartbeatFirewallSnapshot.data = firewallSnapshot{
		state:     st,
		hash:      firewallStateHash(st),
		collected: now,
	}
	heartbeatFirewallSnapshot.mu.Unlock()
	return st
}

func invalidateFirewallCache() {
	heartbeatFirewallSnapshot.mu.Lock()
	heartbeatFirewallSnapshot.data.invalidated = true
	heartbeatFirewallSnapshot.mu.Unlock()
}

func packageStoreSignature() string {
	switch runtime.GOOS {
	case "linux":
		return statSignature([]string{
			"/var/lib/dpkg/status",
			"/var/lib/rpm/Packages",
			"/var/lib/rpm/rpmdb.sqlite",
			"/var/lib/rpm/Packages.db",
			"/lib/apk/db/installed",
			"/etc/apk/world",
		})
	case "aix":
		return statSignature([]string{
			"/usr/lib/objrepos/product",
			"/etc/objrepos/product",
		})
	default:
		return ""
	}
}

func statSignature(paths []string) string {
	var b strings.Builder
	for _, path := range paths {
		st, err := os.Stat(path)
		if err != nil {
			continue
		}
		b.WriteString(path)
		b.WriteByte('=')
		b.WriteString(st.ModTime().UTC().Format(time.RFC3339Nano))
		b.WriteByte(':')
		b.WriteString(strconv.FormatInt(st.Size(), 10))
		b.WriteByte(';')
	}
	return b.String()
}
