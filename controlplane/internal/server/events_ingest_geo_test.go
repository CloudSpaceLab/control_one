package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/ipintel"
)

func TestEnrichConnectionGeoWithLookupAddsSourceAndDestinationMetadata(t *testing.T) {
	events := []IngestedEvent{{
		Type:  "conn.open",
		SrcIP: "8.8.8.8",
		DstIP: "1.1.1.1",
		Details: map[string]any{
			"direction": "inbound",
		},
	}}
	calls := map[string]int{}
	lookup := func(_ context.Context, ip string) (*ipintel.Enrichment, error) {
		calls[ip]++
		switch ip {
		case "8.8.8.8":
			return &ipintel.Enrichment{
				Source: "dbip-lite", GeoDatasetVersion: "2026-10", ASNDatasetVersion: "2026-10",
				Geo: ipintel.GeoInfo{Country: "United States", CountryCode: "US", City: "Mountain View", ASN: "AS15169", Org: "Google LLC"},
			}, nil
		case "1.1.1.1":
			return &ipintel.Enrichment{
				Source: "dbip-lite",
				Geo: ipintel.GeoInfo{Country: "Australia", CountryCode: "AU", ASN: "AS13335", Org: "Cloudflare, Inc."},
			}, nil
		default:
			return nil, nil
		}
	}

	enrichConnectionGeoWithLookup(context.Background(), events, lookup)

	details := events[0].Details
	require.Equal(t, "US", details["src_country_code"])
	require.Equal(t, "United States", details["src_country"])
	require.Equal(t, "AS15169", details["src_asn"])
	require.Equal(t, "Google LLC", details["src_as_org"])
	require.Equal(t, "AU", details["dst_country_code"])
	require.Equal(t, "AS13335", details["dst_asn"])
	require.Equal(t, "US", details["country_code"])
	require.Equal(t, "AS15169", details["asn"])
	require.Equal(t, "2026-10", details["src_geo_dataset_version"])
	require.Equal(t, 1, calls["8.8.8.8"])
	require.Equal(t, 1, calls["1.1.1.1"])
}

func TestEnrichConnectionGeoDoesNotOverwriteParserMetadataOrLookupPrivateIPs(t *testing.T) {
	events := []IngestedEvent{
		{
			Type:  "conn.open",
			SrcIP: "8.8.8.8",
			DstIP: "10.0.0.4",
			Details: map[string]any{
				"country_code":     "GB",
				"src_country_code": "GB",
				"asn":              "AS64500",
			},
		},
		{Type: "proc.exec", SrcIP: "8.8.8.8"},
	}
	calls := 0
	lookup := func(_ context.Context, _ string) (*ipintel.Enrichment, error) {
		calls++
		return &ipintel.Enrichment{
			Source: "dbip-lite",
			Geo: ipintel.GeoInfo{Country: "United States", CountryCode: "US", ASN: "AS15169"},
		}, nil
	}

	enrichConnectionGeoWithLookup(context.Background(), events, lookup)

	require.Equal(t, 1, calls)
	require.Equal(t, "GB", events[0].Details["country_code"])
	require.Equal(t, "GB", events[0].Details["src_country_code"])
	require.Equal(t, "AS64500", events[0].Details["asn"])
	require.Nil(t, events[0].Details["dst_country_code"])
}
