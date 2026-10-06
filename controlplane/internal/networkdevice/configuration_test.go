package networkdevice

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeConfigurationSnapshotRedactsAndHashesDeterministically(t *testing.T) {
	first, err := NormalizeConfigurationSnapshot("ssh_config/cisco", "management-read/v1", "text", "hostname edge\r\nsnmp-server community TOP_SECRET ro\r\nusername ops secret HASHED\r\nsnmp-server user ops group v3 auth sha AUTH_SENTINEL priv aes 128 PRIV_SENTINEL\r\ntacacs-server key TACACS_SENTINEL\r\ninterface Gi1  \r\n")
	require.NoError(t, err)
	require.NotContains(t, first.Content, "TOP_SECRET")
	require.NotContains(t, first.Content, "HASHED")
	require.NotContains(t, first.Content, "AUTH_SENTINEL")
	require.NotContains(t, first.Content, "PRIV_SENTINEL")
	require.NotContains(t, first.Content, "TACACS_SENTINEL")
	require.Contains(t, first.Content, "hostname edge\n[redacted]\n[redacted]\n[redacted]\n[redacted]\ninterface Gi1")
	second, err := NormalizeConfigurationSnapshot("ssh_config/cisco", "management-read/v1", "text", "hostname edge\nsnmp-server community OTHER_SECRET ro\nusername ops secret DIFFERENT_HASH\nsnmp-server user ops group v3 auth sha OTHER_AUTH priv aes 128 OTHER_PRIV\ntacacs-server key OTHER_TACACS\ninterface Gi1\n")
	require.NoError(t, err)
	require.Equal(t, first.ContentHash, second.ContentHash, "different secrets must not create a configuration revision")
	for _, test := range []struct{ adapter, version, format, content string }{
		{"", "v1", "text", "hostname x"}, {"ssh_config/cisco", "v1", "json", "{"}, {"ssh_config/cisco", "v1", "text", ""}, {"ssh_config/cisco", "v1", "text", string(make([]byte, 16385))},
	} {
		_, err := NormalizeConfigurationSnapshot(test.adapter, test.version, test.format, test.content)
		require.Error(t, err)
	}
}

func TestConfigurationDiffAndPostureAreDeterministicAndEvidenceLinked(t *testing.T) {
	added, removed := ConfigurationDiff("hostname edge\ninterface Gi1\ninterface Gi1\n", "hostname edge\ninterface Gi1\ntransport input telnet\n")
	require.Equal(t, []string{"transport input telnet"}, added)
	require.Equal(t, []string{"interface Gi1"}, removed)
	findings := EvaluateConfigurationPosture("snapshot-123", "ip http server\ntransport input telnet\nsnmp-server community [redacted] RO\n")
	require.Len(t, findings, 11)
	for _, finding := range findings[:3] {
		require.Equal(t, "finding", finding.Status)
		require.Equal(t, "snapshot-123", finding.SnapshotID)
		require.Equal(t, "network_configuration_snapshots:snapshot-123", finding.EvidenceRef)
		require.NotEmpty(t, finding.Evidence)
	}
	for _, finding := range findings[3:] {
		require.Equal(t, "unsupported", finding.Status)
	}
	clean := EvaluateConfigurationPosture("snapshot-124", "hostname edge\n")
	require.Equal(t, "not_observed", clean[0].Status)
}

func TestStructuredConfigurationFormatsAreValidatedAndRedacted(t *testing.T) {
	jsonSnapshot, err := NormalizeConfigurationSnapshot("restconf", "management-read/v1", "json", `{"system":{"hostname":"edge","password":"JSON_SENTINEL"}}`)
	require.NoError(t, err)
	require.NotContains(t, jsonSnapshot.Content, "JSON_SENTINEL")
	require.Contains(t, jsonSnapshot.Content, "[redacted]")

	xmlSnapshot, err := NormalizeConfigurationSnapshot("netconf", "management-read/v1", "xml", `<config><hostname>edge</hostname><community>XML_SENTINEL</community><interface name="eth0" key="ATTRIBUTE_SENTINEL"/></config>`)
	require.NoError(t, err)
	require.NotContains(t, xmlSnapshot.Content, "XML_SENTINEL")
	require.NotContains(t, xmlSnapshot.Content, "ATTRIBUTE_SENTINEL")
	require.Contains(t, xmlSnapshot.Content, "hostname")
	canonicalXML, err := NormalizeConfigurationSnapshot("netconf", "management-read/v1", "xml", `<config>
 <hostname>edge</hostname>
 <community>OTHER_XML_SECRET</community>
 <interface key="OTHER_ATTRIBUTE_SECRET" name="eth0"/>
</config>`)
	require.NoError(t, err)
	require.Equal(t, xmlSnapshot.ContentHash, canonicalXML.ContentHash)

	_, err = NormalizeConfigurationSnapshot("netconf", "management-read/v1", "xml", `<config>`)
	require.Error(t, err)
}

func TestSNMPv3HostDoesNotTriggerWeakCommunityFinding(t *testing.T) {
	findings := EvaluateConfigurationPosture("snapshot-v3", "snmp-server host 192.0.2.1 version 3 priv")
	require.Equal(t, "not_observed", findings[2].Status)
}

func TestUnsupportedFormatsNeverClaimPostureChecksPassed(t *testing.T) {
	findings := EvaluateConfigurationPostureForSnapshot("snapshot-xml", "xml", `<config><community>secret</community></config>`)
	require.Len(t, findings, 11)
	for _, finding := range findings {
		require.NotEqual(t, "pass", finding.Status)
	}
	for _, finding := range findings[:3] {
		require.Equal(t, "unsupported", finding.Status)
	}
}

func TestManagementConfigRejectsCommandTextInsteadOfAllowListingIt(t *testing.T) {
	t.Setenv("CONFIG_READ_PASSWORD", "fixture-only")
	m := ManagementConfig{Adapter: "cisco; configure terminal", Username: "reader", PasswordEnv: "CONFIG_READ_PASSWORD", HostKeyFingerprint: "SHA256:fixture"}
	require.Error(t, m.Validate("ssh_config"))
	_, exists := snapshotCommands[m.Adapter]
	require.False(t, exists, "collector accepts only named fixed read-only adapters")
}
