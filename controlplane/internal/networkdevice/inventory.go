package networkdevice

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gosnmp/gosnmp"
)

// Facts are observations, not defaults. Empty/unsupported values are omitted.
type Fact struct {
	Value      any       `json:"value"`
	Protocol   string    `json:"protocol"`
	Source     string    `json:"source"`
	ObservedAt time.Time `json:"observed_at"`
}
type InventoryRecord struct {
	ID    string          `json:"id"`
	Facts map[string]Fact `json:"facts"`
}
type Inventory struct {
	State       string            `json:"state"`
	ObservedAt  time.Time         `json:"observed_at"`
	Adapter     string            `json:"adapter"`
	Facts       map[string]Fact   `json:"facts"`
	Interfaces  []InventoryRecord `json:"interfaces"`
	Neighbors   []InventoryRecord `json:"neighbors"`
	Entities    []InventoryRecord `json:"entities"`
	Addresses   []InventoryRecord `json:"addresses"`
	VLANs       []InventoryRecord `json:"vlans"`
	ARP         []InventoryRecord `json:"arp"`
	Routes      []InventoryRecord `json:"routes"`
	Resources   []InventoryRecord `json:"resources"`
	Unavailable []string          `json:"unavailable"`
	Raw         []InventoryRecord `json:"raw_evidence"`
}

type inventoryReader interface {
	Get([]string) (*gosnmp.SnmpPacket, error)
	BulkWalk(string, gosnmp.WalkFunc) error
}

func newSNMPClient(ctx context.Context, ip string, port int, c Credential) *gosnmp.GoSNMP {
	if port == 0 {
		port = 161
	}
	auth := gosnmp.SHA256
	if c.AuthProtocol == "SHA" {
		auth = gosnmp.SHA
	}
	return &gosnmp.GoSNMP{Target: ip, Port: uint16(port), Version: gosnmp.Version3, Timeout: time.Second, Retries: 0, Context: ctx, MaxOids: 8, MaxRepetitions: 20, SecurityModel: gosnmp.UserSecurityModel, MsgFlags: gosnmp.AuthPriv, SecurityParameters: &gosnmp.UsmSecurityParameters{UserName: c.Username, AuthenticationProtocol: auth, AuthenticationPassphrase: c.AuthSecret, PrivacyProtocol: gosnmp.AES, PrivacyPassphrase: c.PrivSecret}}
}

// Refresh reads only known inventory trees. No SET or arbitrary CLI is used.
// Wall-clock, row, PDU, and value limits also apply to hostile devices.
func Refresh(ctx context.Context, ip string, port int, protocol string, c Credential) Inventory {
	adapter, ok := adapters[protocol]
	if !ok || adapter.Inventory == nil {
		return Inventory{State: "unsupported"}
	}
	return adapter.Inventory(ctx, ip, port, c)
}

func collectSNMPInventory(ctx context.Context, ip string, port int, c Credential) Inventory {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	client := newSNMPClient(ctx, ip, port, c)
	if err := client.Connect(); err != nil {
		return Inventory{State: "unreachable"}
	}
	defer client.Conn.Close()
	return collectInventory(ctx, client, c, time.Now().UTC())
}

func snmpFailure(packet *gosnmp.SnmpPacket, err error) string {
	if errors.Is(err, gosnmp.ErrUnknownUsername) || errors.Is(err, gosnmp.ErrWrongDigest) || errors.Is(err, gosnmp.ErrDecryption) || errors.Is(err, gosnmp.ErrUnknownSecurityLevel) {
		return "auth_failed"
	}
	if err != nil {
		lower := strings.ToLower(err.Error())
		for _, word := range []string{"authentication", "not authentic", "unknown user", "wrong digest", "decryption"} {
			if strings.Contains(lower, word) {
				return "auth_failed"
			}
		}
		return "unreachable"
	}
	if packet == nil {
		return "unsupported"
	}
	if packet.Error == gosnmp.AuthorizationError {
		return "auth_failed"
	}
	if packet.Error != gosnmp.NoError || packet.PDUType == gosnmp.Report {
		return "unsupported"
	}
	return ""
}

