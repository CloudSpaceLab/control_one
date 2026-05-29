package contentpacks

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

var (
	syslog3164RE = regexp.MustCompile(`^<(?P<priority>\d{1,3})>(?P<timestamp>[A-Z][a-z]{2}\s+\d{1,2}\s+\d{2}:\d{2}:\d{2})\s+(?P<hostname>\S+)\s+(?P<body>.*)$`)
	syslog5424RE = regexp.MustCompile(`^<(?P<priority>\d{1,3})>(?P<version>\d+)\s+(?P<timestamp>\S+)\s+(?P<hostname>\S+)\s+(?P<app_name>\S+)\s+(?P<proc_id>\S+)\s+(?P<msg_id>\S+)\s+(?P<rest>.*)$`)
	syslogAppRE  = regexp.MustCompile(`^(?P<app_name>[A-Za-z0-9_.@/-]+)(?:\[(?P<proc_id>[^\]]+)\])?:\s*(?P<message>.*)$`)
)

type syslogStageRuntime struct {
	stageType string
}

func (r syslogStageRuntime) Type() string { return r.stageType }

func (r syslogStageRuntime) Compile(stage ParserStage) (CompiledParserStage, error) {
	return syslogCompiledStage{stageType: r.stageType, sourceField: stringConfig(stage.Config, "source_field")}, nil
}

type syslogCompiledStage struct {
	stageType   string
	sourceField string
}

func (s syslogCompiledStage) Type() string { return s.stageType }

func (s syslogCompiledStage) Apply(event *ParserEvent) error {
	input := stageInput(event, s.sourceField)
	if input == "" {
		return fmt.Errorf("empty syslog input")
	}
	if s.stageType == StageSyslogRFC5424 {
		return parseSyslog5424(event, input)
	}
	return parseSyslog3164(event, input)
}

func parseSyslog3164(event *ParserEvent, input string) error {
	fields, ok := namedMatch(syslog3164RE, input)
	if !ok {
		return fmt.Errorf("RFC3164 syslog pattern did not match")
	}
	setSyslogPriority(event, fields["priority"])
	setField(event.Fields, "syslog.timestamp", strings.TrimSpace(fields["timestamp"]))
	setField(event.Fields, "host.hostname", fields["hostname"])
	if ts, err := parseRFC3164Time(fields["timestamp"], event.Timestamp); err == nil {
		event.Timestamp = ts
	}
	body := strings.TrimSpace(fields["body"])
	if app, ok := namedMatch(syslogAppRE, body); ok {
		setField(event.Fields, "process.name", dashToEmpty(app["app_name"]))
		setField(event.Fields, "process.pid", dashToEmpty(app["proc_id"]))
		setField(event.Fields, "message", app["message"])
	} else {
		setField(event.Fields, "message", body)
	}
	return nil
}

func parseSyslog5424(event *ParserEvent, input string) error {
	fields, ok := namedMatch(syslog5424RE, input)
	if !ok {
		return fmt.Errorf("RFC5424 syslog pattern did not match")
	}
	setSyslogPriority(event, fields["priority"])
	setField(event.Fields, "syslog.version", fields["version"])
	setField(event.Fields, "host.hostname", dashToEmpty(fields["hostname"]))
	setField(event.Fields, "process.name", dashToEmpty(fields["app_name"]))
	setField(event.Fields, "process.pid", dashToEmpty(fields["proc_id"]))
	setField(event.Fields, "syslog.msg_id", dashToEmpty(fields["msg_id"]))
	if ts, err := time.Parse(time.RFC3339Nano, fields["timestamp"]); err == nil {
		event.Timestamp = ts.UTC()
		setField(event.Fields, "syslog.timestamp", ts.UTC().Format(time.RFC3339Nano))
	} else {
		setField(event.Fields, "syslog.timestamp", dashToEmpty(fields["timestamp"]))
	}
	structured, message := splitRFC5424Rest(fields["rest"])
	setField(event.Fields, "syslog.structured_data", dashToEmpty(structured))
	setField(event.Fields, "message", message)
	return nil
}

type cefStageRuntime struct{}

func (cefStageRuntime) Type() string { return StageCEF }

func (cefStageRuntime) Compile(stage ParserStage) (CompiledParserStage, error) {
	return cefCompiledStage{sourceField: stringConfig(stage.Config, "source_field")}, nil
}

type cefCompiledStage struct {
	sourceField string
}

func (s cefCompiledStage) Type() string { return StageCEF }

