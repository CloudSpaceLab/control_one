package server

import (
	"strings"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
)

const (
	analyticsModeAuto     = "auto"
	analyticsModeSmall    = "small"
	analyticsModeOLAP     = "olap"
	analyticsModeDisabled = "disabled"
)

func effectiveAnalyticsMode(cfg *config.Config) string {
	if cfg == nil {
		return analyticsModeSmall
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Analytics.Mode)) {
	case analyticsModeSmall:
		return analyticsModeSmall
	case analyticsModeOLAP:
		return analyticsModeOLAP
	case analyticsModeDisabled:
		return analyticsModeDisabled
	case "", analyticsModeAuto:
		if cfg.Doris.Enabled && strings.TrimSpace(cfg.Doris.DSN) != "" {
			return analyticsModeOLAP
		}
		return analyticsModeSmall
	default:
		return analyticsModeSmall
	}
}

func (s *Server) usesDorisAnalytics() bool {
	return s != nil && effectiveAnalyticsMode(s.cfg) == analyticsModeOLAP && s.dorisClient != nil
}
