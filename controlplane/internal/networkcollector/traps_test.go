package networkcollector

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/networkdevice"
	"github.com/gosnmp/gosnmp"
	"github.com/stretchr/testify/require"
)

// Actual UDP/BER transport with a software SNMPv2c trap sender. This does not
// establish SNMPv3 trap or physical-switch compatibility.
func TestTrapUDPIdentityCredentialsAndMalformedInput(t *testing.T) {
	t.Setenv("TRAP_COMMUNITY", "fixture-only")
	listener, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	c := &Collector{cfg: Config{AllowedCIDRs: []string{"127.0.0.1/32"}}, traps: make(chan trapDatagram, 256), targets: map[string]Target{"target": {TrapCommunityEnv: "TRAP_COMMUNITY"}}, pending: map[string]networkdevice.SourceReport{}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.receiveTraps(ctx, listener)
	packet := &gosnmp.SnmpPacket{Version: gosnmp.Version2c, Community: "fixture-only", PDUType: gosnmp.SNMPv2Trap, Variables: []gosnmp.SnmpPDU{{Name: "1.3.6.1.6.3.1.1.4.1.0", Type: gosnmp.ObjectIdentifier, Value: "1.3.6.1.6.3.1.1.5.3"}}}
	raw, err := packet.MarshalMsg()
	require.NoError(t, err)
	sender, err := net.Dial("udp", listener.LocalAddr().String())
	require.NoError(t, err)
	defer sender.Close()
	_, err = sender.Write(raw)
	require.NoError(t, err)
	require.Eventually(t, func() bool { return len(c.traps) > 0 }, time.Second, 10*time.Millisecond)
	c.drainTraps([]binding{{ID: "bound", TargetID: "target", SourceType: "snmp_trap", SenderAddress: "127.0.0.1"}})
	require.Equal(t, "partial", c.pending["bound"].State)
	require.Len(t, c.pending["bound"].Records, 1)
	require.NotNil(t, c.pending["bound"].LastContactAt)
	t.Setenv("TRAP_COMMUNITY", "wrong")
	state, _ := decodeTrap(raw, Target{TrapCommunityEnv: "TRAP_COMMUNITY"})
	require.Equal(t, "auth_failed", state)
	state, record := decodeTrap([]byte{1, 2, 3}, Target{})
	require.NotEqual(t, "ready", state)
	require.Nil(t, record)
	c.traps <- trapDatagram{sender: "192.0.2.99", data: raw, observed: time.Now().UTC()}
	c.drainTraps(nil)
	require.Equal(t, int64(1), c.dropped.Load())
}
