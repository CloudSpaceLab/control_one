package networkdevice

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// The isolated Net-SNMP fixture validates real USM engine discovery, authPriv,
// GET decoding and explicit unknown-user reports; it is not a physical device.
func TestSNMPv3WithNetSNMP(t *testing.T) {
	if testing.Short() {
		t.Skip("SNMPv3 integration requires Docker")
	}
	ctx := context.Background()
	client, err := testcontainers.NewDockerClientWithOpts(ctx)
	if err != nil {
		t.Skip("Docker unavailable")
	}
	defer client.Close()
	if _, err = client.Ping(ctx); err != nil {
		t.Skip("Docker unavailable")
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{Started: true, ContainerRequest: testcontainers.ContainerRequest{
		Image: "alpine:3.21", ExposedPorts: []string{"161/udp"},
		Env: map[string]string{"SNMP_PERSISTENT_DIR": "/var/lib/net-snmp"},
		Files: []testcontainers.ContainerFile{
			{HostFilePath: "testdata/snmpd.conf", ContainerFilePath: "/etc/snmp/snmpd.conf", FileMode: 0600},
			{HostFilePath: "testdata/snmpd-persistent.conf", ContainerFilePath: "/var/lib/net-snmp/snmpd.conf", FileMode: 0600},
		},
		Cmd:        []string{"sh", "-c", "apk add --no-cache net-snmp && exec snmpd -f -Lo -C -c /var/lib/net-snmp/snmpd.conf,/etc/snmp/snmpd.conf"},
		WaitingFor: wait.ForLog("NET-SNMP version").WithStartupTimeout(2 * time.Minute),
	}})
	require.NoError(t, err)
	defer container.Terminate(ctx)
	host, err := container.Host(ctx)
	require.NoError(t, err)
	port, err := container.MappedPort(ctx, "161/udp")
	require.NoError(t, err)
	credential := Credential{Username: "fixture-readonly", AuthProtocol: "SHA256", AuthSecret: "fixture-auth-secret", PrivProtocol: "AES", PrivSecret: "fixture-priv-secret"}
	result := probeSNMP(ctx, host, port.Int(), credential)
	require.Equal(t, "authenticated", result.State)
	require.Equal(t, "Cisco", result.Vendor)
	require.Equal(t, "C9300-48P", result.Model)
	require.Equal(t, []string{"snmp_identity"}, result.Capabilities)
	credential.Username = "fixture-unknown-user"
	result = probeSNMP(ctx, host, port.Int(), credential)
	require.Equal(t, "auth_failed", result.State)
}
