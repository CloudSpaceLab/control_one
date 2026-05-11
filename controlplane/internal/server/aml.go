package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/amlservice"
)

type amlScreener interface {
	Screen(context.Context, amlservice.ScreeningRequest) (*amlservice.ScreeningResult, error)
}

type amlScreenRequest struct {
	FullName            string `json:"full_name"`
	AccountNo           string `json:"account_no,omitempty"`
	Country             string `json:"country,omitempty"`
	DOB                 string `json:"dob,omitempty"`
	BirthDate           string `json:"birth_date,omitempty"`
	BVN                 string `json:"bvn,omitempty"`
	NIN                 string `json:"nin,omitempty"`
	IDNumber            string `json:"id_number,omitempty"`
	EntityType          string `json:"entity_type,omitempty"`
	IncludePEP          *bool  `json:"include_pep,omitempty"`
	IncludeSanctions    *bool  `json:"include_sanctions,omitempty"`
	IncludeAdverseMedia *bool  `json:"include_adverse_media,omitempty"`
	IncludeRegistry     *bool  `json:"include_registry,omitempty"`
}

func (r amlScreenRequest) validate() error {
	if strings.TrimSpace(r.FullName) == "" {
		return errors.New("full_name is required")
	}
	return nil
}

func (r amlScreenRequest) toAMLServiceRequest() amlservice.ScreeningRequest {
	birthDate := strings.TrimSpace(r.BirthDate)
	if birthDate == "" {
		birthDate = strings.TrimSpace(r.DOB)
	}

	idNumber := strings.TrimSpace(r.IDNumber)
	if idNumber == "" {
		idNumber = strings.TrimSpace(r.BVN)
	}
	if idNumber == "" {
		idNumber = strings.TrimSpace(r.NIN)
	}

	return amlservice.ScreeningRequest{
		Name:                strings.TrimSpace(r.FullName),
		AccountNo:           strings.TrimSpace(r.AccountNo),
		Country:             strings.TrimSpace(r.Country),
		BirthDate:           birthDate,
		IDNumber:            idNumber,
		EntityType:          strings.TrimSpace(r.EntityType),
		IncludePEP:          boolDefault(r.IncludePEP, true),
		IncludeSanctions:    boolDefault(r.IncludeSanctions, true),
		IncludeAdverseMedia: boolDefault(r.IncludeAdverseMedia, true),
		IncludeRegistry:     boolDefault(r.IncludeRegistry, strings.TrimSpace(r.Country) != ""),
	}
}

func boolDefault(v *bool, fallback bool) bool {
	if v == nil {
		return fallback
	}
	return *v
}

func (s *Server) handleAMLScreen(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	if _, ok := s.authorize(w, r, roleOperator, roleAdmin); !ok {
		return
	}
	if s.amlClient == nil {
		http.Error(w, "aml service not configured", http.StatusServiceUnavailable)
		return
	}

	defer func() { _ = r.Body.Close() }()
	var req amlScreenRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := req.validate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	result, err := s.amlClient.Screen(r.Context(), req.toAMLServiceRequest())
	if err != nil {
		http.Error(w, "aml service unavailable", http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
