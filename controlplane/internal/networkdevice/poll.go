package networkdevice

import (
	"context"
	"github.com/gosnmp/gosnmp"
	"time"
)

// PollSNMP reads standard operational counters only. No SET or device command
// is issued; optional MIB coverage is independent of other telemetry sources.
func PollSNMP(ctx context.Context, ip string, port int, credential Credential) (string, map[string]float64) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	client := newSNMPClient(ctx, ip, port, credential)
	if err := client.Connect(); err != nil {
		return "unreachable", nil
	}
	defer client.Conn.Close()
	packet, err := client.Get([]string{"1.3.6.1.2.1.1.3.0", "1.3.6.1.2.1.2.1.0"})
	if state := snmpFailure(packet, err); state != "" {
		return state, nil
	}
	metrics := map[string]float64{}
	for _, v := range packet.Variables {
		if v.Type != gosnmp.TimeTicks && v.Type != gosnmp.Integer {
			continue
		}
		n := gosnmp.ToBigInt(v.Value)
		if n.Sign() < 0 {
			continue
		}
		switch v.Name {
		case ".1.3.6.1.2.1.1.3.0", "1.3.6.1.2.1.1.3.0":
			metrics["uptime_ticks"] = float64(n.Uint64())
		case ".1.3.6.1.2.1.2.1.0", "1.3.6.1.2.1.2.1.0":
			metrics["interface_count"] = float64(n.Uint64())
		}
	}
	if len(metrics) == 0 {
		return "unsupported", nil
	}
	if len(metrics) < 2 {
		return "partial", metrics
	}
	return "ready", metrics
}
