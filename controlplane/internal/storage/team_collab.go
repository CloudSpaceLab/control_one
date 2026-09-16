package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// TeamUser is the tenant-scoped member used by the assignee picker and
// mention autosuggest.
type TeamUser struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	Email string    `json:"email,omitempty"`
}

// ListTenantUsers returns tenant members holding investigator/operator/admin
// roles (tenant-scoped or org-global), deduplicated, matched textually on
// prefix, limited to `limit` (<= 0 means 50).
func (s *Store) ListTenantUsers(ctx context.Context, tenantID uuid.UUID, query string, limit int) ([]TeamUser, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if limit <= 0 {
		limit = 50
	}
	pattern := "%" + escapeLike(strings.TrimSpace(query)) + "%"
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT ON (u.id) u.id, u.display_name, u.email
		FROM user_roles ur
		JOIN users u ON u.id = ur.user_id
		JOIN roles r ON r.id = ur.role_id
		WHERE (ur.tenant_id IS NULL OR ur.tenant_id = $1)
		  AND LOWER(r.name) IN ('investigator', 'operator', 'admin')
		  AND (ur.expires_at IS NULL OR ur.expires_at > NOW())
		  AND (LOWER(COALESCE(u.display_name, '')) LIKE $2
		       OR LOWER(COALESCE(u.email, '')) LIKE $2)
		ORDER BY u.id, LOWER(COALESCE(u.display_name, u.email, u.id::text))
		LIMIT $3
	`, tenantID, pattern, limit)
	if err != nil {
		return nil, fmt.Errorf("query tenant users: %w", err)
	}
	defer rows.Close()

	out := make([]TeamUser, 0, limit)
	for rows.Next() {
		var u TeamUser
		var name, email sql.NullString
		if err := rows.Scan(&u.ID, &name, &email); err != nil {
			return nil, fmt.Errorf("scan tenant user: %w", err)
		}
		u.Name = strings.TrimSpace(name.String)
		u.Email = strings.TrimSpace(email.String)
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tenant users: %w", err)
	}
	return out, nil
}
