package onlineapt

import (
	"github.com/CloudSpaceLab/control_one/internal/patchsupport"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestArtifactIdentityBindsBytesWithoutLocalListState(t *testing.T) {
	p := []patchsupport.ExactPackage{{Name: "liba", Version: "2"}}
	files := []fileIdentity{{Path: "archives/liba_2_amd64.deb", SHA256: "archive-a"}, {Path: "lists/release", SHA256: "release-a"}}
	expected := artifactIdentity(p, files)
	files[1].SHA256 = "release-b"
	require.Equal(t, expected, artifactIdentity(p, files))
	files[0].SHA256 = "archive-b"
	require.NotEqual(t, expected, artifactIdentity(p, files))
	files[0].SHA256 = "archive-a"
	p[0].Version = "3"
	require.NotEqual(t, expected, artifactIdentity(p, files))
}
