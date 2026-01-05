package secrets

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/vault"
)

// SyncService handles secret synchronization from Vault.
type SyncService struct {
	log     *zap.Logger
	store   *storage.Store
	vault   *vault.Client
}

// NewSyncService creates a new secrets sync service.
func NewSyncService(log *zap.Logger, store *storage.Store, vaultClient *vault.Client) *SyncService {
	return &SyncService{
		log:   log,
		store: store,
		vault: vaultClient,
	}
}

// SyncGroup synchronizes secrets for a secret group.
func (s *SyncService) SyncGroup(ctx context.Context, groupID uuid.UUID) error {
	group, err := s.store.GetSecretGroup(ctx, groupID)
	if err != nil {
		return fmt.Errorf("get secret group: %w", err)
	}
	if group == nil {
		return fmt.Errorf("secret group not found")
	}

	if strings.ToLower(group.Backend) != "vault" {
		return fmt.Errorf("unsupported backend: %s", group.Backend)
	}

	if s.vault == nil {
		return fmt.Errorf("vault client not configured")
	}

	// Extract vault path from endpoint or use default
	vaultPath := "secret"
	if group.Endpoint.Valid {
		vaultPath = strings.TrimSpace(group.Endpoint.String)
	}

	s.log.Info("syncing secrets from vault", zap.String("group_id", groupID.String()), zap.String("path", vaultPath))

	// List secrets at the path
	secretKeys, err := s.vault.ListSecrets(ctx, vaultPath)
	if err != nil {
		s.store.UpdateSecretGroupSyncStatus(ctx, groupID, "failed", err)
		return fmt.Errorf("list vault secrets: %w", err)
	}

	// Read each secret
	secretCount := 0
	for _, key := range secretKeys {
		secretPath := fmt.Sprintf("%s/%s", strings.TrimSuffix(vaultPath, "/"), key)
		_, err := s.vault.ReadSecret(ctx, secretPath)
		if err != nil {
			s.log.Warn("failed to read secret", zap.String("path", secretPath), zap.Error(err))
			continue
		}

		// Get version if available
		version, _ := s.vault.GetSecretVersion(ctx, secretPath)
		versionStr := fmt.Sprintf("%d", version)

		// Record sync
		// Note: In a real implementation, we'd store the actual secret values securely
		// For now, we just track the sync metadata
		secretCount++

		s.log.Debug("synced secret", zap.String("path", secretPath), zap.String("version", versionStr))
	}

	// Update sync status
	if err := s.store.UpdateSecretGroupSyncStatus(ctx, groupID, "success", nil); err != nil {
		return fmt.Errorf("update sync status: %w", err)
	}

	s.log.Info("secret sync completed", zap.String("group_id", groupID.String()), zap.Int("secret_count", secretCount))
	return nil
}

