package ipintel

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/oschwald/maxminddb-golang/v2"
)

const (
	dbIPSource         = "dbip-lite"
	dbIPAttribution    = "IP Geolocation by DB-IP"
	dbIPAttributionURL = "https://db-ip.com"
)

type dbIPMMDBProvider struct {
	city      *maxminddb.Reader
	asn       *maxminddb.Reader
	cityBuild string
	asnBuild  string
}

type dbIPCityRecord struct {
	Country struct {
		ISOCode string            `maxminddb:"iso_code"`
		Names   map[string]string `maxminddb:"names"`
	} `maxminddb:"country"`
	City struct {
		Names map[string]string `maxminddb:"names"`
	} `maxminddb:"city"`
	Subdivisions []struct {
		Names map[string]string `maxminddb:"names"`
	} `maxminddb:"subdivisions"`
	Location struct {
		Latitude  float64 `maxminddb:"latitude"`
		Longitude float64 `maxminddb:"longitude"`
		Timezone  string  `maxminddb:"time_zone"`
	} `maxminddb:"location"`
}

type dbIPASNRecord struct {
	ASN uint32 `maxminddb:"autonomous_system_number"`
	Org string `maxminddb:"autonomous_system_organization"`
}

// NewDBIPMMDBProvider opens one or both DB-IP Lite MMDB files. Partial
// availability is allowed so a missing ASN file does not disable city/country
// enrichment (and vice versa).
func NewDBIPMMDBProvider(cityPath, asnPath string) (Provider, error) {
	cityPath = strings.TrimSpace(cityPath)
	asnPath = strings.TrimSpace(asnPath)
	if cityPath == "" && asnPath == "" {
		return nil, errors.New("dbip: no MMDB paths configured")
	}
	provider := &dbIPMMDBProvider{}
	var errs []error
	if cityPath != "" {
		reader, err := maxminddb.Open(cityPath)
		if err != nil {
			errs = append(errs, fmt.Errorf("open DB-IP city MMDB: %w", err))
		} else {
			provider.city = reader
			provider.cityBuild = mmdbBuildVersion(reader)
		}
	}
	if asnPath != "" {
		reader, err := maxminddb.Open(asnPath)
		if err != nil {
			errs = append(errs, fmt.Errorf("open DB-IP ASN MMDB: %w", err))
		} else {
			provider.asn = reader
			provider.asnBuild = mmdbBuildVersion(reader)
		}
	}
	if provider.city == nil && provider.asn == nil {
		return nil, errors.Join(errs...)
	}
	return provider, errors.Join(errs...)
}

func (p *dbIPMMDBProvider) Name() string  { return dbIPSource }
func (p *dbIPMMDBProvider) Offline() bool { return true }

func (p *dbIPMMDBProvider) Lookup(_ context.Context, ip string) (*Enrichment, error) {
	addr, err := netip.ParseAddr(strings.TrimSpace(ip))
	if err != nil {
		return nil, errors.New("dbip: invalid ip")
	}
	addr = addr.Unmap()
	out := &Enrichment{
		Addr:              addr.String(),
		Source:            dbIPSource,
		GeoDatasetVersion: p.cityBuild,
		ASNDatasetVersion: p.asnBuild,
		Attribution:       dbIPAttribution,
		AttributionURL:    dbIPAttributionURL,
		FetchedAt:         time.Now().UTC(),
	}
	if p.city != nil {
		result := p.city.Lookup(addr)
		if err := result.Err(); err != nil {
			return nil, fmt.Errorf("dbip city lookup: %w", err)
		}
		if result.Found() {
			var record dbIPCityRecord
			if err := result.Decode(&record); err != nil {
				return nil, fmt.Errorf("decode DB-IP city record: %w", err)
			}
			applyDBIPCityRecord(out, record)
		}
	}
	if p.asn != nil {
		result := p.asn.Lookup(addr)
		if err := result.Err(); err != nil {
			return nil, fmt.Errorf("dbip ASN lookup: %w", err)
		}
		if result.Found() {
			var record dbIPASNRecord
			if err := result.Decode(&record); err != nil {
				return nil, fmt.Errorf("decode DB-IP ASN record: %w", err)
			}
			applyDBIPASNRecord(out, record)
		}
	}
	return out, nil
}

func (p *dbIPMMDBProvider) Close() error {
	if p == nil {
		return nil
	}
	var errs []error
	if p.city != nil {
		errs = append(errs, p.city.Close())
		p.city = nil
	}
	if p.asn != nil {
		errs = append(errs, p.asn.Close())
		p.asn = nil
	}
	return errors.Join(errs...)
}

func applyDBIPCityRecord(out *Enrichment, record dbIPCityRecord) {
	if out == nil {
		return
	}
	out.Geo.CountryCode = strings.ToUpper(strings.TrimSpace(record.Country.ISOCode))
	out.Geo.Country = localizedName(record.Country.Names)
	out.Geo.City = localizedName(record.City.Names)
	if len(record.Subdivisions) > 0 {
		out.Geo.Region = localizedName(record.Subdivisions[0].Names)
	}
	out.Geo.Latitude = record.Location.Latitude
	out.Geo.Longitude = record.Location.Longitude
	out.Geo.Timezone = strings.TrimSpace(record.Location.Timezone)
}

func applyDBIPASNRecord(out *Enrichment, record dbIPASNRecord) {
	if out == nil {
		return
	}
	if record.ASN > 0 {
		out.Geo.ASN = fmt.Sprintf("AS%d", record.ASN)
	}
	out.Geo.Org = strings.TrimSpace(record.Org)
}

func localizedName(names map[string]string) string {
	if len(names) == 0 {
		return ""
	}
	for _, key := range []string{"en", "en-US", "en-GB"} {
		if value := strings.TrimSpace(names[key]); value != "" {
			return value
		}
	}
	for _, value := range names {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func mmdbBuildVersion(reader *maxminddb.Reader) string {
	if reader == nil {
		return ""
	}
	build := reader.Metadata.BuildTime().UTC()
	if build.IsZero() {
		return ""
	}
	return build.Format("2006-01")
}
