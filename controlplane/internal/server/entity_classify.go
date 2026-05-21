package server

import (
	"net"
	"regexp"
	"strings"
	"time"
)

// Entity type identifiers used across the Investigate API surface.
const (
	EntityTypeIP      = "ip"
	EntityTypeHash    = "hash"
	EntityTypeUUID    = "uuid"
	EntityTypeEmail   = "email"
	EntityTypeDomain  = "domain"
	EntityTypeProcess = "process"
	EntityTypeFile    = "file"
	EntityTypeUser    = "user"
	EntityTypeHost    = "host"
)

// Pre-compiled regexes for cheap classification. Order matters in
// ClassifyValue — we prefer specific matches (UUID, hash) over general
// (domain) ones.
var (
	reMD5    = regexp.MustCompile(`^[a-fA-F0-9]{32}$`)
	reSHA1   = regexp.MustCompile(`^[a-fA-F0-9]{40}$`)
	reSHA256 = regexp.MustCompile(`^[a-fA-F0-9]{64}$`)
	reUUID   = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	reEmail  = regexp.MustCompile(`^[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}$`)
	reDomain = regexp.MustCompile(`^(?:[A-Za-z0-9](?:[A-Za-z0-9\-]{0,61}[A-Za-z0-9])?\.)+[A-Za-z]{2,}$`)
)

// ClassifyValue inspects a free-text query value and returns the most
// specific entity type it matches plus a confidence score in [0,1]. An
// empty entityType means we couldn't classify the input.
func ClassifyValue(v string) (entityType string, confidence float64) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", 0
	}

	// Specific first: hashes / UUIDs are unambiguous.
	switch {
	case reSHA256.MatchString(v):
		return EntityTypeHash, 1.0
	case reSHA1.MatchString(v):
		return EntityTypeHash, 1.0
	case reMD5.MatchString(v):
		return EntityTypeHash, 1.0
	case reUUID.MatchString(v):
		return EntityTypeUUID, 1.0
	}

	// IP (v4 or v6) — net.ParseIP handles both.
	if ip := net.ParseIP(v); ip != nil {
		return EntityTypeIP, 1.0
	}

	if reEmail.MatchString(v) {
		return EntityTypeEmail, 0.95
	}

	// Domain only after email so foo@bar.com isn't classified as a domain.
	if reDomain.MatchString(v) {
		return EntityTypeDomain, 0.85
	}

	// Filesystem-looking values: anything with a path separator we treat
	// as a file path.
	if strings.ContainsAny(v, "/\\") {
		return EntityTypeFile, 0.6
	}

	// Otherwise fall through unclassified — caller can scope to process
	// / user / host based on context.
	return "", 0
}

// ClassificationChip is a UI-facing label produced by ClassifyIP. It
// becomes the orange/red/blue chips next to an IP in the Investigate
// timeline.
type ClassificationChip struct {
	Label    string `json:"label"`
	Severity string `json:"severity,omitempty"`
}

// TFRow is the minimal threat-feed match record consumed by ClassifyIP.
// The storage layer builds these from threat_feeds + indicator tables.
type TFRow struct {
	Feed      string
	Severity  string
	FirstSeen time.Time
}

// ClassifyIP produces the chips displayed on an IP in the Investigate UI.
// Pure-Go: no DB, no I/O — caller pre-fetches asset CIDRs and threat-feed
// matches and passes them in.
func ClassifyIP(addr string, tenantAssets []net.IPNet, threatFeeds []TFRow) []ClassificationChip {
	var chips []ClassificationChip

	ip := net.ParseIP(strings.TrimSpace(addr))
	if ip == nil {
		return chips
	}

	// Asset match wins over generic INTERNAL / EXTERNAL.
	asset := false
	for _, n := range tenantAssets {
		if n.Contains(ip) {
			asset = true
			break
		}
	}
	if asset {
		chips = append(chips, ClassificationChip{Label: "ASSET", Severity: "info"})
	}

	if isPrivateIP(ip) {
		chips = append(chips, ClassificationChip{Label: "INTERNAL", Severity: "info"})
	} else if !asset {
		chips = append(chips, ClassificationChip{Label: "EXTERNAL", Severity: "info"})
	}

	for _, tf := range threatFeeds {
		feed := strings.TrimSpace(tf.Feed)
		if feed == "" {
			continue
		}
		sev := strings.ToLower(strings.TrimSpace(tf.Severity))
		if sev == "" {
			sev = "high"
		}
		chips = append(chips, ClassificationChip{
			Label:    "BLACKLISTED:" + feed,
			Severity: sev,
		})
	}

	return chips
}

// isPrivateIP returns true for RFC1918, loopback, link-local and unique
// local IPv6 addresses — anything we wouldn't expect to see on the public
// internet.
func isPrivateIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil {
		switch {
		case ip4[0] == 10:
			return true
		case ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31:
			return true
		case ip4[0] == 192 && ip4[1] == 168:
			return true
		case ip4[0] == 127:
			return true
		case ip4[0] == 169 && ip4[1] == 254:
			return true
		}
		return false
	}
	// IPv6 unique local fc00::/7
	if len(ip) == net.IPv6len && (ip[0]&0xfe) == 0xfc {
		return true
	}
	return false
}

func isPublicRoutableIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip4 := ip.To4(); ip4 != nil {
		ip = ip4
	}
	if ip.IsUnspecified() || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return false
	}
	if ip4 := ip.To4(); ip4 != nil {
		a, b, c := ip4[0], ip4[1], ip4[2]
		switch {
		case a == 0 || a == 127 || a == 255 || a >= 224:
			return false
		case a == 100 && b >= 64 && b <= 127:
			return false
		case a == 192 && b == 0 && c == 2:
			return false
		case a == 198 && (b == 18 || b == 19):
			return false
		case a == 198 && b == 51 && c == 100:
			return false
		case a == 203 && b == 0 && c == 113:
			return false
		}
		return ip.IsGlobalUnicast()
	}
	if len(ip) == net.IPv6len && ip[0] == 0x20 && ip[1] == 0x01 && ip[2] == 0x0d && ip[3] == 0xb8 {
		return false
	}
	return ip.IsGlobalUnicast()
}
