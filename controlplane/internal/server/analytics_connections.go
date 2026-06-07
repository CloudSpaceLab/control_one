package server

import (
	"context"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/doris"
)

const (
	analyticsSourceDoris        = "doris"
	analyticsSourceSmall        = "small-analytics"
	analyticsSourceSmallPending = "small-analytics-pending"
)

func (s *Server) listAnalyticsConnectionsForIP(ctx context.Context, tenantID, ip string, since, until time.Time, limit int) ([]doris.ConnectionRow, string, error) {
	if s == nil {
		return nil, analyticsSourceSmallPending, nil
	}
	if s.usesDorisAnalytics() {
		rows, err := s.dorisClient.ListConnectionsForIP(ctx, tenantID, ip, since, until, limit)
		return rows, analyticsSourceDoris, err
	}
	if s.localAnalytics != nil {
		rows, err := s.localAnalytics.ListConnectionsForIP(ctx, tenantID, ip, since, until, limit)
		return rows, analyticsSourceSmall, err
	}
	return nil, analyticsSourceSmallPending, nil
}

func (s *Server) listAnalyticsConnectionsForNode(ctx context.Context, tenantID, nodeID string, since, until time.Time, limit int, openOnly, externalOnly bool) ([]doris.ConnectionRow, string, error) {
	if s == nil {
		return nil, analyticsSourceSmallPending, nil
	}
	if s.usesDorisAnalytics() {
		rows, err := s.dorisClient.ListConnectionsForNode(ctx, tenantID, nodeID, since, until, limit, openOnly, externalOnly)
		return rows, analyticsSourceDoris, err
	}
	if s.localAnalytics != nil {
		rows, err := s.localAnalytics.ListConnectionsForNode(ctx, tenantID, nodeID, since, until, limit, openOnly, externalOnly)
		return rows, analyticsSourceSmall, err
	}
	return nil, analyticsSourceSmallPending, nil
}

func (s *Server) listAnalyticsConnectionsForTenant(ctx context.Context, tenantID string, since, until time.Time, limit int, externalOnly bool) ([]doris.ConnectionRow, string, error) {
	if s == nil {
		return nil, analyticsSourceSmallPending, nil
	}
	if s.usesDorisAnalytics() {
		rows, err := s.dorisClient.ListConnectionsForTenant(ctx, tenantID, since, until, limit, externalOnly)
		return rows, analyticsSourceDoris, err
	}
	if s.localAnalytics != nil {
		rows, err := s.localAnalytics.ListConnectionsForTenant(ctx, tenantID, since, until, limit, externalOnly)
		return rows, analyticsSourceSmall, err
	}
	return nil, analyticsSourceSmallPending, nil
}
