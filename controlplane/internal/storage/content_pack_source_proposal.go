package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/CloudSpaceLab/control_one/internal/connectordiscovery"
)

const (
	ContentPackSourceProposalStatusProposed         = "proposed"
	ContentPackSourceProposalStatusAutoEligible     = "auto_eligible"
	ContentPackSourceProposalStatusApprovalRequired = "approval_required"
	ContentPackSourceProposalStatusApproved         = "approved"
	ContentPackSourceProposalStatusRejected         = "rejected"
	ContentPackSourceProposalStatusPrivacyBlocked   = "privacy_blocked"
	ContentPackSourceProposalStatusStale            = "stale"

	ContentPackSourceProposalCollectModeObserveOnly   = "observe_only"
	ContentPackSourceProposalCollectModeMetadataOnly  = "metadata_only"
	ContentPackSourceProposalCollectModeCollectParsed = "collect_parsed"
	ContentPackSourceProposalCollectModeCollectRaw    = "collect_raw"
	ContentPackSourceProposalCollectModeDisabled      = "disabled"
)

type ContentPackSourceProposalRecord struct {
	ID                  uuid.UUID
	TenantID            uuid.UUID
	NodeID              uuid.UUID
	ProposalID          string
	Kind                string
	Program             string
	SourceID            string
	CollectorType       string
	Formatter           string
	Status              string
	Confidence          int
	Risk                string
	AutoConnectEligible bool
	RequiresApproval    bool
	Reason              string
	Paths               []string
	Evidence            []string
	Labels              map[string]string
	FirstSeenAt         time.Time
	LastSeenAt          time.Time
	ApprovedBySubject   string
	ApprovedAt          *time.Time
	ApprovalNote        string
	CollectMode         string
	RejectedBySubject   string
	RejectedAt          *time.Time
	RejectionReason     string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type UpsertContentPackSourceProposalsParams struct {
	TenantID  uuid.UUID
	NodeID    uuid.UUID
	Proposals []connectordiscovery.Proposal
}

type ApproveContentPackSourceProposalParams struct {
	TenantID          uuid.UUID
	ProposalID        uuid.UUID
	ApprovedBySubject string
	ApprovalNote      string
	CollectMode       string
}

type RejectContentPackSourceProposalParams struct {
	TenantID          uuid.UUID
	ProposalID        uuid.UUID
	RejectedBySubject string
	RejectionReason   string
	PrivacyBlocked    bool
}

type ContentPackSourceProposalFilter struct {
	Query    string
	Statuses []string
}

type ContentPackSourceProposalSummary struct {
	Total    int
	ByStatus map[string]int
}

func (s *Store) UpsertContentPackSourceProposals(ctx context.Context, p UpsertContentPackSourceProposalsParams) ([]ContentPackSourceProposalRecord, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if p.TenantID == uuid.Nil {
		return nil, errors.New("tenant id is required")
	}
	if p.NodeID == uuid.Nil {
		return nil, errors.New("node id is required")
	}
	records := make([]ContentPackSourceProposalRecord, 0, len(p.Proposals))
	for _, proposal := range p.Proposals {
		record, ok, err := normalizeContentPackSourceProposal(p.TenantID, p.NodeID, proposal)
		if err != nil {
			return nil, err
		}
		if ok {
			records = append(records, record)
		}
	}
	if len(records) == 0 {
		return nil, nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin source proposal upsert transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	out := make([]ContentPackSourceProposalRecord, 0, len(records))
	for _, record := range records {
		labels, err := marshalContentPackStringMap(record.Labels)
		if err != nil {
			return nil, err
		}
		row := tx.QueryRowContext(ctx, `
			INSERT INTO content_pack_source_proposals (
				tenant_id, node_id, proposal_id, kind, program, source_id, collector_type,
				formatter, status, confidence, risk, auto_connect_eligible, requires_approval,
				reason, paths, evidence, labels, first_seen_at, last_seen_at, created_at, updated_at
			)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17::jsonb,NOW(),NOW(),NOW(),NOW())
			ON CONFLICT (tenant_id, node_id, proposal_id) DO UPDATE
			SET kind = EXCLUDED.kind,
			    program = EXCLUDED.program,
			    source_id = EXCLUDED.source_id,
			    collector_type = EXCLUDED.collector_type,
			    formatter = EXCLUDED.formatter,
			    status = CASE
			        WHEN content_pack_source_proposals.status IN ('approved', 'rejected', 'privacy_blocked') THEN content_pack_source_proposals.status
			        ELSE EXCLUDED.status
			    END,
			    confidence = EXCLUDED.confidence,
			    risk = EXCLUDED.risk,
			    auto_connect_eligible = EXCLUDED.auto_connect_eligible,
			    requires_approval = EXCLUDED.requires_approval,
			    reason = EXCLUDED.reason,
			    paths = EXCLUDED.paths,
			    evidence = EXCLUDED.evidence,
			    labels = EXCLUDED.labels,
			    last_seen_at = NOW(),
			    updated_at = NOW()
			RETURNING `+contentPackSourceProposalSelectColumns,
			record.TenantID,
			record.NodeID,
			record.ProposalID,
			record.Kind,
			record.Program,
			record.SourceID,
			record.CollectorType,
			record.Formatter,
			record.Status,
			record.Confidence,
			record.Risk,
			record.AutoConnectEligible,
			record.RequiresApproval,
			record.Reason,
			pq.Array(record.Paths),
			pq.Array(record.Evidence),
			labels,
		)
		saved, err := scanContentPackSourceProposal(row)
		if err != nil {
			return nil, fmt.Errorf("upsert source proposal %s: %w", record.ProposalID, err)
		}
		out = append(out, *saved)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit source proposal upsert transaction: %w", err)
	}
	return out, nil
}

func (s *Store) ListContentPackSourceProposals(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]ContentPackSourceProposalRecord, int, error) {
	return s.ListContentPackSourceProposalsFiltered(ctx, tenantID, ContentPackSourceProposalFilter{}, limit, offset)
}

func (s *Store) ListContentPackSourceProposalsFiltered(ctx context.Context, tenantID uuid.UUID, filter ContentPackSourceProposalFilter, limit, offset int) ([]ContentPackSourceProposalRecord, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil {
		return nil, 0, errors.New("tenant id is required")
	}
	if limit <= 0 {
		limit = 100
	}
	if offset < 0 {
		return nil, 0, errors.New("offset must be non-negative")
	}
	query := strings.ToLower(strings.TrimSpace(filter.Query))
	pattern := "%" + query + "%"
	statuses, err := normalizeContentPackSourceProposalStatusFilter(filter.Statuses)
	if err != nil {
		return nil, 0, err
	}
	var total int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM content_pack_source_proposals
		WHERE tenant_id = $1
		  AND (
		    $2 = ''
		    OR LOWER(id::text) LIKE $3
		    OR LOWER(node_id::text) LIKE $3
		    OR LOWER(proposal_id) LIKE $3
		    OR LOWER(kind) LIKE $3
		    OR LOWER(program) LIKE $3
		    OR LOWER(COALESCE(source_id, '')) LIKE $3
		    OR LOWER(COALESCE(collector_type, '')) LIKE $3
		    OR LOWER(COALESCE(formatter, '')) LIKE $3
		    OR LOWER(status) LIKE $3
		    OR LOWER(COALESCE(risk, '')) LIKE $3
		    OR LOWER(COALESCE(reason, '')) LIKE $3
		    OR LOWER(COALESCE(approved_by_subject, '')) LIKE $3
		    OR LOWER(COALESCE(approval_note, '')) LIKE $3
		    OR LOWER(COALESCE(rejected_by_subject, '')) LIKE $3
		    OR LOWER(COALESCE(rejection_reason, '')) LIKE $3
		    OR LOWER(paths::text) LIKE $3
		    OR LOWER(evidence::text) LIKE $3
		    OR LOWER(labels::text) LIKE $3
		  )
		  AND (cardinality($4::text[]) = 0 OR status = ANY($4::text[]))
	`, tenantID, query, pattern, pq.Array(statuses)).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count content pack source proposals: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+contentPackSourceProposalSelectColumns+`
		FROM content_pack_source_proposals
		WHERE tenant_id = $1
		  AND (
		    $2 = ''
		    OR LOWER(id::text) LIKE $3
		    OR LOWER(node_id::text) LIKE $3
		    OR LOWER(proposal_id) LIKE $3
		    OR LOWER(kind) LIKE $3
		    OR LOWER(program) LIKE $3
		    OR LOWER(COALESCE(source_id, '')) LIKE $3
		    OR LOWER(COALESCE(collector_type, '')) LIKE $3
		    OR LOWER(COALESCE(formatter, '')) LIKE $3
		    OR LOWER(status) LIKE $3
		    OR LOWER(COALESCE(risk, '')) LIKE $3
		    OR LOWER(COALESCE(reason, '')) LIKE $3
		    OR LOWER(COALESCE(approved_by_subject, '')) LIKE $3
		    OR LOWER(COALESCE(approval_note, '')) LIKE $3
		    OR LOWER(COALESCE(rejected_by_subject, '')) LIKE $3
		    OR LOWER(COALESCE(rejection_reason, '')) LIKE $3
		    OR LOWER(paths::text) LIKE $3
		    OR LOWER(evidence::text) LIKE $3
		    OR LOWER(labels::text) LIKE $3
		  )
		  AND (cardinality($4::text[]) = 0 OR status = ANY($4::text[]))
		ORDER BY last_seen_at DESC, updated_at DESC
		LIMIT $5 OFFSET $6
	`, tenantID, query, pattern, pq.Array(statuses), limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("query content pack source proposals: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]ContentPackSourceProposalRecord, 0, limit)
	for rows.Next() {
		record, err := scanContentPackSourceProposal(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *record)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

func (s *Store) ContentPackSourceProposalSummaryFiltered(ctx context.Context, tenantID uuid.UUID, filter ContentPackSourceProposalFilter) (ContentPackSourceProposalSummary, error) {
	if s.db == nil {
		return ContentPackSourceProposalSummary{}, errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil {
		return ContentPackSourceProposalSummary{}, errors.New("tenant id is required")
	}
	query := strings.ToLower(strings.TrimSpace(filter.Query))
	pattern := "%" + query + "%"
	statuses, err := normalizeContentPackSourceProposalStatusFilter(filter.Statuses)
	if err != nil {
		return ContentPackSourceProposalSummary{}, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT status, COUNT(*)
		FROM content_pack_source_proposals
		WHERE tenant_id = $1
		  AND (
		    $2 = ''
		    OR LOWER(id::text) LIKE $3
		    OR LOWER(node_id::text) LIKE $3
		    OR LOWER(proposal_id) LIKE $3
		    OR LOWER(kind) LIKE $3
		    OR LOWER(program) LIKE $3
		    OR LOWER(COALESCE(source_id, '')) LIKE $3
		    OR LOWER(COALESCE(collector_type, '')) LIKE $3
		    OR LOWER(COALESCE(formatter, '')) LIKE $3
		    OR LOWER(status) LIKE $3
		    OR LOWER(COALESCE(risk, '')) LIKE $3
		    OR LOWER(COALESCE(reason, '')) LIKE $3
		    OR LOWER(COALESCE(approved_by_subject, '')) LIKE $3
		    OR LOWER(COALESCE(approval_note, '')) LIKE $3
		    OR LOWER(COALESCE(rejected_by_subject, '')) LIKE $3
		    OR LOWER(COALESCE(rejection_reason, '')) LIKE $3
		    OR LOWER(paths::text) LIKE $3
		    OR LOWER(evidence::text) LIKE $3
		    OR LOWER(labels::text) LIKE $3
		  )
		  AND (cardinality($4::text[]) = 0 OR status = ANY($4::text[]))
		GROUP BY status
	`, tenantID, query, pattern, pq.Array(statuses))
	if err != nil {
		return ContentPackSourceProposalSummary{}, fmt.Errorf("summarize content pack source proposals: %w", err)
	}
	defer func() { _ = rows.Close() }()

	summary := ContentPackSourceProposalSummary{ByStatus: map[string]int{}}
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return ContentPackSourceProposalSummary{}, err
		}
		summary.ByStatus[status] = count
		summary.Total += count
	}
	if err := rows.Err(); err != nil {
		return ContentPackSourceProposalSummary{}, err
	}
	return summary, nil
}