func (s cefCompiledStage) Apply(event *ParserEvent) error {
	input := stageInput(event, s.sourceField)
	parts := splitEscapedLimit(input, '|', 8)
	if len(parts) < 8 || !strings.HasPrefix(parts[0], "CEF:") {
		return fmt.Errorf("CEF input must have 8 header fields")
	}
	setField(event.Fields, "cef.version", strings.TrimPrefix(parts[0], "CEF:"))
	setField(event.Fields, "observer.vendor", unescapeCEF(parts[1]))
	setField(event.Fields, "observer.product", unescapeCEF(parts[2]))
	setField(event.Fields, "observer.version", unescapeCEF(parts[3]))
	setField(event.Fields, "rule.id", unescapeCEF(parts[4]))
	setField(event.Fields, "rule.name", unescapeCEF(parts[5]))
	setField(event.Fields, "event.severity", unescapeCEF(parts[6]))
	for key, value := range parseSeparatedKeyValues(parts[7], ' ') {
		setField(event.Fields, "cef.extensions."+key, unescapeCEF(value))
	}
	normalizeCEFCarrierFields(event)
	return nil
}

type leefStageRuntime struct{}

func (leefStageRuntime) Type() string { return StageLEEF }

func (leefStageRuntime) Compile(stage ParserStage) (CompiledParserStage, error) {
	return leefCompiledStage{sourceField: stringConfig(stage.Config, "source_field")}, nil
}

type leefCompiledStage struct {
	sourceField string
}

func (s leefCompiledStage) Type() string { return StageLEEF }

func (s leefCompiledStage) Apply(event *ParserEvent) error {
	input := stageInput(event, s.sourceField)
	parts := strings.Split(input, "|")
	if len(parts) < 6 || !strings.HasPrefix(parts[0], "LEEF:") {
		return fmt.Errorf("LEEF input must include header and extension fields")
	}
	setField(event.Fields, "leef.version", strings.TrimPrefix(parts[0], "LEEF:"))
	setField(event.Fields, "observer.vendor", parts[1])
	setField(event.Fields, "observer.product", parts[2])
	setField(event.Fields, "observer.version", parts[3])
	setField(event.Fields, "event.code", parts[4])
	delimiter := "\t"
	extensionIndex := 5
	if strings.HasPrefix(parts[0], "LEEF:2.") && len(parts) >= 7 {
		delimiter = parts[5]
		extensionIndex = 6
	}
	extensions := strings.Join(parts[extensionIndex:], "|")
	for key, value := range parseLEEFExtensions(extensions, delimiter) {
		setField(event.Fields, "leef.extensions."+key, value)
	}
	normalizeLEEFCarrierFields(event)
	return nil
}

func normalizeCEFCarrierFields(event *ParserEvent) {
	setFieldIfAbsent(event.Fields, "event.kind", "event")
	copyStringField(event.Fields, "cef.extensions.src", "source.ip")
	copyStringField(event.Fields, "cef.extensions.dst", "destination.ip")
	copyIntField(event.Fields, "cef.extensions.spt", "source.port")
	copyIntField(event.Fields, "cef.extensions.dpt", "destination.port")
	copyStringField(event.Fields, "cef.extensions.proto", "network.protocol")
	copyStringField(event.Fields, "cef.extensions.act", "event.action")
	copyStringField(event.Fields, "cef.extensions.cat", "event.category")
	copyStringField(event.Fields, "cef.extensions.suser", "user.name")
	copyStringField(event.Fields, "cef.extensions.duser", "destination.user.name")
	if _, ok := getField(event.Fields, "event.action"); !ok {
		copyStringField(event.Fields, "rule.name", "event.action")
	}
	setOutcomeFromAction(event.Fields)
	if vendor, ok := getField(event.Fields, "observer.vendor"); ok {
		if product, productOK := getField(event.Fields, "observer.product"); productOK {
			setFieldIfAbsent(event.Fields, "event.provider", strings.TrimSpace(fmt.Sprint(vendor))+"/"+strings.TrimSpace(fmt.Sprint(product)))
		}
	}
}

func normalizeLEEFCarrierFields(event *ParserEvent) {
	setFieldIfAbsent(event.Fields, "event.kind", "event")
	copyStringField(event.Fields, "leef.extensions.src", "source.ip")
	copyStringField(event.Fields, "leef.extensions.dst", "destination.ip")
	copyIntField(event.Fields, "leef.extensions.srcPort", "source.port")
	copyIntField(event.Fields, "leef.extensions.dstPort", "destination.port")
	copyStringField(event.Fields, "leef.extensions.proto", "network.protocol")
	copyStringField(event.Fields, "leef.extensions.usrName", "user.name")
	copyStringField(event.Fields, "leef.extensions.cat", "event.category")
	copyStringField(event.Fields, "leef.extensions.sev", "event.severity")
	copyStringField(event.Fields, "leef.extensions.action", "event.action")
	if _, ok := getField(event.Fields, "event.action"); !ok {
		copyStringField(event.Fields, "event.code", "event.action")
	}
	setOutcomeFromAction(event.Fields)
	if vendor, ok := getField(event.Fields, "observer.vendor"); ok {
		if product, productOK := getField(event.Fields, "observer.product"); productOK {
			setFieldIfAbsent(event.Fields, "event.provider", strings.TrimSpace(fmt.Sprint(vendor))+"/"+strings.TrimSpace(fmt.Sprint(product)))
		}
	}
}

