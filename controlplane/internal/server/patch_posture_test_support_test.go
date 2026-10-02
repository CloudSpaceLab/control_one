package server

import (
	"context"

	"github.com/google/uuid"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// GetPatchPosture keeps the shared server test double aligned with the Store
// contract. Patch-posture behavior is covered by focused patch tests.
func (f *fakeStore) GetPatchPosture(_ context.Context, _ uuid.UUID) (storage.PatchPosture, error) {
	return storage.PatchPosture{}, nil
}