func normalizeContentPackSourceProposalStatusFilter(statuses []string) ([]string, error) {
	if len(statuses) == 0 {
		return []string{}, nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(statuses))
	for _, status := range statuses {
		status = strings.ToLower(strings.TrimSpace(status))
		if status == "" || status == "all" {
			continue
		}
		if !validContentPackSourceProposalStatus(status) {
			return nil, fmt.Errorf("unsupported source proposal status filter %q", status)
		}
		if _, ok := seen[status]; ok {
			continue
		}
		seen[status] = struct{}{}
		out = append(out, status)
	}
	return out, nil
}

func validContentPackSourceProposalStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case ContentPackSourceProposalStatusProposed,
		ContentPackSourceProposalStatusAutoEligible,
		ContentPackSourceProposalStatusApprovalRequired,
		ContentPackSourceProposalStatusApproved,
		ContentPackSourceProposalStatusRejected,
		ContentPackSourceProposalStatusPrivacyBlocked,
		ContentPackSourceProposalStatusStale:
		return true
	default:
		return false
	}
}

func (s *Store) ListContentPackSourceProposalsByIDs(ctx context.Context, tenantID uuid.UUID, proposalIDs []uuid.UUID) ([]ContentPackSourceProposalRecord, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil {
		return nil, errors.New("tenant id is required")
	}
	ids := normalizeContentPackSourceProposalUUIDs(proposalIDs)
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+contentPackSourceProposalSelectColumns+`
		FROM content_pack_source_proposals
		WHERE tenant_id = $1
		  AND id = ANY($2)
		ORDER BY last_seen_at DESC, updated_at DESC
	`, tenantID, pq.Array(ids))
	if err != nil {
		return nil, fmt.Errorf("query content pack source proposals by ids: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]ContentPackSourceProposalRecord, 0, len(ids))
	for rows.Next() {
		record, err := scanContentPackSourceProposal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) ListApprovedContentPackSourceProposalsForNode(ctx context.Context, tenantID, nodeID uuid.UUID, limit int) ([]ContentPackSourceProposalRecord, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil {
		return nil, errors.New("tenant id is required")
	}
	if nodeID == uuid.Nil {
		return nil, errors.New("node id is required")
	}
	if limit <= 0 || limit > 256 {
		limit = 128
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+contentPackSourceProposalSelectColumns+`
		FROM content_pack_source_proposals
		WHERE tenant_id = $1
		  AND node_id = $2
		  AND status = $3
		  AND kind = $4
		  AND (collector_type = '' OR collector_type = $5)
		  AND (collect_mode = '' OR collect_mode IN ($7, $8))
		ORDER BY approved_at DESC NULLS LAST, updated_at DESC
		LIMIT $6
	`, tenantID, nodeID, ContentPackSourceProposalStatusApproved, connectordiscovery.KindLocalLog, connectordiscovery.CollectorTypeFile, limit, ContentPackSourceProposalCollectModeCollectRaw, ContentPackSourceProposalCollectModeCollectParsed)
	if err != nil {
		return nil, fmt.Errorf("query approved source proposals for node: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]ContentPackSourceProposalRecord, 0, limit)
	for rows.Next() {
		record, err := scanContentPackSourceProposal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func normalizeContentPackSourceProposalUUIDs(values []uuid.UUID) []uuid.UUID {
	seen := map[uuid.UUID]struct{}{}
	out := make([]uuid.UUID, 0, len(values))
	for _, value := range values {
		if value == uuid.Nil {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func (s *Store) ApproveContentPackSourceProposal(ctx context.Context, p ApproveContentPackSourceProposalParams) (*ContentPackSourceProposalRecord, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if p.TenantID == uuid.Nil {
		return nil, errors.New("tenant id is required")
	}
	if p.ProposalID == uuid.Nil {
		return nil, errors.New("source proposal id is required")
	}
	approvedBy := strings.TrimSpace(p.ApprovedBySubject)
	if approvedBy == "" {
		return nil, errors.New("approved by subject is required")
	}
	collectMode, err := normalizeContentPackSourceProposalCollectMode(p.CollectMode)
	if err != nil {
		return nil, err
	}
	row := s.db.QueryRowContext(ctx, `
		UPDATE content_pack_source_proposals
		SET status = 'approved',
		    approved_by_subject = $3,
		    approved_at = NOW(),
		    approval_note = $4,
		    collect_mode = $5,
		    rejected_by_subject = '',
		    rejected_at = NULL,
		    rejection_reason = '',
		    updated_at = NOW()
		WHERE tenant_id = $1
		  AND id = $2
		  AND status IN ('proposed', 'auto_eligible', 'approval_required', 'stale')
		RETURNING `+contentPackSourceProposalSelectColumns,
		p.TenantID,
		p.ProposalID,
		approvedBy,
		strings.TrimSpace(p.ApprovalNote),
		collectMode,
	)
	record, err := scanContentPackSourceProposal(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("content pack source proposal is not approvable or not found")
	}
	if err != nil {
		return nil, fmt.Errorf("approve content pack source proposal: %w", err)
	}
	return record, nil
}

func (s *Store) RejectContentPackSourceProposal(ctx context.Context, p RejectContentPackSourceProposalParams) (*ContentPackSourceProposalRecord, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if p.TenantID == uuid.Nil {
		return nil, errors.New("tenant id is required")
	}
	if p.ProposalID == uuid.Nil {
		return nil, errors.New("source proposal id is required")
	}
	rejectedBy := strings.TrimSpace(p.RejectedBySubject)
	if rejectedBy == "" {
		return nil, errors.New("rejected by subject is required")
	}
	status := ContentPackSourceProposalStatusRejected
	if p.PrivacyBlocked {
		status = ContentPackSourceProposalStatusPrivacyBlocked
	}
	row := s.db.QueryRowContext(ctx, `
		UPDATE content_pack_source_proposals
		SET status = $3,
		    rejected_by_subject = $4,
		    rejected_at = NOW(),
		    rejection_reason = $5,
		    approved_by_subject = '',
		    approved_at = NULL,
		    approval_note = '',
		    collect_mode = '',
		    updated_at = NOW()
		WHERE tenant_id = $1
		  AND id = $2
		  AND status IN ('proposed', 'auto_eligible', 'approval_required', 'stale')
		RETURNING `+contentPackSourceProposalSelectColumns,
		p.TenantID,
		p.ProposalID,
		status,
		rejectedBy,
		strings.TrimSpace(p.RejectionReason),
	)
	record, err := scanContentPackSourceProposal(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("content pack source proposal is not rejectable or not found")
	}
	if err != nil {
		return nil, fmt.Errorf("reject content pack source proposal: %w", err)
	}
	return record, nil
}

const contentPackSourceProposalSelectColumns = `
	id, tenant_id, node_id, proposal_id, kind, program, source_id, collector_type,
	formatter, status, confidence, risk, auto_connect_eligible, requires_approval,
	reason, paths, evidence, labels, first_seen_at, last_seen_at,
	approved_by_subject, approved_at, approval_note, rejected_by_subject,
	collect_mode, rejected_at, rejection_reason, created_at, updated_at
`

func scanContentPackSourceProposal(row scanner) (*ContentPackSourceProposalRecord, error) {
	var record ContentPackSourceProposalRecord
	var paths, evidence []string
	var labelsRaw []byte
	var approvedAt, rejectedAt sql.NullTime
	if err := row.Scan(
		&record.ID,
		&record.TenantID,
		&record.NodeID,
		&record.ProposalID,
		&record.Kind,
		&record.Program,
		&record.SourceID,
		&record.CollectorType,
		&record.Formatter,
		&record.Status,
		&record.Confidence,
		&record.Risk,
		&record.AutoConnectEligible,
		&record.RequiresApproval,
		&record.Reason,
		pq.Array(&paths),
		pq.Array(&evidence),
		&labelsRaw,
		&record.FirstSeenAt,
		&record.LastSeenAt,
		&record.ApprovedBySubject,
		&approvedAt,
		&record.ApprovalNote,
		&record.RejectedBySubject,
		&record.CollectMode,
		&rejectedAt,
		&record.RejectionReason,
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		return nil, err
	}
	labels, err := decodeContentPackStringMap(labelsRaw)
	if err != nil {
		return nil, err
	}
	record.Paths = normalizeContentPackSourceProposalStrings(paths, 64)
	record.Evidence = normalizeContentPackSourceProposalStrings(evidence, 64)
	record.Labels = labels
	record.CollectMode = strings.TrimSpace(record.CollectMode)
	if approvedAt.Valid {
		t := approvedAt.Time
		record.ApprovedAt = &t
	}
	if rejectedAt.Valid {
		t := rejectedAt.Time
		record.RejectedAt = &t
	}
	return &record, nil
}

func normalizeContentPackSourceProposal(tenantID, nodeID uuid.UUID, proposal connectordiscovery.Proposal) (ContentPackSourceProposalRecord, bool, error) {
	if tenantID == uuid.Nil {
		return ContentPackSourceProposalRecord{}, false, errors.New("tenant id is required")
	}
	if nodeID == uuid.Nil {
		return ContentPackSourceProposalRecord{}, false, errors.New("node id is required")
	}
	program := strings.ToLower(strings.TrimSpace(proposal.Program))
	if program == "" {
		return ContentPackSourceProposalRecord{}, false, nil
	}
	kind := strings.ToLower(strings.TrimSpace(proposal.Kind))
	if kind == "" {
		kind = connectordiscovery.KindLocalLog
	}
	collectorType := strings.ToLower(strings.TrimSpace(proposal.CollectorType))
	if collectorType == "" && kind == connectordiscovery.KindLocalLog {
		collectorType = connectordiscovery.CollectorTypeFile
	}
	risk := strings.ToLower(strings.TrimSpace(proposal.Risk))
	requiresApproval := proposal.RequiresApproval || risk == "high" || risk == "critical"
	autoEligible := proposal.AutoConnectEligible && !requiresApproval
	status := ContentPackSourceProposalStatusProposed
	switch {
	case requiresApproval:
		status = ContentPackSourceProposalStatusApprovalRequired
	case autoEligible:
		status = ContentPackSourceProposalStatusAutoEligible
	}
	confidence := proposal.Confidence
	if confidence < 0 {
		confidence = 0
	}
	if confidence > 100 {
		confidence = 100
	}
	labels := normalizeContentPackSourceProposalLabels(proposal.Labels)
	sourceID := firstContentPackSourceProposalValue(
		labels["content_pack_source_id"],
		labels["source_id"],
		labels["parser_profile"],
		program,
	)
	proposalID := strings.ToLower(strings.TrimSpace(proposal.ID))
	if proposalID == "" {
		proposalID = kind + ":" + program
	}
	return ContentPackSourceProposalRecord{
		TenantID:            tenantID,
		NodeID:              nodeID,
		ProposalID:          proposalID,
		Kind:                kind,
		Program:             program,
		SourceID:            sourceID,
		CollectorType:       collectorType,
		Formatter:           strings.ToLower(strings.TrimSpace(proposal.Formatter)),
		Status:              status,
		Confidence:          confidence,
		Risk:                risk,
		AutoConnectEligible: autoEligible,
		RequiresApproval:    requiresApproval,
		Reason:              strings.TrimSpace(proposal.Reason),
		Paths:               normalizeContentPackSourceProposalStrings(proposal.Paths, 64),
		Evidence:            normalizeContentPackSourceProposalStrings(proposal.Evidence, 64),
		Labels:              labels,
	}, true, nil
}

func normalizeContentPackSourceProposalStrings(values []string, limit int) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func normalizeContentPackSourceProposalCollectMode(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ContentPackSourceProposalCollectModeCollectRaw, nil
	}
	switch value {
	case ContentPackSourceProposalCollectModeObserveOnly,
		ContentPackSourceProposalCollectModeMetadataOnly,
		ContentPackSourceProposalCollectModeCollectParsed,
		ContentPackSourceProposalCollectModeCollectRaw,
		ContentPackSourceProposalCollectModeDisabled:
		return value, nil
	default:
		return "", fmt.Errorf("unsupported source proposal collect_mode %q", value)
	}
}

func ContentPackSourceProposalCollectModeDeploysRaw(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return value == "" || value == ContentPackSourceProposalCollectModeCollectRaw
}

func ContentPackSourceProposalCollectModeDeploysOTel(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return value == "" ||
		value == ContentPackSourceProposalCollectModeCollectRaw ||
		value == ContentPackSourceProposalCollectModeCollectParsed
}

func ContentPackSourceProposalCollectModeDeploysNodeAgent(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return value == "" ||
		value == ContentPackSourceProposalCollectModeCollectRaw ||
		value == ContentPackSourceProposalCollectModeCollectParsed
}

func normalizeContentPackSourceProposalLabels(labels map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range labels {
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		out[key] = value
	}
	keys := make([]string, 0, len(out))
	for key := range out {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) <= 64 {
		return out
	}
	trimmed := make(map[string]string, 64)
	for _, key := range keys[:64] {
		trimmed[key] = out[key]
	}
	return trimmed
}

func firstContentPackSourceProposalValue(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