func setOutcomeFromAction(fields map[string]any) {
	if _, ok := getField(fields, "event.outcome"); ok {
		return
	}
	value, ok := getField(fields, "event.action")
	if !ok {
		return
	}
	action := strings.ToLower(strings.TrimSpace(fmt.Sprint(value)))
	if action == "" {
		return
	}
	switch {
	case strings.Contains(action, "allow"), strings.Contains(action, "accept"), strings.Contains(action, "permit"), strings.Contains(action, "success"):
		setFieldIfAbsent(fields, "event.outcome", "success")
	case strings.Contains(action, "deny"), strings.Contains(action, "denied"), strings.Contains(action, "block"), strings.Contains(action, "drop"), strings.Contains(action, "fail"), strings.Contains(action, "reject"), strings.Contains(action, "reset"):
		setFieldIfAbsent(fields, "event.outcome", "failure")
	}
}

func copyStringField(fields map[string]any, source, target string) {
	value, ok := getField(fields, source)
	if !ok {
		return
	}
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" || text == "-" {
		return
	}
	setFieldIfAbsent(fields, target, text)
}

func copyIntField(fields map[string]any, source, target string) {
	value, ok := getField(fields, source)
	if !ok {
		return
	}
	raw := strings.TrimSpace(fmt.Sprint(value))
	if raw == "" || raw == "-" {
		return
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return
	}
	setFieldIfAbsent(fields, target, parsed)
}

func setFieldIfAbsent(fields map[string]any, path string, value any) {
	if _, ok := getField(fields, path); ok {
		return
	}
	setField(fields, path, value)
}

type grokStageRuntime struct{}

func (grokStageRuntime) Type() string { return StageGrok }

func (grokStageRuntime) Compile(stage ParserStage) (CompiledParserStage, error) {
	pattern := stringConfig(stage.Config, "pattern")
	if pattern == "" {
		return nil, fmt.Errorf("pattern is required")
	}
	compiledPattern, fieldNames, err := compileGrokPattern(pattern)
	if err != nil {
		return nil, err
	}
	re, err := regexp.Compile(compiledPattern)
	if err != nil {
		return nil, err
	}
	return grokCompiledStage{pattern: re, fieldNames: fieldNames, sourceField: stringConfig(stage.Config, "source_field")}, nil
}

type grokCompiledStage struct {
	pattern     *regexp.Regexp
	fieldNames  map[string]string
	sourceField string
}

func (s grokCompiledStage) Type() string { return StageGrok }

func (s grokCompiledStage) Apply(event *ParserEvent) error {
	input := stageInput(event, s.sourceField)
	matches := s.pattern.FindStringSubmatch(input)
	if len(matches) == 0 {
		return fmt.Errorf("grok pattern did not match")
	}
	names := s.pattern.SubexpNames()
	for i := 1; i < len(matches) && i < len(names); i++ {
		group := names[i]
		field := s.fieldNames[group]
		if field != "" {
			setField(event.Fields, field, matches[i])
		}
	}
	return nil
}

type kvStageRuntime struct {
	stageType string
}

func (r kvStageRuntime) Type() string { return r.stageType }

func (r kvStageRuntime) Compile(stage ParserStage) (CompiledParserStage, error) {
	delimiter := ' '
	if raw := stringConfig(stage.Config, "delimiter"); raw != "" {
		delimiter = []rune(raw)[0]
	}
	return kvCompiledStage{
		stageType:    r.stageType,
		sourceField:  stringConfig(stage.Config, "source_field"),
		targetPrefix: stringConfig(stage.Config, "target_prefix"),
		delimiter:    delimiter,
	}, nil
}

type kvCompiledStage struct {
	stageType    string
	sourceField  string
	targetPrefix string
	delimiter    rune
}

func (s kvCompiledStage) Type() string { return s.stageType }

func (s kvCompiledStage) Apply(event *ParserEvent) error {
	input := stageInput(event, s.sourceField)
	values := parseSeparatedKeyValues(input, s.delimiter)
	if len(values) == 0 {
		return fmt.Errorf("no key/value pairs parsed")
	}
	for key, value := range values {
		setField(event.Fields, s.targetPrefix+key, value)
	}
	return nil
}

type xmlStageRuntime struct{}

func (xmlStageRuntime) Type() string { return StageXML }

