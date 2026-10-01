package ipintel

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
)

func TestDBIPRecordMapping(t *testing.T) {
	var city dbIPCityRecord
	city.Country.ISOCode = "ng"
	city.Country.Names = map[string]string{"en": "Nigeria"}
	city.City.Names = map[string]string{"en": "Lagos"}
	city.Subdivisions = append(city.Subdivisions, struct {
		Names map[string]string `maxminddb:"names"`
	}{Names: map[string]string{"en": "Lagos"}})
	city.Location.Latitude = 6.45
	city.Location.Longitude = 3.39

	out := &Enrichment{}
	applyDBIPCityRecord(out, city)
	applyDBIPASNRecord(out, dbIPASNRecord{ASN: 29465, Org: "MTN Nigeria"})

	require.Equal(t, "NG", out.Geo.CountryCode)
	require.Equal(t, "Nigeria", out.Geo.Country)
	require.Equal(t, "Lagos", out.Geo.Region)
	require.Equal(t, "Lagos", out.Geo.City)
	require.Equal(t, "AS29465", out.Geo.ASN)
	require.Equal(t, "MTN Nigeria", out.Geo.Org)
	require.InDelta(t, 6.45, out.Geo.Latitude, 0.0001)
}

func TestLocalizedNamePrefersEnglish(t *testing.T) {
	require.Equal(t, "Nigeria", localizedName(map[string]string{
		"fr": "Nigéria",
		"en": "Nigeria",
	}))
	require.Empty(t, localizedName(nil))
}

func TestServiceLocalMMDBFailureKeepsReputationFallback(t *testing.T) {
	cfg := config.IPIntelConfig{
		Enabled:      true,
		CityMMDBPath: "/does/not/exist/city.mmdb",
		ASNMMDBPath:  "/does/not/exist/asn.mmdb",
		AbuseIPDBKey: "test-key",
		HTTPTimeout:  time.Second,
	}
	svc := New(cfg, NewMemCache())
	require.True(t, svc.Enabled())
	require.False(t, svc.OfflineGeoEnabled())
	require.Error(t, svc.InitError())
	require.Equal(t, "abuseipdb", svc.primary.Name())
}

func TestLookupGeoLocalNeverUsesNetworkProvider(t *testing.T) {
	doer := &stubDoer{resp: makeJSONResp(200, map[string]any{
		"ip":       "8.8.8.8",
		"location": map[string]any{"country_code": "US"},
	})}
	svc := &Service{
		cfg:     config.IPIntelConfig{Enabled: true},
		primary: NewIpqueryProvider("https://ipq.example", doer),
	}
	_, err := svc.LookupGeoLocal(context.Background(), "8.8.8.8")
	require.ErrorIs(t, err, ErrOfflineGeoUnavailable)
	require.Nil(t, doer.last)
}

func TestValidatePrefersLocalMMDB(t *testing.T) {
	enabled, primary := Validate(config.IPIntelConfig{
		Enabled:        true,
		CityMMDBPath:   "/data/city.mmdb",
		IpqueryBaseURL: "https://legacy.example",
		AbuseIPDBKey:   "key",
	})
	require.True(t, enabled)
	require.Equal(t, "dbip-lite", primary)
}
