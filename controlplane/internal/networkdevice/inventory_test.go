package networkdevice

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"
	"github.com/stretchr/testify/require"
)

// These fixtures are authored synthetic PDUs, not captures from physical devices.
type inventoryFixture struct {
	Description string `json:"description"`
	ObjectID    string `json:"object_id"`
	PDUs        []struct {
		OID    string `json:"oid"`
		Text   string `json:"text"`
		Number *int   `json:"number"`
	} `json:"pdus"`
	failure error
	repeat  int
	calls   int
}

func (f *inventoryFixture) Get(_ []string) (*gosnmp.SnmpPacket, error) {
	if f.failure != nil {
		return nil, f.failure
	}
	return &gosnmp.SnmpPacket{Variables: []gosnmp.SnmpPDU{
		{Name: "1.3.6.1.2.1.1.1.0", Type: gosnmp.OctetString, Value: []byte(f.Description)},
		{Name: "1.3.6.1.2.1.1.2.0", Type: gosnmp.ObjectIdentifier, Value: f.ObjectID},
		{Name: "1.3.6.1.2.1.1.3.0", Type: gosnmp.TimeTicks, Value: uint32(12345)},
		{Name: "1.3.6.1.2.1.1.5.0", Type: gosnmp.OctetString, Value: []byte("fixture-appliance")},
	}}, nil
}
func (f *inventoryFixture) BulkWalk(root string, callback gosnmp.WalkFunc) error {
	for _, p := range f.PDUs {
		if strings.HasPrefix(p.OID, root+".") {
			pdu := gosnmp.SnmpPDU{Name: p.OID, Type: gosnmp.OctetString, Value: []byte(p.Text)}
			if p.Number != nil {
				pdu.Type = gosnmp.Integer
				pdu.Value = *p.Number
			}
			repeats := 1
			if f.repeat > 0 {
				repeats = f.repeat
			}
			for i := 0; i < repeats; i++ {
				f.calls++
				if err := callback(pdu); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
func loadInventoryFixture(t *testing.T, path string) *inventoryFixture {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var f inventoryFixture
	require.NoError(t, json.Unmarshal(raw, &f))
	return &f
}

func TestInventorySyntheticVendorFixtures(t *testing.T) {
	for _, test := range []struct {
		name, vendor, model string
		neighbors           int
	}{{"cisco", "Cisco", "C9300-48P", 2}, {"fortinet", "Fortinet", "FortiGate-100F", 1}} {
		t.Run(test.name, func(t *testing.T) {
			fixture := loadInventoryFixture(t, "testdata/inventory-"+test.name+".json")
			observed := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
			inv := collectInventory(context.Background(), fixture, Credential{}, observed)
			require.Equal(t, "inventory_ready", inv.State)
			require.Equal(t, test.vendor, inv.Facts["vendor"].Value)
			require.Equal(t, test.model, inv.Facts["model"].Value)
			require.Len(t, inv.Interfaces, 1)
			require.Contains(t, inv.Interfaces[0].Facts, "name")
			require.Len(t, inv.Neighbors, test.neighbors)
			require.Contains(t, inv.Facts, "serial")
			require.Contains(t, inv.Facts, "firmware")
			for _, neighbor := range inv.Neighbors {
				require.Contains(t, neighbor.Facts, "local_port_index")
				require.Equal(t, observed, neighbor.Facts["discovery_protocol"].ObservedAt)
			}
			require.NotContains(t, inv.Facts, "config_hash")
			require.NotContains(t, inv.Facts, "ha_role")
			repeated := collectInventory(context.Background(), fixture, Credential{}, observed)
			require.Equal(t, inv, repeated)
			if test.name == "cisco" {
				require.Equal(t, "down", inv.Interfaces[0].Facts["oper_state"].Value)
				require.Len(t, inv.VLANs, 1)
				require.Len(t, inv.Resources, 1)
				require.Equal(t, 34, inv.Resources[0].Facts["value"].Value)
				require.Equal(t, "degrees C", inv.Resources[0].Facts["units_display"].Value)
			}
		})
	}
}

func TestInventoryBoundsRedactionAndFailures(t *testing.T) {
	fixture := loadInventoryFixture(t, "testdata/inventory-cisco.json")
	fixture.Description += " SENSITIVE_AUTH"
	fixture.PDUs[1].Text = "port-SENSITIVE_PRIV"
	inv := collectInventory(context.Background(), fixture, Credential{AuthSecret: "SENSITIVE_AUTH", PrivSecret: "SENSITIVE_PRIV"}, time.Now())
	raw, err := json.Marshal(inv)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "SENSITIVE_AUTH")
	require.NotContains(t, string(raw), "SENSITIVE_PRIV")
	fixture.PDUs[1].Text = "\x00SENSITIVE_AUTH"
	inv = collectInventory(context.Background(), fixture, Credential{AuthSecret: "SENSITIVE_AUTH"}, time.Now())
	raw, err = json.Marshal(inv)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "SENSITIVE_AUTH")
	require.NotContains(t, string(raw), hex.EncodeToString([]byte("SENSITIVE_AUTH")))
	fixture.repeat = 10000
	fixture.calls = 0
	inv = collectInventory(context.Background(), fixture, Credential{}, time.Now())
	require.LessOrEqual(t, len(inv.Interfaces), 512)
	require.LessOrEqual(t, len(inv.Raw), 2048)
	require.Less(t, fixture.calls, 10000)
	require.Contains(t, strings.Join(inv.Unavailable, " "), "limit")
	fixture.repeat = 0
	for _, test := range []struct {
		err   error
		state string
	}{{gosnmp.ErrUnknownUsername, "auth_failed"}, {errors.New("timeout"), "unreachable"}, {errors.New("incoming packet is not authentic"), "auth_failed"}} {
		fixture.failure = test.err
		inv = collectInventory(context.Background(), fixture, Credential{}, time.Now())
		require.Equal(t, test.state, inv.State)
		require.Empty(t, inv.Interfaces)
	}
	require.Equal(t, "unsupported", Refresh(context.Background(), "192.0.2.1", 22, "ssh", Credential{}).State)
}

func TestFirstWaveIdentitySignatures(t *testing.T) {
	// Authored signatures test parser behavior, not device compatibility.
	for _, sample := range []struct{ description, vendor, platform string }{
		{"Cisco IOS XE fixture", "Cisco", "IOS XE"},
		{"Cisco IOS Software fixture", "Cisco", "IOS"},
		{"Cisco NX-OS fixture", "Cisco", "NX-OS"},
		{"Juniper Junos fixture", "Juniper", "Junos"},
		{"Arista EOS fixture", "Arista", "EOS"},
		{"FortiGate-100F FortiOS fixture", "Fortinet", "FortiOS"},
		{"Palo Alto PAN-OS fixture", "Palo Alto Networks", "PAN-OS"},
		{"F5 BIG-IP fixture", "F5", "BIG-IP"},
	} {
		r := Fingerprint(sample.description, "", Credential{})
		require.Equal(t, sample.vendor, r.Vendor)
		require.Equal(t, sample.platform, r.Platform)
	}
}
