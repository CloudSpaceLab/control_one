package offlineapt

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

// CheckExecutionIsolation is a conservative prerequisite, not a host firewall.
// This initial profile accepts only disconnected routing tables: private,
// link-local and loopback networks, with no active default/public route.
// External isolation must also prevent another host acting as an upstream
// relay. Root administrators and signed package publishers remain trusted.
func CheckExecutionIsolation() error {
	v4, err := os.ReadFile("/proc/net/route")
	if err != nil {
		return fmt.Errorf("offline routing evidence unavailable: %w", err)
	}
	v6, err := os.ReadFile("/proc/net/ipv6_route")
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("offline IPv6 routing evidence unavailable: %w", err)
	}
	return checkRoutes(string(v4), string(v6))
}

func checkRoutes(v4, v6 string) error {
	allowed := []*net.IPNet{}
	for _, cidr := range []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "127.0.0.0/8", "169.254.0.0/16", "::1/128", "fc00::/7", "fe80::/10", "ff02::/16"} {
		_, block, _ := net.ParseCIDR(cidr)
		allowed = append(allowed, block)
	}
	check := func(ip net.IP, mask net.IPMask) error {
		ones, bits := mask.Size()
		if ones == 0 || bits == 0 {
			return fmt.Errorf("offline installation requires no active default route")
		}
		last := append(net.IP(nil), ip...)
		for i := range last {
			last[i] |= ^mask[i]
		}
		for _, block := range allowed {
			if block.Contains(ip) && block.Contains(last) {
				return nil
			}
		}
		return fmt.Errorf("offline installation refuses an active public network route")
	}
	for index, line := range strings.Split(strings.TrimSpace(v4), "\n") {
		if index == 0 && strings.HasPrefix(strings.TrimSpace(line), "Iface") {
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 8 {
			return fmt.Errorf("invalid IPv4 routing evidence")
		}
		flags, err := strconv.ParseUint(fields[3], 16, 32)
		if err != nil {
			return fmt.Errorf("invalid IPv4 route flags")
		}
		if flags&1 == 0 {
			continue
		}
		dest, e1 := strconv.ParseUint(fields[1], 16, 32)
		mask, e2 := strconv.ParseUint(fields[7], 16, 32)
		if e1 != nil || e2 != nil {
			return fmt.Errorf("invalid IPv4 routing evidence")
		}
		ip := net.IP{byte(dest), byte(dest >> 8), byte(dest >> 16), byte(dest >> 24)}
		netmask := net.IPMask{byte(mask), byte(mask >> 8), byte(mask >> 16), byte(mask >> 24)}
		if err = check(ip, netmask); err != nil {
			return err
		}
	}
	for _, line := range strings.Split(strings.TrimSpace(v6), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 10 {
			return fmt.Errorf("invalid IPv6 routing evidence")
		}
		flags, err := strconv.ParseUint(fields[8], 16, 32)
		if err != nil {
			return fmt.Errorf("invalid IPv6 route flags")
		}
		if flags&1 == 0 {
			continue
		}
		prefix, err := strconv.ParseUint(fields[1], 16, 8)
		if err != nil || prefix > 128 {
			return fmt.Errorf("invalid IPv6 route prefix")
		}
		if len(fields[0]) != 32 {
			return fmt.Errorf("invalid IPv6 routing evidence")
		}
		parts := []string{}
		for i := 0; i < 32; i += 4 {
			parts = append(parts, fields[0][i:i+4])
		}
		ip := net.ParseIP(strings.Join(parts, ":"))
		if ip == nil {
			return fmt.Errorf("invalid IPv6 route address")
		}
		if err = check(ip, net.CIDRMask(int(prefix), 128)); err != nil {
			return err
		}
	}
	return nil
}
