package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	logspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// OTel's Syslog receiver supplies transport address attributes independently
// of parsed message hostnames. Only those attributes resolve device ownership.
func networkEventsFromOTLP(p *logspb.ExportLogsServiceRequest) ([]networkSyslogEvent, error) {
	events := []networkSyslogEvent{}
	for _, resource := range p.ResourceLogs {
		for _, scope := range resource.ScopeLogs {
			for _, record := range scope.LogRecords {
				if len(events) >= 100 {
					return nil, errors.New("OTLP batch exceeds 100 records")
				}
				sender, sourceType := "", "syslog"
				fields := map[string]any{}
				for _, attribute := range record.Attributes {
					if strings.HasPrefix(attribute.Key, "flow.") || strings.HasPrefix(attribute.Key, "source.") || strings.HasPrefix(attribute.Key, "destination.") || strings.HasPrefix(attribute.Key, "network.") {
						switch v := attribute.Value.GetValue().(type) {
						case *commonpb.AnyValue_StringValue:
							fields[attribute.Key] = v.StringValue
						case *commonpb.AnyValue_IntValue:
							fields[attribute.Key] = v.IntValue
						}
					}
				}
				if kind, ok := fields["flow.type"].(string); ok {
					switch kind {
					case "netflow_v5", "netflow_v9":
						sourceType = "netflow"
					case "ipfix":
						sourceType = "ipfix"
					case "sflow_5":
						sourceType = "sflow"
					default:
						return nil, errors.New("unsupported flow encoding")
					}
					// In the official receiver this attribute is the UDP sampler peer,
					// not the flow source/destination or payload agent address.
					sender, _ = fields["flow.sampler_address"].(string)
				}
				for _, attribute := range record.Attributes {
					if attribute.Key == "net.peer.ip" || attribute.Key == "network.peer.address" {
						sender = attribute.Value.GetStringValue()
						break
					}
				}
				if sender == "" {
					return nil, errors.New("OTLP Syslog requires transport sender address")
				}
				body, ok := record.Body.GetValue().(*commonpb.AnyValue_StringValue)
				if !ok && sourceType == "syslog" {
					return nil, errors.New("OTLP Syslog body must be text")
				}
				observed := record.ObservedTimeUnixNano
				if observed == 0 {
					return nil, errors.New("OTLP Syslog requires receiver observation time")
				}
				message := "Decoded " + sourceType + " flow"
				if sourceType == "syslog" {
					message = body.StringValue
					fields = nil
				}
				events = append(events, networkSyslogEvent{SourceType: sourceType, Fields: fields, SenderAddress: sender, ObservedAt: time.Unix(0, int64(observed)).UTC(), Message: message})
			}
		}
	}
	if len(events) == 0 {
		return nil, errors.New("empty OTLP logs")
	}
	return events, nil
}

func (s *Server) handleNetworkOTLP(w http.ResponseWriter, r *http.Request, collector string) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", 405)
		return
	}
	if _, _, ok := s.authorizeContentPackEdgeCollectorCall(w, r, collector, roleOperator, roleAdmin); !ok {
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		http.Error(w, "OTLP body exceeds limit", 413)
		return
	}
	if r.Header.Get("Content-Encoding") != "" && r.Header.Get("Content-Encoding") != "identity" {
		http.Error(w, "compressed network OTLP is unsupported", 415)
		return
	}
	p := &logspb.ExportLogsServiceRequest{}
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		err = protojson.Unmarshal(raw, p)
	} else if strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-protobuf") {
		err = proto.Unmarshal(raw, p)
	} else {
		http.Error(w, "OTLP JSON or protobuf required", 415)
		return
	}
	if err != nil {
		http.Error(w, "invalid OTLP logs", 400)
		return
	}
	events, err := networkEventsFromOTLP(p)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	canonical, _ := proto.MarshalOptions{Deterministic: true}.Marshal(p)
	sum := sha256.Sum256(canonical)
	payload, _ := json.Marshal(struct {
		Events  []networkSyslogEvent `json:"events"`
		BatchID string               `json:"batch_id"`
	}{events, hex.EncodeToString(sum[:])})
	forwarded := r.Clone(r.Context())
	forwarded.Body = io.NopCloser(bytes.NewReader(payload))
	forwarded.ContentLength = int64(len(payload))
	recorder := &networkOTLPResponse{header: make(http.Header)}
	s.handleNetworkSyslogEvents(recorder, forwarded, collector)
	if recorder.status != 202 {
		for k, vs := range recorder.header {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(recorder.status)
		_, _ = w.Write(recorder.body.Bytes())
		return
	}
	// OTLP exporters require the protocol response, rather than the journal DTO.
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-protobuf") {
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(200)
	} else {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("{}"))
	}
}

type networkOTLPResponse struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (w *networkOTLPResponse) Header() http.Header  { return w.header }
func (w *networkOTLPResponse) WriteHeader(code int) { w.status = code }
func (w *networkOTLPResponse) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = 200
	}
	return w.body.Write(p)
}
