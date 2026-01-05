package storage

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateProvisioningTemplate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupPostgresStore(t, ctx)

	template := &ProvisioningTemplate{
		Name:     "test-template",
		Provider: "aws",
		Labels:   map[string]string{"env": "test"},
	}

	created, err := store.CreateProvisioningTemplate(ctx, template)
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, created.ID)
	assert.Equal(t, "test-template", created.Name)
	assert.Equal(t, "aws", created.Provider)
	assert.Equal(t, map[string]string{"env": "test"}, created.Labels)
}

func TestGetProvisioningTemplate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupPostgresStore(t, ctx)

	template := &ProvisioningTemplate{
		Name:     "get-test-template",
		Provider: "azure",
	}
	created, err := store.CreateProvisioningTemplate(ctx, template)
	require.NoError(t, err)

	retrieved, err := store.GetProvisioningTemplate(ctx, created.ID)
	require.NoError(t, err)
	assert.NotNil(t, retrieved)
	assert.Equal(t, created.ID, retrieved.ID)
	assert.Equal(t, "get-test-template", retrieved.Name)
}

func TestListProvisioningTemplates(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupPostgresStore(t, ctx)

	for i := 0; i < 5; i++ {
		template := &ProvisioningTemplate{
			Name:     fmt.Sprintf("list-template-%d", i),
			Provider: "vmware",
		}
		_, err := store.CreateProvisioningTemplate(ctx, template)
		require.NoError(t, err)
	}

	filter := ProvisioningTemplateFilter{
		Provider: "vmware",
	}
	templates, total, err := store.ListProvisioningTemplates(ctx, filter, 10, 0)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, total, 5)
	assert.GreaterOrEqual(t, len(templates), 5)
}

func TestUpdateProvisioningTemplate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupPostgresStore(t, ctx)

	template := &ProvisioningTemplate{
		Name:     "update-test-template",
		Provider: "libvirt",
	}
	created, err := store.CreateProvisioningTemplate(ctx, template)
	require.NoError(t, err)

	desc := "Updated description"
	params := UpdateProvisioningTemplateParams{
		Description: &desc,
	}
	updated, err := store.UpdateProvisioningTemplate(ctx, created.ID, params)
	require.NoError(t, err)
	assert.NotNil(t, updated)
	assert.True(t, updated.Description.Valid)
	assert.Equal(t, desc, updated.Description.String)
}

func TestCreateProvisioningTemplateVersion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupPostgresStore(t, ctx)

	template := &ProvisioningTemplate{
		Name:     "version-test-template",
		Provider: "aws",
	}
	created, err := store.CreateProvisioningTemplate(ctx, template)
	require.NoError(t, err)

	params := CreateTemplateVersionParams{
		TemplateID: created.ID,
		Body:       "---\n# Test template body",
	}
	version, err := store.CreateProvisioningTemplateVersion(ctx, params)
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, version.ID)
	assert.Equal(t, created.ID, version.TemplateID)
	assert.Equal(t, 1, version.Version)
	assert.Equal(t, "---\n# Test template body", version.Body)
}

func TestPromoteProvisioningTemplateVersion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupPostgresStore(t, ctx)

	template := &ProvisioningTemplate{
		Name:     "promote-test-template",
		Provider: "azure",
	}
	created, err := store.CreateProvisioningTemplate(ctx, template)
	require.NoError(t, err)

	params := CreateTemplateVersionParams{
		TemplateID: created.ID,
		Body:       "version 1",
	}
	version1, err := store.CreateProvisioningTemplateVersion(ctx, params)
	require.NoError(t, err)

	promoted, err := store.PromoteProvisioningTemplateVersion(ctx, created.ID, version1.Version)
	require.NoError(t, err)
	assert.NotNil(t, promoted)
	assert.True(t, promoted.PromotedAt.Valid)

	retrieved, err := store.GetProvisioningTemplate(ctx, created.ID)
	require.NoError(t, err)
	assert.NotNil(t, retrieved.PromotedVersionID)
	assert.Equal(t, version1.ID, *retrieved.PromotedVersionID)
}

func TestListProvisioningTemplateVersions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupPostgresStore(t, ctx)

	template := &ProvisioningTemplate{
		Name:     "list-versions-template",
		Provider: "vmware",
	}
	created, err := store.CreateProvisioningTemplate(ctx, template)
	require.NoError(t, err)

	for i := 0; i < 3; i++ {
		params := CreateTemplateVersionParams{
			TemplateID: created.ID,
			Body:       fmt.Sprintf("version %d", i+1),
		}
		_, err := store.CreateProvisioningTemplateVersion(ctx, params)
		require.NoError(t, err)
	}

	versions, total, err := store.ListProvisioningTemplateVersions(ctx, created.ID, 10, 0)
	require.NoError(t, err)
	assert.Equal(t, 3, total)
	assert.Equal(t, 3, len(versions))
}