func (xmlStageRuntime) Compile(stage ParserStage) (CompiledParserStage, error) {
	targetField := stringConfig(stage.Config, "target_field")
	if targetField == "" {
		targetField = "xml"
	}
	return xmlCompiledStage{sourceField: stringConfig(stage.Config, "source_field"), targetField: targetField}, nil
}

type xmlCompiledStage struct {
	sourceField string
	targetField string
}

func (s xmlCompiledStage) Type() string { return StageXML }

func (s xmlCompiledStage) Apply(event *ParserEvent) error {
	input := stageInput(event, s.sourceField)
	node, err := parseXMLNode(input)
	if err != nil {
		return err
	}
	setField(event.Fields, s.targetField, map[string]any{node.Name.Local: xmlNodeToMap(node)})
	return nil
}

type windowsEventDataStageRuntime struct{}

func (windowsEventDataStageRuntime) Type() string { return StageWindowsEventData }

func (windowsEventDataStageRuntime) Compile(stage ParserStage) (CompiledParserStage, error) {
	return windowsEventDataCompiledStage{sourceField: stringConfig(stage.Config, "source_field")}, nil
}

type windowsEventDataCompiledStage struct {
	sourceField string
}

func (s windowsEventDataCompiledStage) Type() string { return StageWindowsEventData }

func (s windowsEventDataCompiledStage) Apply(event *ParserEvent) error {
	var decoded any
	if s.sourceField != "" {
		value, ok := getField(event.Fields, s.sourceField)
		if !ok {
			return fmt.Errorf("source field %q not found", s.sourceField)
		}
		decoded = value
	} else {
		raw := strings.TrimSpace(event.Raw)
		if strings.HasPrefix(raw, "<") {
			node, err := parseXMLNode(raw)
			if err != nil {
				return err
			}
			decoded = xmlNodeToMap(node)
		} else {
			if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
				return err
			}
		}
	}
	flattenWindowsEventData(event, decoded)
	return nil
}

type timestampStageRuntime struct{}

func (timestampStageRuntime) Type() string { return StageTimestamp }

func (timestampStageRuntime) Compile(stage ParserStage) (CompiledParserStage, error) {
	layouts := stringSliceConfig(stage.Config, "layouts")
	if len(layouts) == 0 {
		layouts = []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05", "Jan _2 15:04:05"}
	}
	return timestampCompiledStage{
		sourceField: stringConfig(stage.Config, "source_field"),
		targetField: stringConfig(stage.Config, "target_field"),
		layouts:     layouts,
	}, nil
}

type timestampCompiledStage struct {
	sourceField string
	targetField string
	layouts     []string
}

func (s timestampCompiledStage) Type() string { return StageTimestamp }

func (s timestampCompiledStage) Apply(event *ParserEvent) error {
	value, ok := getField(event.Fields, s.sourceField)
	if s.sourceField == "" {
		value = event.Raw
		ok = strings.TrimSpace(event.Raw) != ""
	}
	if !ok {
		return fmt.Errorf("timestamp source not found")
	}
	ts, err := parseTimestampValue(value, s.layouts, event.Timestamp)
	if err != nil {
		return err
	}
	event.Timestamp = ts
	if s.targetField != "" {
		setField(event.Fields, s.targetField, ts.Format(time.RFC3339Nano))
	}
	return nil
}

type xmlNode struct {
	Name     xml.Name
	Attrs    []xml.Attr `xml:",any,attr"`
	Content  string     `xml:",chardata"`
	Children []xmlNode  `xml:",any"`
}

func stageInput(event *ParserEvent, sourceField string) string {
	if sourceField == "" {
		return strings.TrimSpace(event.Raw)
	}
	value, ok := getField(event.Fields, sourceField)
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func namedMatch(re *regexp.Regexp, input string) (map[string]string, bool) {
	matches := re.FindStringSubmatch(input)
	if len(matches) == 0 {
		return nil, false
	}
	out := map[string]string{}
	names := re.SubexpNames()
	for i := 1; i < len(matches) && i < len(names); i++ {
		if names[i] != "" {
			out[names[i]] = matches[i]
		}
	}
	return out, true
}

func setSyslogPriority(event *ParserEvent, raw string) {
	priority, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return
	}
	setField(event.Fields, "syslog.priority", priority)
	setField(event.Fields, "syslog.facility", priority/8)
	setField(event.Fields, "syslog.severity_code", priority%8)
}

func parseRFC3164Time(raw string, base time.Time) (time.Time, error) {
	parsed, err := time.ParseInLocation("Jan _2 15:04:05", strings.TrimSpace(raw), time.UTC)
	if err != nil {
		return time.Time{}, err
	}
	year := base.UTC().Year()
	if year == 1 {
		year = time.Now().UTC().Year()
	}
	return time.Date(year, parsed.Month(), parsed.Day(), parsed.Hour(), parsed.Minute(), parsed.Second(), 0, time.UTC), nil
}

