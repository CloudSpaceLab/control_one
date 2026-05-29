package securityschema

import (
	"fmt"
	"net"
	"sort"
	"strings"
)

const (
	SchemaName    = "controlone.security_event"
	SchemaVersion = 1
)

type FieldType string

const (
	TypeString = FieldType("string")
	TypeInt    = FieldType("int")
	TypeBool   = FieldType("bool")
	TypeIP     = FieldType("ip")
	TypeMap    = FieldType("map")
)

type FieldDefinition struct {
	Name        string    `json:"name"`
	Type        FieldType `json:"type"`
	ECSAlias    string    `json:"ecs_alias,omitempty"`
	OCSFObject  string    `json:"ocsf_object,omitempty"`
	UDMAlias    string    `json:"udm_alias,omitempty"`
	Description string    `json:"description,omitempty"`
}

type Violation struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type OCSFEventMapping struct {
	Category string `json:"category_name"`
	Class    string `json:"class_name"`
}

func Dictionary() []FieldDefinition {
	fields := []FieldDefinition{
		{Name: "event.kind", Type: TypeString, ECSAlias: "event.kind", OCSFObject: "metadata", Description: "Event record kind, usually event or alert."},
		{Name: "event.category", Type: TypeString, ECSAlias: "event.category", OCSFObject: "category_name", Description: "High-level event family such as authentication, process, network, or file."},
		{Name: "event.action", Type: TypeString, ECSAlias: "event.action", OCSFObject: "activity_name", Description: "Normalized action inside the event category."},
		{Name: "event.outcome", Type: TypeString, ECSAlias: "event.outcome", OCSFObject: "status", Description: "success, failure, unknown, or equivalent normalized outcome."},
		{Name: "event.code", Type: TypeString, ECSAlias: "event.code", OCSFObject: "type_uid", UDMAlias: "metadata.product_event_type", Description: "Vendor or platform event identifier."},
		{Name: "event.provider", Type: TypeString, ECSAlias: "event.provider", OCSFObject: "metadata.product.name", UDMAlias: "metadata.product_name", Description: "Vendor/product/provider that emitted the event."},
		{Name: "event.dataset", Type: TypeString, ECSAlias: "event.dataset", OCSFObject: "metadata.log_name", UDMAlias: "metadata.log_type", Description: "Source dataset, channel, or log stream."},
		{Name: "host.hostname", Type: TypeString, ECSAlias: "host.hostname", OCSFObject: "device.hostname", UDMAlias: "principal.hostname", Description: "Host that emitted or owns the event."},
		{Name: "user.name", Type: TypeString, ECSAlias: "user.name", OCSFObject: "user.name", UDMAlias: "target.user.userid", Description: "Primary actor or target account."},
		{Name: "user.domain", Type: TypeString, ECSAlias: "user.domain", OCSFObject: "user.domain", Description: "Account domain, realm, or tenant."},
		{Name: "source.user.name", Type: TypeString, ECSAlias: "source.user.name", OCSFObject: "actor.user.name", UDMAlias: "principal.user.userid", Description: "Initiating user when distinct from target user."},
		{Name: "source.ip", Type: TypeIP, ECSAlias: "source.ip", OCSFObject: "src_endpoint.ip", UDMAlias: "principal.ip", Description: "Source endpoint IP address."},
		{Name: "source.port", Type: TypeInt, ECSAlias: "source.port", OCSFObject: "src_endpoint.port", UDMAlias: "principal.port", Description: "Source transport port."},
		{Name: "source.hostname", Type: TypeString, ECSAlias: "source.hostname", OCSFObject: "src_endpoint.hostname", UDMAlias: "principal.hostname", Description: "Source endpoint hostname."},
		{Name: "destination.ip", Type: TypeIP, ECSAlias: "destination.ip", OCSFObject: "dst_endpoint.ip", UDMAlias: "target.ip", Description: "Destination endpoint IP address."},
		{Name: "destination.port", Type: TypeInt, ECSAlias: "destination.port", OCSFObject: "dst_endpoint.port", UDMAlias: "target.port", Description: "Destination transport port."},
		{Name: "destination.user.name", Type: TypeString, ECSAlias: "destination.user.name", OCSFObject: "dst_endpoint.user.name", UDMAlias: "target.user.userid", Description: "Destination or impersonated user where present."},
		{Name: "network.protocol", Type: TypeString, ECSAlias: "network.protocol", OCSFObject: "connection_info.protocol_name", Description: "L4/L7 protocol name."},
		{Name: "process.executable", Type: TypeString, ECSAlias: "process.executable", OCSFObject: "process.file.path", UDMAlias: "target.process.file.full_path", Description: "Process image path."},
		{Name: "process.command_line", Type: TypeString, ECSAlias: "process.command_line", OCSFObject: "process.cmd_line", UDMAlias: "target.process.command_line", Description: "Process command line after policy-approved parsing."},
		{Name: "process.parent.executable", Type: TypeString, ECSAlias: "process.parent.executable", OCSFObject: "actor.process.file.path", UDMAlias: "principal.process.file.full_path", Description: "Parent process image path."},
		{Name: "process.parent.command_line", Type: TypeString, ECSAlias: "process.parent.command_line", OCSFObject: "actor.process.cmd_line", UDMAlias: "principal.process.command_line", Description: "Parent process command line."},
		{Name: "process.pid", Type: TypeString, ECSAlias: "process.pid", OCSFObject: "process.pid", UDMAlias: "target.process.pid", Description: "Process identifier. String preserves Windows hex IDs."},
		{Name: "rule.id", Type: TypeString, ECSAlias: "rule.id", OCSFObject: "rule.uid", UDMAlias: "security_result.rule_id", Description: "Detection or source rule identifier."},
		{Name: "rule.name", Type: TypeString, ECSAlias: "rule.name", OCSFObject: "rule.name", UDMAlias: "security_result.rule_name", Description: "Detection or source rule name."},
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })
	return fields
}