func TestGetPromotedProvisioningTemplateVersion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupPostgresStore(t, ctx)

	template := &ProvisioningTemplate{
		Name:     "get-promoted-template",
		Provider: "libvirt",
	}
	created, err := store.CreateProvisioningTemplate(ctx, template)
	require.NoError(t, err)

	params := CreateTemplateVersionParams{
		TemplateID: created.ID,
		Body:       "promoted version",
	}
	version, err := store.CreateProvisioningTemplateVersion(ctx, params)
	require.NoError(t, err)

	_, err = store.PromoteProvisioningTemplateVersion(ctx, created.ID, version.Version)
	require.NoError(t, err)

	promoted, err := store.GetPromotedProvisioningTemplateVersion(ctx, created.ID)
	require.NoError(t, err)
	assert.NotNil(t, promoted)
	assert.Equal(t, version.ID, promoted.ID)
	assert.True(t, promoted.PromotedAt.Valid)
}

func TestTemplateVersioning(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupPostgresStore(t, ctx)

	template := &ProvisioningTemplate{
		Name:     "versioning-test-template",
		Provider: "aws",
	}
	created, err := store.CreateProvisioningTemplate(ctx, template)
	require.NoError(t, err)

	versions := []string{"v1", "v2", "v3"}
	for i, body := range versions {
		params := CreateTemplateVersionParams{
			TemplateID: created.ID,
			Body:       body,
		}
		version, err := store.CreateProvisioningTemplateVersion(ctx, params)
		require.NoError(t, err)
		assert.Equal(t, i+1, version.Version)
		assert.Equal(t, body, version.Body)
	}

	allVersions, total, err := store.ListProvisioningTemplateVersions(ctx, created.ID, 10, 0)
	require.NoError(t, err)
	assert.Equal(t, 3, total)
	assert.Equal(t, 3, len(allVersions))

	for i, version := range allVersions {
		assert.Equal(t, 3-i, version.Version)
	}
}

func TestTemplateFiltering(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupPostgresStore(t, ctx)

	providers := []string{"aws", "azure", "vmware"}
	for _, provider := range providers {
		for i := 0; i < 2; i++ {
			template := &ProvisioningTemplate{
				Name:     fmt.Sprintf("%s-template-%d", provider, i),
				Provider: provider,
			}
			_, err := store.CreateProvisioningTemplate(ctx, template)
			require.NoError(t, err)
		}
	}

	filter := ProvisioningTemplateFilter{
		Provider: "aws",
	}
	templates, total, err := store.ListProvisioningTemplates(ctx, filter, 10, 0)
	require.NoError(t, err)
	assert.Equal(t, 2, total)
	assert.Equal(t, 2, len(templates))
	for _, tpl := range templates {
		assert.Equal(t, "aws", tpl.Provider)
	}
}

func TestTemplateArchiving(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := setupPostgresStore(t, ctx)

	template := &ProvisioningTemplate{
		Name:     "archive-test-template",
		Provider: "azure",
	}
	created, err := store.CreateProvisioningTemplate(ctx, template)
	require.NoError(t, err)

	archived := true
	params := UpdateProvisioningTemplateParams{
		Archived: &archived,
	}
	updated, err := store.UpdateProvisioningTemplate(ctx, created.ID, params)
	require.NoError(t, err)
	assert.True(t, updated.ArchivedAt.Valid)

	filter := ProvisioningTemplateFilter{
		IncludeArchived: false,
	}
	templates, _, err := store.ListProvisioningTemplates(ctx, filter, 10, 0)
	require.NoError(t, err)
	for _, tpl := range templates {
		if tpl.ID == created.ID {
			t.Errorf("archived template should not appear in non-archived list")
		}
	}

	filter.IncludeArchived = true
	templates, _, err = store.ListProvisioningTemplates(ctx, filter, 10, 0)
	require.NoError(t, err)
	found := false
	for _, tpl := range templates {
		if tpl.ID == created.ID {
			found = true
			assert.True(t, tpl.ArchivedAt.Valid)
		}
	}
	assert.True(t, found, "archived template should appear in archived list")
}