func splitRFC5424Rest(rest string) (string, string) {
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return "", ""
	}
	if strings.HasPrefix(rest, "-") {
		return "-", strings.TrimSpace(strings.TrimPrefix(rest, "-"))
	}
	if !strings.HasPrefix(rest, "[") {
		return "", rest
	}
	depth := 0
	for i, r := range rest {
		switch r {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return rest[:i+1], strings.TrimSpace(rest[i+1:])
			}
		}
	}
	return rest, ""
}

func dashToEmpty(value string) string {
	if strings.TrimSpace(value) == "-" {
		return ""
	}
	return value
}

func splitEscapedLimit(input string, sep rune, limit int) []string {
	var parts []string
	var b strings.Builder
	escaped := false
	for _, r := range input {
		if escaped {
			b.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			b.WriteRune(r)
			continue
		}
		if r == sep && (limit <= 0 || len(parts) < limit-1) {
			parts = append(parts, b.String())
			b.Reset()
			continue
		}
		b.WriteRune(r)
	}
	parts = append(parts, b.String())
	return parts
}

func unescapeCEF(value string) string {
	replacer := strings.NewReplacer(`\|`, "|", `\\`, `\`, `\=`, "=", `\n`, "\n", `\r`, "\r")
	return replacer.Replace(value)
}

func parseSeparatedKeyValues(input string, delimiter rune) map[string]string {
	tokens := splitTokens(input, delimiter)
	out := map[string]string{}
	for _, token := range tokens {
		key, value, ok := strings.Cut(token, "=")
		if !ok || strings.TrimSpace(key) == "" {
			continue
		}
		out[strings.TrimSpace(key)] = trimQuotes(value)
	}
	return out
}

func splitTokens(input string, delimiter rune) []string {
	var out []string
	var b strings.Builder
	quote := rune(0)
	escaped := false
	for _, r := range input {
		if escaped {
			b.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				b.WriteRune(r)
			}
			continue
		}
		if r == '"' || r == '\'' {
			quote = r
			continue
		}
		if r == delimiter || (delimiter == ' ' && unicode.IsSpace(r)) {
			if strings.TrimSpace(b.String()) != "" {
				out = append(out, b.String())
			}
			b.Reset()
			continue
		}
		b.WriteRune(r)
	}
	if strings.TrimSpace(b.String()) != "" {
		out = append(out, b.String())
	}
	return out
}

func trimQuotes(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 {
		if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
			return value[1 : len(value)-1]
		}
	}
	return value
}

func parseLEEFExtensions(input, delimiter string) map[string]string {
	if delimiter == "" {
		delimiter = "\t"
	}
	out := map[string]string{}
	for _, token := range strings.Split(input, delimiter) {
		key, value, ok := strings.Cut(token, "=")
		if !ok || strings.TrimSpace(key) == "" {
			continue
		}
		out[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return out
}

func compileGrokPattern(pattern string) (string, map[string]string, error) {
	fields := map[string]string{}
	var out strings.Builder
	groupID := 0
	for {
		start := strings.Index(pattern, "%{")
		if start < 0 {
			out.WriteString(pattern)
			break
		}
		out.WriteString(pattern[:start])
		end := strings.Index(pattern[start:], "}")
		if end < 0 {
			return "", nil, fmt.Errorf("unterminated grok token")
		}
		token := pattern[start+2 : start+end]
		pattern = pattern[start+end+1:]
		parts := strings.SplitN(token, ":", 2)
		name := strings.TrimSpace(parts[0])
		regex := grokBuiltinPattern(name)
		if regex == "" {
			return "", nil, fmt.Errorf("unsupported grok pattern %q", name)
		}
		if len(parts) == 2 && strings.TrimSpace(parts[1]) != "" {
			group := fmt.Sprintf("grok_%d", groupID)
			groupID++
			fields[group] = strings.TrimSpace(parts[1])
			out.WriteString("(?P<" + group + ">" + regex + ")")
		} else {
			out.WriteString("(?:" + regex + ")")
		}
	}
	return out.String(), fields, nil
}

func grokBuiltinPattern(name string) string {
	switch strings.ToUpper(strings.TrimSpace(name)) {
	case "DATA":
		return `.*?`
	case "GREEDYDATA":
		return `.*`
	case "WORD":
		return `\b\w+\b`
	case "USERNAME", "USER":
		return `[A-Za-z0-9._-]+`
	case "INT":
		return `[+-]?\d+`
	case "NUMBER":
		return `[+-]?(?:\d+(?:\.\d+)?|\.\d+)`
	case "IP", "IPV4":
		return `(?:\d{1,3}\.){3}\d{1,3}`
	case "HOSTNAME":
		return `[A-Za-z0-9][A-Za-z0-9.-]*`
	case "NOTSPACE":
		return `\S+`
	case "SPACE":
		return `\s*`
	case "QS", "QUOTEDSTRING":
		return `"[^"]*"`
	default:
		return ""
	}
}

