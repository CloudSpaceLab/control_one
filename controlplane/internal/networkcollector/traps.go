package networkcollector

import (
	"context"
	"crypto/subtle"
	"net"
	"os"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/networkdevice"
	"github.com/gosnmp/gosnmp"
	"golang.org/x/time/rate"
)

type trapDatagram struct {
	sender   string
	data     []byte
	observed time.Time
}

func (c *Collector) receiveTraps(ctx context.Context, conn net.PacketConn) {
	limiter := rate.NewLimiter(1000, 2000)
	buf := make([]byte, 65536)
	for {
		n, peer, err := conn.ReadFrom(buf)
		if err != nil {
			return
		}
		if ctx.Err() != nil {
			return
		}
		ip, _, err := net.SplitHostPort(peer.String())
		if err != nil {
			continue
		}
		allowed := false
		for _, cidr := range c.cfg.AllowedCIDRs {
			_, subnet, _ := net.ParseCIDR(cidr)
			if subnet.Contains(net.ParseIP(ip)) {
				allowed = true
			}
		}
		if !allowed || !limiter.Allow() {
			c.dropped.Add(1)
			continue
		}
		event := trapDatagram{sender: ip, data: append([]byte(nil), buf[:n]...), observed: time.Now().UTC()}
		select {
		case c.traps <- event:
		default:
			c.dropped.Add(1)
		}
	}
}
func (c *Collector) drainTraps(bindings []binding) {
	bySender := map[string]binding{}
	for _, b := range bindings {
		if b.SourceType == "snmp_trap" {
			bySender[b.SenderAddress] = b
		}
	}
	for i := 0; i < 256; i++ {
		select {
		case packet := <-c.traps:
			b, ok := bySender[packet.sender]
			if !ok {
				c.dropped.Add(1)
				continue
			}
			target, ok := c.targets[b.TargetID]
			if !ok {
				c.dropped.Add(1)
				continue
			}
			state, record := decodeTrap(packet.data, target)
			report := networkdevice.SourceReport{BindingID: b.ID, State: state, ObservedAt: packet.observed}
			if record != nil {
				contact := packet.observed
				report.LastContactAt = &contact
				report.Records = []map[string]any{record}
				if old, ok := c.pending[b.ID]; ok && len(old.Records) > 0 {
					if len(old.Records) < 10 {
						report.Records = append(old.Records, record)
					} else {
						report.Records = append(old.Records[1:], record)
						c.dropped.Add(1)
					}
				}
			}
			c.pending[b.ID] = report
			c.queued.Store(int64(len(c.pending)))
		default:
			return
		}
	}
}
func decodeTrap(data []byte, target Target) (state string, record map[string]any) {
	// Guard the library's packet parser from malformed external UDP input.
	state = "unsupported"
	defer func() {
		if recover() != nil {
			state = "unsupported"
			record = nil
		}
	}()
	decoder := &gosnmp.GoSNMP{Version: gosnmp.Version3, SecurityModel: gosnmp.UserSecurityModel, MsgFlags: gosnmp.AuthPriv}
	credentials := credential(target)
	if credentials.Validate("snmpv3") == nil {
		auth := gosnmp.SHA256
		if credentials.AuthProtocol == "SHA" {
			auth = gosnmp.SHA
		}
		decoder.SecurityParameters = &gosnmp.UsmSecurityParameters{UserName: credentials.Username, AuthenticationProtocol: auth, AuthenticationPassphrase: credentials.AuthSecret, PrivacyProtocol: gosnmp.AES, PrivacyPassphrase: credentials.PrivSecret}
	}
	packet, err := decoder.UnmarshalTrap(data, false)
	if err != nil {
		return "auth_failed", nil
	}
	if packet.PDUType != gosnmp.SNMPv2Trap && packet.PDUType != gosnmp.Trap {
		return "unsupported", nil
	}
	if packet.Version == gosnmp.Version3 {
		params, ok := packet.SecurityParameters.(*gosnmp.UsmSecurityParameters)
		if !ok || params.UserName != credentials.Username || packet.MsgFlags&gosnmp.AuthPriv != gosnmp.AuthPriv || credentials.Validate("snmpv3") != nil {
			return "auth_failed", nil
		}
	} else if packet.Version == gosnmp.Version2c {
		expected := os.Getenv(target.TrapCommunityEnv)
		if expected == "" || subtle.ConstantTimeCompare([]byte(packet.Community), []byte(expected)) != 1 {
			return "auth_failed", nil
		}
	} else {
		return "unsupported", nil
	}
	if len(packet.Variables) == 0 || len(packet.Variables) > 100 {
		return "unsupported", nil
	}
	// OID/type evidence only: arbitrary varbind strings may contain credentials.
	varbinds := []map[string]any{}
	for _, v := range packet.Variables {
		varbinds = append(varbinds, map[string]any{"oid": v.Name, "type": int(v.Type)})
	}
	return "partial", map[string]any{"version": int(packet.Version), "varbinds": varbinds, "values_omitted": "unclassified trap values require a parser/redaction contract"}
}
