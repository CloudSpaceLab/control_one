package server

import (
	"context"
	"net"
	"strings"
)

type nodeIPGeoResponse struct {
	IP                string   `json:"ip"`
	Country           string   `json:"country,omitempty"`
	CountryCode       string   `json:"country_code,omitempty"`
	City              string   `json:"city,omitempty"`
	Region            string   `json:"region,omitempty"`
	Latitude          *float64 `json:"latitude,omitempty"`
	Longitude         *float64 `json:"longitude,omitempty"`
	Source            string   `json:"source,omitempty"`
	GeoDatasetVersion string   `json:"geo_dataset_version,omitempty"`
}

// attachNodeGeo adds factual geolocation from the configured offline MMDB.
// It never calls a network provider and deliberately leaves location empty
// when no local evidence is available.
func (s *Server) attachNodeGeo(ctx context.Context, resp *nodeResponse) {
	if s == nil || resp == nil || s.ipIntel == nil || !s.ipIntel.OfflineGeoEnabled() {
		return
	}
	ip := nodeResponsePublicIP(*resp)
	if ip == "" {
		return
	}

	enrichment, err := s.ipIntel.LookupGeoLocal(ctx, ip)
	if err != nil || enrichment == nil {
		return
	}
	geo := enrichment.Geo
	if strings.TrimSpace(geo.Country) == "" &&
		strings.TrimSpace(geo.CountryCode) == "" &&
		strings.TrimSpace(geo.City) == "" &&
		strings.TrimSpace(geo.Region) == "" {
		return
	}

	lat := geo.Latitude
	lon := geo.Longitude
	resp.IPGeo = &nodeIPGeoResponse{
		IP:                ip,
		Country:           strings.TrimSpace(geo.Country),
		CountryCode:       strings.ToUpper(strings.TrimSpace(geo.CountryCode)),
		City:              strings.TrimSpace(geo.City),
		Region:            strings.TrimSpace(geo.Region),
		Latitude:          &lat,
		Longitude:         &lon,
		Source:            enrichment.Source,
		GeoDatasetVersion: enrichment.GeoDatasetVersion,
	}
}

func nodeResponsePublicIP(resp nodeResponse) string {
	bestValue := ""
	bestConfidence := -1
	for _, observation := range resp.NetworkObservations {
		if observation.Kind != "public_ip" {
			continue
		}
		value := strings.TrimSpace(observation.Value)
		if net.ParseIP(value) == nil {
			continue
		}
		if observation.Confidence > bestConfidence {
			bestValue = value
			bestConfidence = observation.Confidence
		}
	}
	if bestValue != "" {
		return bestValue
	}
	if resp.PublicIP == nil {
		return ""
	}
	value := strings.TrimSpace(*resp.PublicIP)
	if net.ParseIP(value) == nil {
		return ""
	}
	return value
}
