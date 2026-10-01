package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestAssembleLogDumpChunksVerifiesFinalChecksum(t *testing.T) {
	root := t.TempDir()
	t.Setenv(logDumpDirEnv, root)
	tenantID, nodeID, jobID := uuid.New(), uuid.New(), uuid.New()
	parts := [][]byte{[]byte("{\"line\":1}\n"), []byte("{\"line\":2}\n")}

	makeChunks := func(t *testing.T, dumpID uuid.UUID) []logDumpChunkArtifact {
		t.Helper()
		chunks := make([]logDumpChunkArtifact, 0, len(parts))
		for ordinal, payload := range parts {
			sum := sha256.Sum256(payload)
			path, size, actualSHA, err := writeLogDumpChunk(
				dumpID,
				jobID,
				ordinal,
				bytes.NewReader(payload),
				hex.EncodeToString(sum[:]),
			)
			require.NoError(t, err)
			chunks = append(chunks, logDumpChunkArtifact{
				Ordinal: ordinal, Path: path, SHA256: actualSHA, SizeBytes: size,
			})
		}
		return chunks
	}

	joined := bytes.Join(parts, nil)
	finalSum := sha256.Sum256(joined)
	expectedSHA := hex.EncodeToString(finalSum[:])
	dumpID := uuid.New()
	artifact, err := assembleLogDumpChunks(tenantID, nodeID, dumpID, makeChunks(t, dumpID), expectedSHA)
	require.NoError(t, err)
	require.Equal(t, expectedSHA, artifact.SHA256)
	require.Equal(t, int64(len(joined)), artifact.SizeBytes)
	payload, err := os.ReadFile(artifact.Path)
	require.NoError(t, err)
	require.Equal(t, joined, payload)

	badDumpID := uuid.New()
	_, err = assembleLogDumpChunks(tenantID, nodeID, badDumpID, makeChunks(t, badDumpID), hex.EncodeToString(make([]byte, sha256.Size)))
	require.ErrorContains(t, err, "checksum mismatch")
	_, statErr := os.Stat(logDumpArtifactPathForTest(root, tenantID, nodeID, badDumpID))
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestAssembleLogDumpChunksRejectsMissingOrdinal(t *testing.T) {
	t.Setenv(logDumpDirEnv, t.TempDir())
	_, err := assembleLogDumpChunks(uuid.New(), uuid.New(), uuid.New(), []logDumpChunkArtifact{{
		Ordinal: 1, Path: "/unused", SHA256: "unused", SizeBytes: 1,
	}}, "")
	require.ErrorContains(t, err, "missing log dump chunk 0")
}

func logDumpArtifactPathForTest(root string, tenantID, nodeID, dumpID uuid.UUID) string {
	return root + string(os.PathSeparator) + tenantID.String() +
		string(os.PathSeparator) + nodeID.String() +
		string(os.PathSeparator) + dumpID.String() + ".ndjson"
}