func collectInventory(ctx context.Context, reader inventoryReader, c Credential, observed time.Time) Inventory {
	inv := Inventory{State: "inventory_ready", ObservedAt: observed, Adapter: "standard-mibs/v1", Facts: map[string]Fact{}, Interfaces: []InventoryRecord{}, Neighbors: []InventoryRecord{}, Entities: []InventoryRecord{}, Addresses: []InventoryRecord{}, VLANs: []InventoryRecord{}, ARP: []InventoryRecord{}, Routes: []InventoryRecord{}, Resources: []InventoryRecord{}, Unavailable: []string{}, Raw: []InventoryRecord{}}
	redact := func(s string) string {
		for _, secret := range []string{c.Password, c.AuthSecret, c.PrivSecret, c.Passphrase, c.PrivateKey} {
			if secret != "" {
				s = strings.ReplaceAll(s, secret, "[redacted]")
			}
		}
		if len(s) > 256 {
			s = s[:256]
		}
		return strings.ToValidUTF8(s, "")
	}
	fact := func(v any, source string) Fact {
		return Fact{Value: v, Protocol: "snmpv3", Source: source, ObservedAt: observed}
	}
	packet, err := reader.Get([]string{"1.3.6.1.2.1.1.1.0", "1.3.6.1.2.1.1.2.0", "1.3.6.1.2.1.1.3.0", "1.3.6.1.2.1.1.5.0"})
	if state := snmpFailure(packet, err); state != "" {
		inv.State = state
		return inv
	}
	var description, objectID string
	addRaw := func(p gosnmp.SnmpPDU) {
		if len(inv.Raw) >= 2048 {
			return
		}
		value := pduValue(p, redact)
		if value != nil {
			inv.Raw = append(inv.Raw, InventoryRecord{ID: strings.TrimPrefix(p.Name, "."), Facts: map[string]Fact{"value": fact(value, p.Name)}})
		}
	}
	for _, p := range packet.Variables {
		value := pduValue(p, redact)
		if value == nil {
			continue
		}
		addRaw(p)
		switch strings.TrimPrefix(p.Name, ".") {
		case "1.3.6.1.2.1.1.1.0":
			description = fmt.Sprint(value)
		case "1.3.6.1.2.1.1.2.0":
			objectID = fmt.Sprint(value)
		case "1.3.6.1.2.1.1.3.0":
			inv.Facts["uptime_ticks"] = fact(value, p.Name)
		case "1.3.6.1.2.1.1.5.0":
			inv.Facts["hostname"] = fact(value, p.Name)
		}
	}
	if description == "" && objectID == "" {
		inv.State = "unsupported"
		return inv
	}
	detected := Fingerprint(description, objectID, c)
	for key, value := range map[string]string{"vendor": detected.Vendor, "model": detected.Model, "platform": detected.Platform} {
		if value != "" {
			inv.Facts[key] = fact(value, "sysDescr/sysObjectID heuristic")
		}
	}
	inv.Facts["classification_confidence"] = fact(detected.Confidence, "sysDescr/sysObjectID heuristic")
	inv.Facts["classification_evidence"] = fact(detected.Evidence, "sysDescr/sysObjectID heuristic")
	if objectID != "" {
		inv.Facts["object_id"] = fact(objectID, "1.3.6.1.2.1.1.2.0")
	}

	interfaces := map[string]InventoryRecord{}
	neighbors := map[string]InventoryRecord{}
	entities := map[string]InventoryRecord{}
	addresses := map[string]InventoryRecord{}
	vlans := map[string]InventoryRecord{}
	arp := map[string]InventoryRecord{}
	routes := map[string]InventoryRecord{}
	resources := map[string]InventoryRecord{}
	type tree struct {
		name, root string
		columns    map[int]string
		rows       map[string]InventoryRecord
		neighbor   bool
	}
	trees := []tree{
		{"interfaces", "1.3.6.1.2.1.2.2.1", map[int]string{1: "index", 2: "description", 3: "type", 4: "mtu", 5: "speed_bps", 6: "mac", 7: "admin_state", 8: "oper_state"}, interfaces, false},
		{"interface_names", "1.3.6.1.2.1.31.1.1.1", map[int]string{1: "name", 15: "speed_mbps", 18: "alias"}, interfaces, false},
		{"entities", "1.3.6.1.2.1.47.1.1.1.1", map[int]string{2: "description", 4: "parent_index", 5: "class", 7: "name", 8: "hardware_revision", 9: "firmware", 10: "software_revision", 11: "serial", 12: "manufacturer", 13: "model"}, entities, false},
		{"lldp", "1.0.8802.1.1.2.1.4.1.1", map[int]string{4: "chassis_subtype", 5: "chassis_id", 6: "port_subtype", 7: "port_id", 8: "port_description", 9: "system_name", 10: "system_description"}, neighbors, true},
		{"cdp", "1.3.6.1.4.1.9.9.23.1.2.1.1", map[int]string{4: "address", 5: "software", 6: "device_id", 7: "port_id", 8: "platform"}, neighbors, true},
		{"ipv4_addresses", "1.3.6.1.2.1.4.20.1", map[int]string{1: "address", 2: "interface_index", 3: "netmask"}, addresses, false},
		{"arp", "1.3.6.1.2.1.4.22.1", map[int]string{1: "interface_index", 2: "mac", 3: "address", 4: "type"}, arp, false},
		{"vlans", "1.3.6.1.2.1.17.7.1.4.3.1", map[int]string{1: "name", 5: "status"}, vlans, false},
		{"routes", "1.3.6.1.2.1.4.21.1", map[int]string{1: "destination", 2: "interface_index", 7: "next_hop", 8: "type", 9: "protocol", 11: "netmask"}, routes, false},
		{"cpu", "1.3.6.1.2.1.25.3.3.1", map[int]string{2: "load_percent"}, resources, false},
		{"memory", "1.3.6.1.2.1.25.2.3.1", map[int]string{2: "storage_type", 3: "description", 4: "allocation_unit_bytes", 5: "size_units", 6: "used_units"}, resources, false},
		{"sensors", "1.3.6.1.2.1.99.1.1.1", map[int]string{1: "sensor_type", 2: "scale", 3: "precision", 4: "value", 5: "oper_status", 6: "units_display", 7: "value_uptime_ticks", 8: "update_rate_ms"}, resources, false},
	}
	total := 0
	for _, table := range trees {
		rowsSeen := 0
		valuesSeen := 0
		if ctx.Err() != nil {
			inv.Unavailable = append(inv.Unavailable, table.name+": timeout")
			continue
		}
		walkErr := reader.BulkWalk(table.root, func(p gosnmp.SnmpPDU) error {
			rowsSeen++
			total++
			if rowsSeen > 2048 || total > 8192 || ctx.Err() != nil {
				return errors.New("inventory limit")
			}
			name := strings.TrimPrefix(p.Name, ".")
			if !strings.HasPrefix(name, table.root+".") {
				return nil
			}
			suffix := strings.TrimPrefix(name, table.root+".")
			parts := strings.Split(suffix, ".")
			if len(parts) < 2 {
				return nil
			}
			column, e := strconv.Atoi(parts[0])
			if e != nil {
				return nil
			}
			field, ok := table.columns[column]
			if !ok {
				return nil
			}
			value := pduValue(p, redact)
			if value == nil {
				return nil
			}
			id := strings.Join(parts[1:], ".")
			if table.neighbor {
				if table.name == "lldp" {
					if len(parts) != 4 {
						return nil
					}
					id = strings.Join(parts[2:], ".")
				}
				id = table.name + ":" + id
			} else if table.rows == nil {
				return nil
			}
			if table.name == "cpu" || table.name == "memory" || table.name == "sensors" {
				id = table.name + ":" + id
			}
			row, exists := table.rows[id]
			if !exists {
				if len(table.rows) >= 512 {
					return errors.New("row limit")
				}
				row = InventoryRecord{ID: id, Facts: map[string]Fact{}}
			}
			if field == "mac" {
				if b, ok := p.Value.([]byte); ok && len(b) == 6 {
					value = net.HardwareAddr(b).String()
				}
			}
			if field == "admin_state" || field == "oper_state" {
				value = interfaceState(fmt.Sprint(value), field == "oper_state")
			}
			row.Facts[field] = fact(value, p.Name)
			if table.neighbor {
				row.Facts["discovery_protocol"] = fact(table.name, table.root)
				localPort := parts[1]
				if table.name == "lldp" {
					localPort = parts[2]
				}
				row.Facts["local_port_index"] = fact(localPort, table.root)
			}
			table.rows[id] = row
			valuesSeen++
			addRaw(p)
			return nil
		})
		if walkErr != nil {
			inv.Unavailable = append(inv.Unavailable, table.name+": incomplete (timeout, access, or limit)")
		}
		if valuesSeen == 0 && walkErr == nil {
			inv.Unavailable = append(inv.Unavailable, table.name+": not exposed")
		}
	}
	inv.Interfaces = sortedRecords(interfaces)
	inv.Neighbors = sortedRecords(neighbors)
	inv.Entities = sortedRecords(entities)
	inv.Addresses = sortedRecords(addresses)
	inv.VLANs = sortedRecords(vlans)
	inv.ARP = sortedRecords(arp)
	inv.Routes = sortedRecords(routes)
	inv.Resources = sortedRecords(resources)
	// Device-level serial/firmware come only from an explicitly identified chassis.
	for _, row := range inv.Entities {
		if f, ok := row.Facts["class"]; ok && fmt.Sprint(f.Value) == "3" {
			for _, key := range []string{"serial", "firmware", "model"} {
				if value, ok := row.Facts[key]; ok {
					inv.Facts[key] = value
				}
			}
			break
		}
	}
	inv.Unavailable = append(inv.Unavailable, "HA/cluster role: no adapter", "routing neighbors: no adapter", "config revision/hash: no adapter", "IPv6/ND: no adapter")
	sort.Strings(inv.Unavailable)
	return inv
}

