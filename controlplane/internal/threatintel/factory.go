package threatintel

import (
	"errors"
	"strings"
)

// SourceFromConfig builds a Source for a stored threat_feed row. The url +
// apiKey arguments come straight from the row (apiKey already unsealed by
// the caller). Unknown feed_type values return an error.
func SourceFromConfig(feedType, url, apiKey, category string) (Source, error) {
	switch strings.TrimSpace(feedType) {
	case "spamhaus_drop":
		return SpamhausDROP{URL: url}, nil
	case "spamhaus_edrop":
		return SpamhausEDROP{URL: url}, nil
	case "firehol_l1":
		return FireHOLLevel1{URL: url}, nil
	case "tor_exit":
		return TorExitNodes{URL: url}, nil
	case "abuseipdb":
		return AbuseIPDBBlocklist{APIKey: apiKey}, nil
	case "otx":
		return AlienVaultOTX{APIKey: apiKey}, nil
	case "custom_lines":
		if url == "" {
			return nil, errors.New("custom_lines feed requires url")
		}
		return CustomLineList{URL: url, FeedName: "custom-" + url, Category: category, Score: 70}, nil
	case "custom_spamhaus":
		if url == "" {
			return nil, errors.New("custom_spamhaus feed requires url")
		}
		return CustomSpamhausFormat{URL: url, FeedName: "custom-" + url}, nil
	default:
		return nil, errors.New("unknown feed_type")
	}
}
