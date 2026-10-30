package patchsupport

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOfflineContentValidation(t *testing.T) {
	for _, scenario := range []string{"valid", "direct", "auto", "digest", "format", "selector", "scope", "duplicate scope", "deny", "version", "duplicate package", "unsorted"} {
		t.Run(scenario, func(t *testing.T) {
			c := OfflineContent{Format: 1, ManifestSHA256: strings.Repeat("a", 64), InstalledStatusSHA256: strings.Repeat("b", 64), KeyringSHA256: strings.Repeat("c", 64), Suite: "noble-updates", Component: "main", Architecture: "amd64", Requested: []ExactPackage{{Name: "liblzma5", Version: "5.6.1+really5.4.5-1ubuntu0.4"}}}
			mode, allow, deny := "airgapped", []string{"liblzma5"}, []string(nil)
			switch scenario {
			case "direct", "auto":
				mode = scenario
			case "digest":
				c.ManifestSHA256 = strings.Repeat("A", 64)
			case "format":
				c.Format = 2
			case "selector":
				c.Suite = "noble\ntrusted=yes"
			case "scope":
				allow = []string{"other"}
			case "duplicate scope":
				c.Requested = append(c.Requested, ExactPackage{Name: "zlib1g", Version: "1"})
				allow = []string{"liblzma5", "liblzma5"}
			case "deny":
				deny = []string{"other"}
			case "version":
				c.Requested[0].Version = "1;touch"
			case "duplicate package":
				c.Requested = append(c.Requested, c.Requested[0])
				allow = append(allow, "liblzma5")
			case "unsorted":
				c.Requested = append([]ExactPackage{{Name: "zlib1g", Version: "1"}}, c.Requested...)
				allow = append(allow, "zlib1g")
			}
			err := c.Validate(mode, allow, deny)
			if scenario == "valid" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
	var missing *OfflineContent
	require.NoError(t, missing.Validate("direct", nil, nil))
}