func pduValue(p gosnmp.SnmpPDU, redact func(string) string) any {
	switch p.Type {
	case gosnmp.NoSuchInstance, gosnmp.NoSuchObject, gosnmp.EndOfMibView, gosnmp.Null:
		return nil
	}
	switch v := p.Value.(type) {
	case []byte:
		if len(v) == 0 {
			return nil
		}
		// Redact before any binary encoding; encoding a secret is not redaction.
		normalized := redact(string(v))
		if strings.Contains(normalized, "[redacted]") {
			return normalized
		}
		if !utf8.Valid(v) || strings.ContainsAny(string(v), "\x00\x01\x02\x03\x04\x05\x06\x07\x08") {
			if len(v) > 128 {
				v = v[:128]
			}
			return hex.EncodeToString(v)
		}
		return normalized
	case string:
		if v == "" {
			return nil
		}
		return redact(v)
	case int, uint, uint32, uint64, int32, int64:
		return v
	default:
		return nil
	}
}
func sortedRecords(rows map[string]InventoryRecord) []InventoryRecord {
	out := make([]InventoryRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
func interfaceState(value string, oper bool) string {
	states := map[string]string{"1": "up", "2": "down", "3": "testing"}
	if oper {
		states["4"] = "unknown"
		states["5"] = "dormant"
		states["6"] = "notPresent"
		states["7"] = "lowerLayerDown"
	}
	if s, ok := states[value]; ok {
		return s
	}
	return "unknown"
}
