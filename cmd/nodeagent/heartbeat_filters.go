package main

import (
	"encoding/json"
	"net"
	"sync/atomic"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/internal/dbquery"
	"github.com/CloudSpaceLab/control_one/internal/fileaccess"
	"github.com/CloudSpaceLab/control_one/internal/netflow"
)

func parseCIDRs(in []string) []*net.IPNet {
	if len(in) == 0 {
		return nil
	}
	out := make([]*net.IPNet, 0, len(in))
	for _, s := range in {
		_, n, err := net.ParseCIDR(s)
		if err != nil {
			continue
		}
		out = append(out, n)
	}
	return out
}

// tenantFilterPolicy mirrors storage.TenantEventFilters JSON shape so we
// can decode without importing the controlplane storage package on the
// agent side.
type tenantFilterPolicy struct {
	CaptureExternal         bool     `json:"capture_external"`
	CaptureInternalSummary  bool     `json:"capture_internal_summary"`
	CaptureListeningChanges bool     `json:"capture_listening_changes"`
	CaptureFiles            bool     `json:"capture_files"`
	CaptureDBQueries        bool     `json:"capture_db_queries"`
	ThreatMatchFull         bool     `json:"threat_match_full"`
	FilePathsWatch          []string `json:"file_paths_watch"`
	FileSizeMinBytes        int64    `json:"file_size_min_bytes"`
	AllowlistCIDRs          []string `json:"allowlist_cidrs"`
	DenylistCIDRs           []string `json:"denylist_cidrs"`
	TrustedProxyCIDRs       []string `json:"trusted_proxy_cidrs"`
	DBQueryTextCapture      bool     `json:"db_query_text_capture"`
	ForensicMode            bool     `json:"forensic_mode"`
}

// makeFilterApplier returns a FilterApplier closure that fans tenant
// policy into the live collector managers. Each call is cheap (atomic
// pointer swap inside SetFilter; mutex-guarded slice copy in
// UpdateFilter; atomic.Bool in SetCaptureQueryText) so calling it on
// every heartbeat is fine.
//
// lastSig records the previous policy fingerprint so we only log on
// genuine flips — ops staring at journalctl don't want a log line every
// 60s saying "nothing changed".
func makeFilterApplier(
	log *zap.Logger,
	collectors *collectorRuntime,
	netMgr *netflow.Manager,
	fileMgr *fileaccess.Manager,
	dbMgr *dbquery.Manager,
) FilterApplier {
	var lastSig atomic.Uint64
	return func(raw json.RawMessage) {
		var p tenantFilterPolicy
		if err := json.Unmarshal(raw, &p); err != nil {
			log.Warn("decode tenant event filters", zap.Error(err))
			return
		}
		sig := policySignature(&p)
		prev := lastSig.Swap(sig)
		changed := prev != sig

		// Netflow filter — atomic swap inside the manager.
		if netMgr != nil && (prev == 0 || changed) {
			netMgr.SetFilter(netflow.FilterConfig{
				CaptureExternal:         p.CaptureExternal,
				CaptureInternalSummary:  p.CaptureInternalSummary,
				CaptureListeningChanges: p.CaptureListeningChanges,
				AlwaysCaptureThreat:     p.ThreatMatchFull,
				AllowlistCIDRs:          parseCIDRs(p.AllowlistCIDRs),
				DenylistCIDRs:           parseCIDRs(p.DenylistCIDRs),
			})
		}
		// Fileaccess — watched prefix list + forensic-mode bypass.
		if fileMgr != nil {
			fileMgr.UpdateFilter(p.FilePathsWatch, p.ForensicMode)
		}
		if collectors != nil {
			collectors.SetEnabled("fileaccess", p.CaptureFiles || p.ForensicMode, "policy disabled capture_files")
		}
		// DB query — text capture toggle.
		if dbMgr != nil {
			dbMgr.SetCaptureQueryText(p.DBQueryTextCapture)
		}
		if collectors != nil {
			collectors.SetEnabled("dbquery", p.CaptureDBQueries || p.ForensicMode, "policy disabled capture_db_queries")
		}

		if changed {
			log.Info("applied tenant event filters",
				zap.Bool("capture_external", p.CaptureExternal),
				zap.Bool("capture_internal_summary", p.CaptureInternalSummary),
				zap.Bool("capture_files", p.CaptureFiles),
				zap.Bool("capture_db_queries", p.CaptureDBQueries),
				zap.Bool("forensic_mode", p.ForensicMode),
				zap.Int("file_paths_watch_count", len(p.FilePathsWatch)),
			)
		}
	}
}

// policySignature is a cheap 64-bit fingerprint of the policy so we can
// detect flips without deep-comparing every tick. Hash collision risk is
// irrelevant — false-negatives just suppress one log line.
func policySignature(p *tenantFilterPolicy) uint64 {
	var h uint64 = 1469598103934665603 // FNV-1a seed
	mix := func(b bool) {
		v := byte(0)
		if b {
			v = 1
		}
		h ^= uint64(v)
		h *= 1099511628211
	}
	mixStr := func(s string) {
		for i := 0; i < len(s); i++ {
			h ^= uint64(s[i])
			h *= 1099511628211
		}
	}
	mix(p.CaptureExternal)
	mix(p.CaptureInternalSummary)
	mix(p.CaptureListeningChanges)
	mix(p.CaptureFiles)
	mix(p.CaptureDBQueries)
	mix(p.ThreatMatchFull)
	mix(p.DBQueryTextCapture)
	mix(p.ForensicMode)
	h ^= uint64(p.FileSizeMinBytes)
	h *= 1099511628211
	for _, s := range p.FilePathsWatch {
		mixStr(s)
	}
	for _, s := range p.AllowlistCIDRs {
		mixStr(s)
	}
	for _, s := range p.DenylistCIDRs {
		mixStr(s)
	}
	for _, s := range p.TrustedProxyCIDRs {
		mixStr(s)
	}
	return h
}
