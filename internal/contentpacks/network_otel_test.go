package contentpacks

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestNetworkSyslogUsesHTTPExporterAndTransportIdentity(t *testing.T) {
	source := testResolvedOTelSource("network.firewall", CollectorSyslog, "syslog", map[string]any{"transport": "udp", "listen_address": "127.0.0.1:5514", "protocol": "rfc3164"})
	plan, err := BuildOTelCollectorConfig([]OTelCollectorConfigSource{{Source: source}}, OTelCollectorConfigOptions{Endpoint: "legacy:4317", NetworkLogsEndpoint: "https://controlone.example/api/v1/content-packs/collectors/edge/network-otlp?tenant_id=tenant", Headers: map[string]string{"X-ControlOne-Collector-Token": "fixture"}})
	require.NoError(t, err)
	require.Equal(t, "none", plan.Config.Exporters["otlphttp/controlone.network"]["compression"])
	for _, pipeline := range plan.Config.Service.Pipelines {
		require.Equal(t, []string{"otlphttp/controlone.network"}, pipeline.Exporters)
	}
	for _, receiver := range plan.Config.Receivers {
		require.Equal(t, true, receiver["udp"].(map[string]any)["add_attributes"])
	}
}

func TestNetworkFlowUsesOfficialDecoderAndHTTPExporter(t *testing.T) {
	for _, scheme := range []string{"netflow", "sflow"} {
		source := testResolvedOTelSource("network."+scheme, CollectorNetFlow, "netflow", map[string]any{"scheme": scheme, "hostname": "127.0.0.1"})
		plan, err := BuildOTelCollectorConfig([]OTelCollectorConfigSource{{Source: source}}, OTelCollectorConfigOptions{Endpoint: "legacy:4317", NetworkLogsEndpoint: "https://controlone.example/network-otlp"})
		require.NoError(t, err)
		for _, pipeline := range plan.Config.Service.Pipelines {
			require.Equal(t, []string{"otlphttp/controlone.network"}, pipeline.Exporters)
		}
		for _, receiver := range plan.Config.Receivers {
			require.Equal(t, scheme, receiver["scheme"])
		}
	}
}