func parseXMLNode(input string) (xmlNode, error) {
	decoder := xml.NewDecoder(strings.NewReader(strings.TrimSpace(input)))
	var root xmlNode
	var stack []*xmlNode
	for {
		token, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return xmlNode{}, err
		}
		switch typed := token.(type) {
		case xml.StartElement:
			node := xmlNode{Name: typed.Name, Attrs: typed.Attr}
			if len(stack) == 0 {
				root = node
				stack = append(stack, &root)
				continue
			}
			parent := stack[len(stack)-1]
			parent.Children = append(parent.Children, node)
			stack = append(stack, &parent.Children[len(parent.Children)-1])
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].Content += string([]byte(typed))
			}
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	if root.Name.Local == "" {
		return xmlNode{}, fmt.Errorf("XML root element missing")
	}
	return root, nil
}

func xmlNodeToMap(node xmlNode) map[string]any {
	out := map[string]any{}
	for _, attr := range node.Attrs {
		out["@"+attr.Name.Local] = attr.Value
	}
	text := strings.TrimSpace(node.Content)
	if text != "" {
		out["#text"] = text
	}
	for _, child := range node.Children {
		value := xmlNodeToMap(child)
		if len(value) == 1 {
			if textValue, ok := value["#text"]; ok {
				value = map[string]any{"#text": textValue}
			}
		}
		existing, ok := out[child.Name.Local]
		if !ok {
			out[child.Name.Local] = value
			continue
		}
		if list, ok := existing.([]any); ok {
			out[child.Name.Local] = append(list, value)
		} else {
			out[child.Name.Local] = []any{existing, value}
		}
	}
	return out
}

func flattenWindowsEventData(event *ParserEvent, decoded any) {
	obj, ok := decoded.(map[string]any)
	if !ok {
		return
	}
	if eventObj, ok := lookupMap(obj, "Event"); ok {
		obj = eventObj
	}
	if system, ok := lookupMap(obj, "System"); ok {
		copyWindowsFields(event, system, "windows.system")
	}
	if eventData, ok := lookupAny(obj, "EventData"); ok {
		copyWindowsEventData(event, eventData)
	}
	if userData, ok := lookupAny(obj, "UserData"); ok {
		setField(event.Fields, "windows.user_data", userData)
	}
	normalizeWindowsEventFields(event)
}

func copyWindowsFields(event *ParserEvent, values map[string]any, prefix string) {
	for key, value := range values {
		field := prefix + "." + strings.ToLower(strings.TrimPrefix(key, "@"))
		setField(event.Fields, field, value)
	}
}

func copyWindowsEventData(event *ParserEvent, data any) {
	switch typed := data.(type) {
	case map[string]any:
		if list, ok := lookupAny(typed, "Data"); ok {
			copyWindowsEventData(event, list)
			return
		}
		for key, value := range typed {
			setField(event.Fields, "windows.event_data."+key, value)
		}
	case []any:
		for _, item := range typed {
			if obj, ok := item.(map[string]any); ok {
				name := firstStringValue(obj, "Name", "@Name", "name", "@name")
				value, _ := lookupAny(obj, "Value")
				if value == nil {
					value, _ = lookupAny(obj, "#text")
				}
				if name != "" {
					setField(event.Fields, "windows.event_data."+name, value)
				}
			}
		}
	}
}

func normalizeWindowsEventFields(event *ParserEvent) {
	setFieldIfAbsent(event.Fields, "event.kind", "event")
	setFieldIfAbsent(event.Fields, "event.module", "windows")
	if code := parserFieldString(event, "windows.system.eventid"); code != "" {
		setFieldIfAbsent(event.Fields, "event.code", code)
	}
	if computer := parserFieldString(event, "windows.system.computer"); computer != "" {
		setFieldIfAbsent(event.Fields, "host.hostname", computer)
	}
	provider := windowsProviderName(event)
	if provider != "" {
		setFieldIfAbsent(event.Fields, "event.provider", provider)
	}
	if channel := parserFieldString(event, "windows.system.channel"); channel != "" {
		setFieldIfAbsent(event.Fields, "event.dataset", channel)
	}
	copyWindowsAccountAliases(event)
	copyWindowsProcessAliases(event)
	copyWindowsNetworkAliases(event)
	copyWindowsPowerShellAliases(event)
	applyWindowsEventSemantics(event, provider)
}