func FieldMap() map[string]FieldDefinition {
	out := map[string]FieldDefinition{}
	for _, field := range Dictionary() {
		out[field.Name] = field
	}
	return out
}

func Validate(fields map[string]any) []Violation {
	definitions := FieldMap()
	var violations []Violation
	for name, def := range definitions {
		value, ok := valueAtPath(fields, name)
		if !ok || value == nil {
			continue
		}
		if message := validateValue(def.Type, value); message != "" {
			violations = append(violations, Violation{Field: name, Message: message})
		}
	}
	sort.Slice(violations, func(i, j int) bool { return violations[i].Field < violations[j].Field })
	return violations
}

func ECSAliases(fields map[string]any) map[string]any {
	out := map[string]any{}
	for _, def := range Dictionary() {
		if strings.TrimSpace(def.ECSAlias) == "" {
			continue
		}
		if value, ok := valueAtPath(fields, def.Name); ok && value != nil {
			out[def.ECSAlias] = value
		}
	}
	return out
}

func OCSFAliases(fields map[string]any) map[string]any {
	out := map[string]any{}
	for _, def := range Dictionary() {
		ocsfObject := strings.TrimSpace(def.OCSFObject)
		if ocsfObject == "" || ocsfObject == "metadata" {
			continue
		}
		if value, ok := valueAtPath(fields, def.Name); ok && value != nil {
			out[ocsfObject] = value
		}
	}
	mapping := DeriveOCSFEventMapping(fields)
	if mapping.Category != "" {
		out["category_name"] = mapping.Category
	}
	if mapping.Class != "" {
		out["class_name"] = mapping.Class
	}
	return out
}

func UDMAliases(fields map[string]any) map[string]any {
	out := map[string]any{}
	for _, def := range Dictionary() {
		if strings.TrimSpace(def.UDMAlias) == "" {
			continue
		}
		if value, ok := valueAtPath(fields, def.Name); ok && value != nil {
			out[def.UDMAlias] = value
		}
	}
	if eventType := UDMEventType(fields); eventType != "" {
		if _, exists := out["metadata.event_type"]; !exists {
			out["metadata.event_type"] = eventType
		}
	}
	return out
}

