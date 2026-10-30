package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/patchsupport"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestSquidConfigActivationRecovery(t *testing.T) {
	for _, scenario := range []string{"parse failure", "reload failure", "readiness failure", "recovery failure", "cancelled request", "success"} {
		t.Run(scenario, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "squid.conf")
			require.NoError(t, os.WriteFile(path, []byte("previous\n"), 0o640))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			reloads, probes := 0, 0
			run := func(runCtx context.Context, args ...string) error {
				if args[1] == "parse" {
					active, err := os.ReadFile(path)
					require.NoError(t, err)
					require.Equal(t, "previous\n", string(active))
					candidate, err := os.ReadFile(args[3])
					require.NoError(t, err)
					require.Equal(t, "candidate\n", string(candidate))
					if scenario == "parse failure" {
						return errors.New("invalid directive")
					}
					return nil
				}
				reloads++
				if scenario == "cancelled request" && reloads == 1 {
					cancel()
					return context.Canceled
				}
				if reloads > 1 {
					require.NoError(t, runCtx.Err())
				}
				if scenario == "recovery failure" || (scenario == "reload failure" && reloads == 1) {
					return errors.New("reload rejected")
				}
				return nil
			}
			ready := func(context.Context) error {
				probes++
				if scenario == "readiness failure" && probes == 1 {
					return errors.New("listener unavailable")
				}
				return nil
			}
			err := replaceSquidConfig(ctx, path, []byte("candidate\n"), run, ready)
			body, readErr := os.ReadFile(path)
			require.NoError(t, readErr)
			if scenario == "success" {
				require.NoError(t, err)
				require.Equal(t, "candidate\n", string(body))
			} else {
				require.Error(t, err)
				require.Equal(t, "previous\n", string(body))
				if scenario == "recovery failure" {
					require.Contains(t, err.Error(), "operator recovery required")
				}
			}
			info, statErr := os.Stat(path)
			require.NoError(t, statErr)
			require.Equal(t, os.FileMode(0o640), info.Mode().Perm())
			files, err := os.ReadDir(filepath.Dir(path))
			require.NoError(t, err)
			require.Len(t, files, 1)
		})
	}
}

func TestSquidConfigRejectsSymlinkWithoutModification(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	link := filepath.Join(root, "squid.conf")
	require.NoError(t, os.WriteFile(target, []byte("previous"), 0o600))
	if err := os.Symlink(target, link); err != nil {
		t.Skip("symlink unavailable")
	}
	err := replaceSquidConfig(context.Background(), link, []byte("candidate"), func(context.Context, ...string) error { t.Fatal("must not run"); return nil }, nil)
	require.Error(t, err)
	body, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, "previous", string(body))
}

func TestSquidDispatchIdentity(t *testing.T) {
	nodeID := uuid.NewString()
	detail := squidActionDetail{JobType: squidJobReconfigure, ProxyID: uuid.NewString(), NodeID: nodeID, Whitelist: []string{"archive.ubuntu.com"}, ExpiresAt: time.Now().Add(time.Minute), RequestedBy: "admin"}
	require.NoError(t, validateSquidDispatchIdentity(squidJobReconfigure, detail, nodeID))
	for _, pair := range [][2]string{{squidJobInstall, nodeID}, {squidJobReconfigure, "other"}, {squidJobReconfigure, ""}, {"unknown", nodeID}} {
		require.Error(t, validateSquidDispatchIdentity(pair[0], detail, pair[1]))
	}
}

func TestNativeSquidCandidateParsing(t *testing.T) {
	if os.Getenv("CONTROLONE_SQUID_NATIVE_ISOLATION") != "1" {
		t.Skip("explicit isolated native Squid fixture required")
	}
	path := filepath.Join(t.TempDir(), "candidate.conf")
	body, err := patchsupport.SquidConfig([]string{"archive.ubuntu.com", "security.ubuntu.com"})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, body, 0o644))
	out, err := exec.Command("squid", "-k", "parse", "-f", path).CombinedOutput()
	require.NoError(t, err, string(out))
	require.NoError(t, os.WriteFile(path, append(body, []byte("controlone_invalid_directive true\n")...), 0o644))
	out, err = exec.Command("squid", "-k", "parse", "-f", path).CombinedOutput()
	require.Error(t, err, string(out))
	t.Log("native Squid accepted the generated configuration and rejected an invalid directive; active configuration was not replaced")
}
