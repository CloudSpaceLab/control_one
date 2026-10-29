package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/packageinventory"
	"github.com/google/uuid"
)

func (s *Store) RecordNodeUpdateCandidates(ctx context.Context, nodeID uuid.UUID, snapshotSHA string, observations []packageinventory.CandidateObservation) error {
	if len(observations) > 2000 {
		return errors.New("candidate observation limit exceeded")
	}
	observed := time.Time{}
	seen := map[string]bool{}
	for _, o := range observations {
		if o.Name == "" || o.InstalledVersion == "" || o.ObservedAt.IsZero() || o.ObservedAt.After(time.Now().UTC()) || time.Since(o.ObservedAt) > time.Hour {
			return errors.New("invalid or stale candidate observation")
		}
		if o.Repository != "" {
			u, err := url.Parse(o.Repository)
			if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
				return errors.New("unsafe repository identity")
			}
		}
		if o.State == "verified_repository_candidate" && !o.VerifiedAt(time.Now().UTC()) {
			return errors.New("candidate provenance incomplete")
		}
		key := o.Name + "\x00" + o.Arch
		if seen[key] {
			return errors.New("duplicate candidate identity")
		}
		seen[key] = true
		if o.ObservedAt.After(observed) {
			observed = o.ObservedAt
		}
	}
	if observed.IsZero() {
		observed = time.Now().UTC()
	}
	raw, err := json.Marshal(observations)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var currentSHA string
	if err = tx.QueryRowContext(ctx, `SELECT i.snapshot->>'sha256' FROM nodes n JOIN node_inventory_sync i ON i.node_id=n.id WHERE n.id=$1 FOR UPDATE OF n`, nodeID).Scan(&currentSHA); err != nil {
		return err
	}
	if currentSHA != snapshotSHA {
		return ErrAssessmentSuperseded
	}
	for _, o := range observations {
		var matches bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM node_packages WHERE node_id=$1 AND name=$2 AND version=$3 AND COALESCE(arch,'')=$4 AND source='dpkg')`, nodeID, o.Name, o.InstalledVersion, o.Arch).Scan(&matches); err != nil {
			return err
		}
		if !matches {
			return ErrAssessmentSuperseded
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO node_update_candidate_observations(id,node_id,snapshot_sha256,observed_at,observations) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, uuid.New(), nodeID, snapshotSHA, observed, raw)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) GetNodeUpdateCandidates(ctx context.Context, nodeID uuid.UUID) ([]packageinventory.CandidateObservation, error) {
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT o.observations FROM node_update_candidate_observations o JOIN node_inventory_sync i ON i.node_id=o.node_id AND i.snapshot->>'sha256'=o.snapshot_sha256 WHERE o.node_id=$1 AND o.observed_at BETWEEN NOW()-INTERVAL '1 hour' AND NOW() ORDER BY o.observed_at DESC LIMIT 1`, nodeID).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var observations []packageinventory.CandidateObservation
	if err = json.Unmarshal(raw, &observations); err != nil {
		return nil, err
	}
	return observations, nil
}