func copyWindowsAccountAliases(event *ParserEvent) {
	if user := windowsEventDataString(event, "TargetUserName", "AccountName", "User"); user != "" && user != "-" {
		setFieldIfAbsent(event.Fields, "user.name", user)
	}
	if domain := windowsEventDataString(event, "TargetDomainName", "AccountDomain"); domain != "" && domain != "-" {
		setFieldIfAbsent(event.Fields, "user.domain", domain)
	}
	if subject := windowsEventDataString(event, "SubjectUserName"); subject != "" && subject != "-" {
		setFieldIfAbsent(event.Fields, "source.user.name", subject)
	}
	if subjectDomain := windowsEventDataString(event, "SubjectDomainName"); subjectDomain != "" && subjectDomain != "-" {
		setFieldIfAbsent(event.Fields, "source.user.domain", subjectDomain)
	}
}

func copyWindowsProcessAliases(event *ParserEvent) {
	if image := windowsEventDataString(event, "NewProcessName", "ProcessName", "Image"); image != "" && image != "-" {
		setFieldIfAbsent(event.Fields, "process.executable", image)
	}
	if commandLine := windowsEventDataString(event, "CommandLine", "ProcessCommandLine"); commandLine != "" && commandLine != "-" {
		setFieldIfAbsent(event.Fields, "process.command_line", commandLine)
	}
	if parent := windowsEventDataString(event, "ParentProcessName", "ParentImage"); parent != "" && parent != "-" {
		setFieldIfAbsent(event.Fields, "process.parent.executable", parent)
	}
	if pid := windowsEventDataString(event, "NewProcessId", "ProcessId", "ProcessID"); pid != "" && pid != "-" {
		setFieldIfAbsent(event.Fields, "process.pid", pid)
	}
}

func copyWindowsNetworkAliases(event *ParserEvent) {
	if ip := windowsEventDataString(event, "IpAddress", "SourceIp", "SourceAddress", "SourceIP"); ip != "" && ip != "-" {
		setFieldIfAbsent(event.Fields, "source.ip", ip)
	}
	if port := windowsEventDataString(event, "IpPort", "SourcePort"); port != "" && port != "-" {
		setIntFieldIfAbsent(event.Fields, "source.port", port)
	}
	if ip := windowsEventDataString(event, "DestinationIp", "DestinationIP", "DestIp", "DestIP"); ip != "" && ip != "-" {
		setFieldIfAbsent(event.Fields, "destination.ip", ip)
	}
	if port := windowsEventDataString(event, "DestinationPort", "DestPort"); port != "" && port != "-" {
		setIntFieldIfAbsent(event.Fields, "destination.port", port)
	}
	if host := windowsEventDataString(event, "WorkstationName", "SourceHostname"); host != "" && host != "-" {
		setFieldIfAbsent(event.Fields, "source.hostname", host)
	}
}

func copyWindowsPowerShellAliases(event *ParserEvent) {
	if script := windowsEventDataString(event, "ScriptBlockText", "Command"); script != "" && script != "-" {
		setFieldIfAbsent(event.Fields, "process.command_line", script)
	}
}

func applyWindowsEventSemantics(event *ParserEvent, provider string) {
	code := parserFieldString(event, "event.code")
	switch code {
	case "4624":
		setFieldIfAbsent(event.Fields, "event.category", "authentication")
		setFieldIfAbsent(event.Fields, "event.action", "logon_success")
		setFieldIfAbsent(event.Fields, "event.outcome", "success")
	case "4625":
		setFieldIfAbsent(event.Fields, "event.category", "authentication")
		setFieldIfAbsent(event.Fields, "event.action", "logon_failure")
		setFieldIfAbsent(event.Fields, "event.outcome", "failure")
	case "4634", "4647":
		setFieldIfAbsent(event.Fields, "event.category", "authentication")
		setFieldIfAbsent(event.Fields, "event.action", "logoff")
		setFieldIfAbsent(event.Fields, "event.outcome", "success")
	case "4688":
		setFieldIfAbsent(event.Fields, "event.category", "process")
		setFieldIfAbsent(event.Fields, "event.action", "process_start")
		setFieldIfAbsent(event.Fields, "event.outcome", "success")
	case "4689":
		setFieldIfAbsent(event.Fields, "event.category", "process")
		setFieldIfAbsent(event.Fields, "event.action", "process_end")
		setFieldIfAbsent(event.Fields, "event.outcome", "success")
	case "1":
		if isSysmonProvider(provider) {
			setFieldIfAbsent(event.Fields, "event.category", "process")
			setFieldIfAbsent(event.Fields, "event.action", "process_start")
			setFieldIfAbsent(event.Fields, "event.outcome", "success")
		}
	case "3":
		if isSysmonProvider(provider) {
			setFieldIfAbsent(event.Fields, "event.category", "network")
			setFieldIfAbsent(event.Fields, "event.action", "network_connection")
			setFieldIfAbsent(event.Fields, "event.outcome", "success")
		}
	case "4103":
		if isPowerShellProvider(provider) {
			setFieldIfAbsent(event.Fields, "event.category", "process")
			setFieldIfAbsent(event.Fields, "event.action", "powershell_pipeline")
			setFieldIfAbsent(event.Fields, "event.outcome", "success")
		}
	case "4104":
		if isPowerShellProvider(provider) {
			setFieldIfAbsent(event.Fields, "event.category", "process")
			setFieldIfAbsent(event.Fields, "event.action", "powershell_script_block")
			setFieldIfAbsent(event.Fields, "event.outcome", "success")
		}
	}
}

