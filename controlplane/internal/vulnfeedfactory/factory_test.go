package vulnfeedfactory

import (
	"testing"
	"time"
)

func TestBuildMergesOSVWithCISAKEV(t *testing.T) {
	feed, err := Build([]Input{
		{
			Format: "osv",
			Name:   "osv.json",
			Data: []byte(`{
				"id":"GHSA-abcd-1234",
				"aliases":["CVE-2026-1000"],
				"summary":"express vulnerable",
				"affected":[{
					"package":{"ecosystem":"npm","name":"express"},
					"ranges":[{"type":"SEMVER","events":[{"introduced":"4.0.0"},{"fixed":"4.18.3"}]}]
				}],
				"references":[{"type":"ADVISORY","url":"https://osv.dev/vulnerability/GHSA-abcd-1234"}]
			}`),
		},
		{
			Format: "cisa-kev",
			Name:   "kev.json",
			Data: []byte(`{
				"vulnerabilities":[{
					"cveID":"CVE-2026-1000",
					"vendorProject":"Example",
					"product":"Express",
					"vulnerabilityName":"Example Express Vulnerability",
					"dateAdded":"2026-05-29",
					"requiredAction":"Apply updates",
					"dueDate":"2026-06-20",
					"knownRansomwareCampaignUse":"Known"
				}]
			}`),
		},
	}, Options{Source: "unit-test", GeneratedAt: time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if feed.Source != "unit-test" || feed.GeneratedAt != "2026-05-29T12:00:00Z" {
		t.Fatalf("feed metadata = %#v", feed)
	}
	if len(feed.Advisories) != 1 {
		t.Fatalf("advisories = %#v", feed.Advisories)
	}
	adv := feed.Advisories[0]
	if adv.CVEID != "CVE-2026-1000" || !adv.KEV {
		t.Fatalf("advisory identity/kev = %#v", adv)
	}
	if adv.Metadata["cisa_kev"] != true || adv.Metadata["upstream_format"] != "osv" {
		t.Fatalf("merged metadata = %#v", adv.Metadata)
	}
	if len(adv.AffectedPackages) != 1 {
		t.Fatalf("affected packages = %#v", adv.AffectedPackages)
	}
	pkg := adv.AffectedPackages[0]
	if pkg.Name != "express" || pkg.Source != "npm" || pkg.VersionScheme != "semver" || pkg.FixedVersion != "4.18.3" {
		t.Fatalf("affected package = %#v", pkg)
	}
	if len(pkg.VersionRanges) != 1 || pkg.VersionRanges[0].Introduced != "4.0.0" || pkg.VersionRanges[0].Fixed != "4.18.3" {
		t.Fatalf("version ranges = %#v", pkg.VersionRanges)
	}
}

func TestBuildGitHubAdvisory(t *testing.T) {
	feed, err := Build([]Input{{
		Format: "github",
		Name:   "ghsa.json",
		Data: []byte(`{
			"ghsa_id":"GHSA-zzzz-yyyy",
			"cve_id":"CVE-2026-2000",
			"summary":"package vulnerable",
			"severity":"critical",
			"cvss":{"score":9.8},
			"html_url":"https://github.com/advisories/GHSA-zzzz-yyyy",
			"vulnerabilities":[{
				"package":{"ecosystem":"pip","name":"django"},
				"vulnerable_version_range":"< 5.0.1",
				"first_patched_version":{"identifier":"5.0.1"}
			}]
		}`),
	}}, Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	adv := feed.Advisories[0]
	if adv.CVEID != "CVE-2026-2000" || adv.Severity != "critical" || adv.CVSSScore == nil || *adv.CVSSScore != 9.8 {
		t.Fatalf("advisory = %#v", adv)
	}
	if got := adv.AffectedPackages[0]; got.Name != "django" || got.Source != "pypi" || got.VersionRange != "< 5.0.1" || got.FixedVersion != "5.0.1" {
		t.Fatalf("affected package = %#v", got)
	}
}

func TestBuildNVDCPEVersionRange(t *testing.T) {
	feed, err := Build([]Input{{
		Format: "nvd",
		Name:   "nvd.json",
		Data: []byte(`{
			"vulnerabilities":[{
				"cve":{
					"id":"CVE-2026-3000",
					"published":"2026-05-01T00:00:00.000",
					"metrics":{"cvssMetricV31":[{"cvssData":{"baseScore":7.5,"baseSeverity":"HIGH"}}]},
					"references":{"referenceData":[{"url":"https://vendor.example/CVE-2026-3000"}]},
					"configurations":[{
						"nodes":[{
							"cpeMatch":[{
								"vulnerable":true,
								"criteria":"cpe:2.3:a:example:product:*:*:*:*:*:*:*:*",
								"versionStartIncluding":"1.0.0",
								"versionEndExcluding":"1.2.0"
							}]
						}]
					}]
				}
			}]
		}`),
	}}, Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	adv := feed.Advisories[0]
	if adv.CVSSScore == nil || *adv.CVSSScore != 7.5 || adv.Severity != "high" {
		t.Fatalf("nvd score/severity = %#v", adv)
	}
	pkg := adv.AffectedPackages[0]
	if pkg.Source != "cpe" || pkg.Name != "cpe:2.3:a:example:product:*:*:*:*:*:*:*:*" {
		t.Fatalf("nvd cpe package = %#v", pkg)
	}
	if len(pkg.VersionRanges) != 1 || pkg.VersionRanges[0].Range != ">= 1.0.0, < 1.2.0" || pkg.FixedVersion != "1.2.0" {
		t.Fatalf("nvd range = %#v", pkg)
	}
}