func DeriveOCSFEventMapping(fields map[string]any) OCSFEventMapping {
	category := lowerField(fields, "event.category")
	action := lowerField(fields, "event.action")
	kind := lowerField(fields, "event.kind")
	ruleID := lowerField(fields, "rule.id")

	switch {
	case category == "authentication" || category == "iam" || category == "identity_access" || containsAny(action, "auth", "logon", "login"):
		return OCSFEventMapping{Category: "identity_access", Class: "authentication"}
	case category == "dns" || containsAny(action, "dns"):
		return OCSFEventMapping{Category: "network_activity", Class: "dns_activity"}
	case category == "network" || category == "network_connection" || category == "firewall" || category == "proxy" || category == "private_access" || containsAny(action, "network_connection", "connection"):
		return OCSFEventMapping{Category: "network_activity", Class: "network_activity"}
	case category == "process" || category == "edr" || containsAny(action, "process", "powershell"):
		return OCSFEventMapping{Category: "system_activity", Class: "process_activity"}
	case category == "file" || containsAny(action, "file_"):
		return OCSFEventMapping{Category: "system_activity", Class: "file_activity"}
	case category == "mail" || category == "email" || category == "email_activity":
		return OCSFEventMapping{Category: "email_activity", Class: "email_activity"}
	case kind == "alert" || ruleID != "":
		return OCSFEventMapping{Category: "findings", Class: "detection_finding"}
	default:
		return OCSFEventMapping{Category: "application_activity", Class: "application_event"}
	}
}

func UDMEventType(fields map[string]any) string {
	category := lowerField(fields, "event.category")
	action := lowerField(fields, "event.action")
	switch {
	case category == "authentication" || strings.Contains(action, "logon") || strings.Contains(action, "login"):
		return "USER_LOGIN"
	case category == "dns":
		return "NETWORK_DNS"
	case category == "network" || category == "network_connection" || strings.Contains(action, "network_connection"):
		return "NETWORK_CONNECTION"
	case strings.Contains(action, "process_end") || strings.Contains(action, "process_termination"):
		return "PROCESS_TERMINATION"
	case category == "process" || strings.Contains(action, "process_start") || strings.Contains(action, "powershell"):
		return "PROCESS_LAUNCH"
	default:
		return "GENERIC_EVENT"
	}
}

func lowerField(fields map[string]any, path string) string {
	value, ok := valueAtPath(fields, path)
	if !ok || value == nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(fmt.Sprint(value)))
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func valueAtPath(fields map[string]any, path string) (any, bool) {
	if len(fields) == 0 {
		return nil, false
	}
	if value, ok := fields[path]; ok {
		return value, true
	}
	parts := strings.Split(path, ".")
	var current any = fields
	for _, part := range parts {
		obj, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		value, ok := obj[part]
		if !ok {
			return nil, false
		}
		current = value
	}
	return current, true
}

func validateValue(fieldType FieldType, value any) string {
	switch fieldType {
	case TypeString:
		if _, ok := value.(string); !ok {
			return fmt.Sprintf("must be string, got %T", value)
		}
	case TypeInt:
		switch value.(type) {
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		default:
			return fmt.Sprintf("must be integer, got %T", value)
		}
	case TypeBool:
		if _, ok := value.(bool); !ok {
			return fmt.Sprintf("must be bool, got %T", value)
		}
	case TypeIP:
		s, ok := value.(string)
		if !ok {
			return fmt.Sprintf("must be IP string, got %T", value)
		}
		if net.ParseIP(strings.TrimSpace(s)) == nil {
			return "must be a valid IP address"
		}
	case TypeMap:
		if _, ok := value.(map[string]any); !ok {
			return fmt.Sprintf("must be object, got %T", value)
		}
	}
	return ""
}
