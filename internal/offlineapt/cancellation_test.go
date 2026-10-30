package offlineapt

import (
	"context"
	"errors"
	"github.com/CloudSpaceLab/control_one/internal/patchsupport"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOfflinePreparationCancellationPreservesSelection(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	base := t.TempDir()
	pointer := filepath.Join(base, publishedPlanFile)
	require.NoError(t, os.WriteFile(pointer, []byte("previous selection"), 0600))
	_, err := StageContext(ctx, Spec{WorkRoot: base}, nil)
	require.True(t, errors.Is(err, context.Canceled))
	err = PublishSelectionContext(ctx, base, patchsupport.OfflineContent{})
	require.ErrorIs(t, err, context.Canceled)
	body, err := os.ReadFile(pointer)
	require.NoError(t, err)
	require.Equal(t, "previous selection", string(body))
	_, err = (contextReader{ctx: ctx, reader: strings.NewReader("content")}).Read(make([]byte, 8))
	require.ErrorIs(t, err, context.Canceled)
}
