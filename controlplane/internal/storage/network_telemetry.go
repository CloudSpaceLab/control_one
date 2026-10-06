package storage

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"strings"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/networkdevice"
	"github.com/google/uuid"
)

type NetworkSource struct {
	ID                   uuid.UUID  `json:"id"`
	TenantID             uuid.UUID  `json:"tenant_id"`
	TargetID             uuid.UUID  `json:"target_id"`
	SourceType           string     `json:"source_type"`
	CollectorID          string     `json:"collector_id"`
	Site                 string     `json:"site"`
	SenderAddress        string     `json:"sender_address"`
	StaleAfterSeconds    int        `json:"stale_after_seconds"`
	State                string     `json:"state"`
	ObservedAt           *time.Time `json:"observed_at,omitempty"`
	LastContactAt        *time.Time `json:"last_contact_at,omitempty"`
	QueueDepth           int64      `json:"queue_depth"`
	LagMillis            int64      `json:"lag_millis"`
	CollectorStatus      string     `json:"collector_status"`
	CollectorHeartbeatAt *time.Time `json:"collector_heartbeat_at,omitempty"`
}

type NetworkSourceConfig struct {
	SourceType        string `json:"source_type"`
	CollectorID       string `json:"collector_id"`
	Site              string `json:"site"`
	SenderAddress     string `json:"sender_address"`
	StaleAfterSeconds int    `json:"stale_after_seconds"`
}

func (p *NetworkSourceConfig) Validate() error {
	if !networkdevice.IsSourceType(p.SourceType) {
		return errors.New("source adapter is not supported")
	}
	p.CollectorID = strings.TrimSpace(p.CollectorID)
	p.Site = strings.TrimSpace(p.Site)
	if p.CollectorID == "" || len(p.CollectorID) > 255 || len(p.Site) > 255 || strings.ContainsAny(p.CollectorID+p.Site, "\r\n\x00") {
		return errors.New("invalid collector or site")
	}
	if p.StaleAfterSeconds < 60 || p.StaleAfterSeconds > 86400 {
		return errors.New("source freshness must be between 60 and 86400 seconds")
	}
	if networkdevice.IsReceiverSource(p.SourceType) {
		ip := net.ParseIP(p.SenderAddress)
		if ip == nil || ip.IsUnspecified() || ip.IsMulticast() {
			return errors.New("receiver requires a transport sender IP")
		}
		p.SenderAddress = ip.String()
	} else if p.SenderAddress != "" {
		return errors.New("sender address is only used for receiver sources")
	}
	return nil
}

const networkSourceSelect = `SELECT n.id,n.tenant_id,n.target_id,n.source_type,n.collector_id,n.site,n.sender_address,n.stale_after_seconds,n.state,n.observed_at,n.last_contact_at,n.queue_depth,n.lag_millis,c.status,c.last_heartbeat_at FROM network_telemetry_sources n JOIN content_pack_edge_collectors c ON c.tenant_id=n.tenant_id AND c.collector_id=n.collector_id`

func scanNetworkSource(row rowScanner) (*NetworkSource, error) {
	var n NetworkSource
	err := row.Scan(&n.ID, &n.TenantID, &n.TargetID, &n.SourceType, &n.CollectorID, &n.Site, &n.SenderAddress, &n.StaleAfterSeconds, &n.State, &n.ObservedAt, &n.LastContactAt, &n.QueueDepth, &n.LagMillis, &n.CollectorStatus, &n.CollectorHeartbeatAt)
	return &n, err
}

