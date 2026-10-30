package offlineapt

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestOfflineDisconnectedRoutes(t *testing.T) {
	private := "Iface Destination Gateway Flags RefCnt Use Metric Mask MTU Window IRTT\neth0 000013AC 00000000 0001 0 0 0 0000FFFF 0 0 0\n"
	require.NoError(t, checkRoutes(private, ""))
	require.ErrorContains(t, checkRoutes(private+"eth0 00000000 010013AC 0003 0 0 0 00000000 0 0 0\n", ""), "default route")
	require.ErrorContains(t, checkRoutes(private+"eth0 00080808 00000000 0001 0 0 0 00FFFFFF 0 0 0\n", ""), "public network")
	require.Error(t, checkRoutes("invalid routing evidence", ""))
	require.ErrorContains(t, checkRoutes(private, "00000000000000000000000000000000 00 00000000000000000000000000000000 00 00000000000000000000000000000001 00000000 00000000 00000000 00000001 eth0"), "default route")
	require.NoError(t, checkRoutes(private, "00000000000000000000000000000000 00 00000000000000000000000000000000 00 00000000000000000000000000000000 ffffffff 00000001 00000000 00200200 lo"))
}