func windowsProviderName(event *ParserEvent) string {
	if raw, ok := getField(event.Fields, "windows.system.provider"); ok {
		switch provider := raw.(type) {
		case string:
			return strings.TrimSpace(provider)
		case map[string]any:
			return firstStringValue(provider, "Name", "@Name", "name", "@name", "#text")
		}
	}
	return ""
}

func windowsEventDataString(event *ParserEvent, keys ...string) string {
	for _, key := range keys {
		if value := parserFieldString(event, "windows.event_data."+key); value != "" {
			return value
		}
	}
	return ""
}

func parserFieldString(event *ParserEvent, path string) string {
	value, ok := getField(event.Fields, path)
	if !ok {
		return ""
	}
	return scalarString(value)
}

func scalarString(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strings.TrimSpace(strconv.FormatFloat(typed, 'f', -1, 64))
	case float32:
		as64 := float64(typed)
		if as64 == float64(int64(as64)) {
			return strconv.FormatInt(int64(as64), 10)
		}
		return strings.TrimSpace(strconv.FormatFloat(as64, 'f', -1, 32))
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func setIntFieldIfAbsent(fields map[string]any, path, value string) {
	if _, exists := getField(fields, path); exists {
		return
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return
	}
	setField(fields, path, parsed)
}

func isSysmonProvider(provider string) bool {
	provider = strings.ToLower(strings.TrimSpace(provider))
	return strings.Contains(provider, "sysmon")
}

func isPowerShellProvider(provider string) bool {
	provider = strings.ToLower(strings.TrimSpace(provider))
	return strings.Contains(provider, "powershell")
}

func lookupAny(values map[string]any, keys ...string) (any, bool) {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			return value, true
		}
	}
	for existingKey, value := range values {
		for _, key := range keys {
			if strings.EqualFold(existingKey, key) {
				return value, true
			}
		}
	}
	return nil, false
}

func lookupMap(values map[string]any, keys ...string) (map[string]any, bool) {
	value, ok := lookupAny(values, keys...)
	if !ok {
		return nil, false
	}
	obj, ok := value.(map[string]any)
	return obj, ok
}

func firstStringValue(values map[string]any, keys ...string) string {
	value, ok := lookupAny(values, keys...)
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func parseTimestampValue(value any, layouts []string, base time.Time) (time.Time, error) {
	switch typed := value.(type) {
	case time.Time:
		return typed.UTC(), nil
	case json.Number:
		return timestampFromNumber(typed.String())
	case float64:
		return timestampFromUnixFloat(typed), nil
	case int64:
		return time.Unix(typed, 0).UTC(), nil
	case int:
		return time.Unix(int64(typed), 0).UTC(), nil
	}
	raw := strings.TrimSpace(fmt.Sprint(value))
	if raw == "" {
		return time.Time{}, fmt.Errorf("empty timestamp")
	}
	if n, err := strconv.ParseFloat(raw, 64); err == nil {
		return timestampFromUnixFloat(n), nil
	}
	for _, layout := range layouts {
		if ts, err := time.Parse(layout, raw); err == nil {
			if layout == "Jan _2 15:04:05" {
				year := base.UTC().Year()
				if year == 1 {
					year = time.Now().UTC().Year()
				}
				return time.Date(year, ts.Month(), ts.Day(), ts.Hour(), ts.Minute(), ts.Second(), 0, time.UTC), nil
			}
			return ts.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("timestamp %q did not match configured layouts", raw)
}

func timestampFromNumber(raw string) (time.Time, error) {
	n, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return time.Time{}, err
	}
	return timestampFromUnixFloat(n), nil
}

func timestampFromUnixFloat(n float64) time.Time {
	seconds := int64(n)
	nanos := int64((n - float64(seconds)) * 1e9)
	return time.Unix(seconds, nanos).UTC()
}