func (s *Store) ListNetworkSources(ctx context.Context, id uuid.UUID, a TargetAccess) ([]NetworkSource, error) {
	if a.Permission != "targets.read" {
		return nil, sql.ErrNoRows
	}
	t, err := s.GetTarget(ctx, id, a)
	if err != nil {
		return nil, err
	}
	if t == nil || t.Family != "network_security" {
		return nil, sql.ErrNoRows
	}
	rows, err := s.db.QueryContext(ctx, networkSourceSelect+` WHERE n.target_id=$1 ORDER BY n.source_type`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []NetworkSource{}
	for rows.Next() {
		n, err := scanNetworkSource(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *n)
	}
	return result, rows.Err()
}

func (s *Store) ConfigureNetworkSource(ctx context.Context, id uuid.UUID, p NetworkSourceConfig, a TargetAccess) error {
	if a.Permission != "targets.write" || a.UserID == uuid.Nil {
		return sql.ErrNoRows
	}
	if err := p.Validate(); err != nil {
		return err
	}
	// Rebinding resets evidence and changes the ID so late reports cannot revive it.
	result, err := s.db.ExecContext(ctx, `INSERT INTO network_telemetry_sources(id,tenant_id,target_id,source_type,collector_id,site,sender_address,stale_after_seconds)
 SELECT $1,t.tenant_id,t.id,$3,$4,$5,$6,$7 FROM targets t JOIN content_pack_edge_collectors c ON c.tenant_id=t.tenant_id AND c.collector_id=$4 AND c.status!='disabled'
 WHERE t.id=$2 AND t.family='network_security' AND t.lifecycle_state='active' AND `+targetAccessPredicate("t.tenant_id", 8, 9)+`
 ON CONFLICT(target_id,source_type) DO UPDATE SET id=EXCLUDED.id,collector_id=EXCLUDED.collector_id,site=EXCLUDED.site,sender_address=EXCLUDED.sender_address,stale_after_seconds=EXCLUDED.stale_after_seconds,state='not_configured',observed_at=NULL,last_contact_at=NULL,queue_depth=0,lag_millis=0`, uuid.New(), id, p.SourceType, p.CollectorID, p.Site, p.SenderAddress, p.StaleAfterSeconds, a.UserID, a.Permission)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) ReportNetworkSources(ctx context.Context, tenant uuid.UUID, collector string, reports []networkdevice.SourceReport) error {
	if len(reports) == 0 || len(reports) > 100 {
		return errors.New("provide 1 to 100 source reports")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	seen := map[uuid.UUID]bool{}
	for _, r := range reports {
		if err := r.Validate(time.Now().UTC()); err != nil {
			return err
		}
		id, err := uuid.Parse(r.BindingID)
		if err != nil || id == uuid.Nil || seen[id] {
			return errors.New("invalid or duplicate binding id")
		}
		seen[id] = true
		var old sql.NullTime
		err = tx.QueryRowContext(ctx, `SELECT n.observed_at FROM network_telemetry_sources n JOIN targets t ON t.id=n.target_id JOIN content_pack_edge_collectors c ON c.tenant_id=n.tenant_id AND c.collector_id=n.collector_id WHERE n.id=$1 AND n.tenant_id=$2 AND n.collector_id=$3 AND t.lifecycle_state='active' AND c.status!='disabled' FOR UPDATE OF n`, id, tenant, collector).Scan(&old)
		if err != nil {
			return err
		}
		if old.Valid && !r.ObservedAt.After(old.Time) {
			continue
		}
		_, err = tx.ExecContext(ctx, `UPDATE network_telemetry_sources SET state=$2,observed_at=$3,last_contact_at=CASE WHEN $4::timestamptz IS NULL THEN last_contact_at WHEN last_contact_at IS NULL OR $4>last_contact_at THEN $4 ELSE last_contact_at END,queue_depth=$5,lag_millis=$6 WHERE id=$1`, id, r.State, r.ObservedAt, r.LastContactAt, r.QueueDepth, r.LagMillis)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ResolveNetworkSyslogSource(ctx context.Context, tenant uuid.UUID, collector, sender string) (*NetworkSource, error) {
	ip := net.ParseIP(sender)
	if ip == nil {
		return nil, sql.ErrNoRows
	}
	return scanNetworkSource(s.db.QueryRowContext(ctx, networkSourceSelect+` JOIN targets t ON t.id=n.target_id WHERE n.tenant_id=$1 AND n.collector_id=$2 AND n.sender_address=$3 AND n.source_type='syslog' AND c.status!='disabled' AND t.lifecycle_state='active'`, tenant, collector, ip.String()))
}

func (s *Store) GetCollectorNetworkSource(ctx context.Context, tenant uuid.UUID, collector string, id uuid.UUID) (*NetworkSource, error) {
	return scanNetworkSource(s.db.QueryRowContext(ctx, networkSourceSelect+` JOIN targets t ON t.id=n.target_id WHERE n.id=$1 AND n.tenant_id=$2 AND n.collector_id=$3 AND c.status!='disabled' AND t.lifecycle_state='active'`, id, tenant, collector))
}

func (s *Store) ResolveNetworkReceiverSource(ctx context.Context, tenant uuid.UUID, collector, sourceType, sender string) (*NetworkSource, error) {
	ip := net.ParseIP(sender)
	if ip == nil || !networkdevice.IsReceiverSource(sourceType) {
		return nil, sql.ErrNoRows
	}
	return scanNetworkSource(s.db.QueryRowContext(ctx, networkSourceSelect+` JOIN targets t ON t.id=n.target_id WHERE n.tenant_id=$1 AND n.collector_id=$2 AND n.sender_address=$3 AND n.source_type=$4 AND c.status!='disabled' AND t.lifecycle_state='active'`, tenant, collector, ip.String(), sourceType))
}

func (s *Store) ListCollectorNetworkSources(ctx context.Context, tenant uuid.UUID, collector string, limit, offset int) ([]NetworkSource, error) {
	if limit < 1 || limit > 100 || offset < 0 {
		return nil, errors.New("invalid binding page")
	}
	rows, err := s.db.QueryContext(ctx, networkSourceSelect+` JOIN targets t ON t.id=n.target_id WHERE n.tenant_id=$1 AND n.collector_id=$2 AND c.status!='disabled' AND t.lifecycle_state='active' ORDER BY n.id LIMIT $3 OFFSET $4`, tenant, collector, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []NetworkSource{}
	for rows.Next() {
		row, err := scanNetworkSource(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *row)
	}
	return result, rows.Err()
}
