# Offline IP intelligence

Control One enriches public IP addresses locally using MMDB files. The default
community datasets are DB-IP City Lite and ASN Lite. DB-IP Lite is updated
monthly and licensed under CC BY 4.0; the Investigate IP panel renders the
required DB-IP attribution when this source is used.

## Runtime

Set:

```bash
IP_INTEL_CITY_MMDB=/var/lib/control-one/ip-intel/dbip-city-lite.mmdb
IP_INTEL_ASN_MMDB=/var/lib/control-one/ip-intel/dbip-asn-lite.mmdb
```

Geo/ASN lookup is native Go using memory-mapped MMDB files. Only observed IP
results are cached in `ip_enrichment_cache`; source network ranges are not
expanded into Postgres.

AbuseIPDB is optional and is used only to augment reputation. Event ingestion
uses `LookupGeoLocal`, which is guaranteed not to invoke network providers.

## Updating DB-IP Lite

```bash
DBIP_LICENSE_ACCEPTED=1 scripts/update_dbip_lite.sh ./ip-intel
```

The updater:
- tries the current UTC month, then the previous month (DB-IP datasets may
  publish on different days);
- validates the gzip payload and a minimum decompressed size;
- writes to a temporary file and atomically renames it;
- leaves a previously installed database untouched if refresh fails;
- writes a small version sidecar and attribution notice.

The production deploy runs this updater before recreating the control-plane
container. Files are mounted read-only into the container.

## Air-gapped deployments

Use the same two MMDB files. Either place them directly in the persistent
`ip-intel` directory or include them as signed offline-content artifacts, for
example:

```json
{
  "type": "ip_intel_mmdb",
  "name": "city",
  "version": "2026-10",
  "path": "content/dbip-city-lite.mmdb"
}
```

and an equivalent `asn` artifact. Point `IP_INTEL_CITY_MMDB` and
`IP_INTEL_ASN_MMDB` to the activated files and restart the control plane.

## Commercial datasets

The reader consumes standard MMDB schema, so a commercial DB-IP/GeoIP-compatible
dataset can replace the Lite files without changing API or UI contracts.
