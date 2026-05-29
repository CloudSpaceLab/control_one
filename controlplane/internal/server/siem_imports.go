package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/siemimport"
)

const (
	maxSIEMImportCompressed   = 25 << 20
	maxSIEMImportDecompressed = 100 << 20
	maxSIEMImportRows         = 10_000
)

type siemImportResponse struct {
	ImportID      string             `json:"import_id"`
	DryRun        bool               `json:"dry_run"`
	RawSHA256     string             `json:"raw_sha256"`
	ContentBytes  int                `json:"content_bytes"`
	StoredRows    int                `json:"stored_rows"`
	Summary       siemimport.Summary `json:"summary"`
	AcceptedLabel map[string]string  `json:"accepted_label,omitempty"`
	Warnings      []string           `json:"warnings,omitempty"`
}

func (s *Server) handleSIEMImports(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleOperator, roleAdmin)
	if !ok {
		return
	}
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	tenantID, ok := s.requireTenantAccessFromQuery(w, r, principal, roleOperator, roleAdmin)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxSIEMImportCompressed)
	defer func() { _ = r.Body.Close() }()
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("read import payload: %v", err), http.StatusBadRequest)
		return
	}
	sum := sha256.Sum256(raw)
	rawSHA := hex.EncodeToString(sum[:])

	reader, closeFn, err := ingestedEventPayloadReader(raw, r.Header.Get("Content-Encoding"), maxSIEMImportDecompressed)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer closeFn()
	payload, err := io.ReadAll(reader)
	if err != nil {
		http.Error(w, fmt.Sprintf("read decoded import payload: %v", err), http.StatusBadRequest)
		return
	}
	if len(bytes.TrimSpace(payload)) == 0 {
		http.Error(w, "import payload is empty", http.StatusBadRequest)
		return
	}

	nodeID, err := parseOptionalUUIDQuery(r, "node_id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	maxRows := maxSIEMImportRows
	if text := strings.TrimSpace(r.URL.Query().Get("max_rows")); text != "" {
		parsed, err := strconv.Atoi(text)
		if err != nil || parsed <= 0 || parsed > maxSIEMImportRows {
			http.Error(w, fmt.Sprintf("max_rows must be between 1 and %d", maxSIEMImportRows), http.StatusBadRequest)
			return
		}
		maxRows = parsed
	}
	importID := uuid.NewString()
	format := firstNonEmptyString(
		r.URL.Query().Get("format"),
		r.Header.Get("X-ControlOne-Import-Format"),
		siemimport.FormatAuto,
	)
	source := firstNonEmptyString(
		r.URL.Query().Get("source"),
		r.Header.Get("X-ControlOne-Import-Source"),
		"existing-siem-archive",
	)
	importedAt := time.Now().UTC()
	rows, summary, err := siemimport.ParseLogs(payload, siemimport.Options{
		TenantID:   tenantID,
		NodeID:     nodeID,
		Format:     format,
		Source:     source,
		ImportID:   importID,
		ImportedAt: importedAt,
		MaxRows:    maxRows,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	dryRun := parseBoolQuery(r.URL.Query().Get("dry_run"))
	storedRows := 0
	if !dryRun && len(rows) > 0 {
		if err := s.store.CreateTelemetryLogs(r.Context(), rows); err != nil {
			s.logger.Error("SIEM import telemetry logs",
				zap.Error(err),
				zap.String("tenant_id", tenantID.String()),
				zap.String("import_id", importID),
				zap.Int("rows", len(rows)),
			)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		storedRows = len(rows)
	}

	status := "completed"
	if dryRun {
		status = "dry_run"
	}
	s.recordAudit(r.Context(), principal, tenantID, "siem_import."+status, "siem_import", importID, map[string]any{
		"format":        summary.Format,
		"source":        summary.Source,
		"raw_sha256":    rawSHA,
		"content_bytes": len(payload),
		"rows_parsed":   summary.RowsParsed,
		"rows_accepted": summary.RowsAccepted,
		"rows_skipped":  summary.RowsSkipped,
		"stored_rows":   storedRows,
		"dry_run":       dryRun,
	})

	resp := siemImportResponse{
		ImportID:     importID,
		DryRun:       dryRun,
		RawSHA256:    rawSHA,
		ContentBytes: len(payload),
		StoredRows:   storedRows,
		Summary:      summary,
		AcceptedLabel: map[string]string{
			"control_one.import_id":     importID,
			"control_one.import_format": summary.Format,
		},
	}
	if summary.RowsSkipped > 0 {
		resp.Warnings = append(resp.Warnings, "some records were skipped because no log message could be derived")
	}
	writeJSON(w, http.StatusAccepted, resp)
}

func parseOptionalUUIDQuery(r *http.Request, key string) (uuid.UUID, error) {
	value := strings.TrimSpace(r.URL.Query().Get(key))
	if value == "" {
		return uuid.Nil, nil
	}
	parsed, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%s must be a valid UUID", key)
	}
	return parsed, nil
}
