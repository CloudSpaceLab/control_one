package logs

import "github.com/CloudSpaceLab/control_one/internal/config"

type haproxyFormatter struct{}

func (haproxyFormatter) Format(raw RawLog, source config.LogSourceConfig) (StructuredLog, error) {
	if structured, ok := formatJSONAccessLog(raw, source, "haproxy"); ok {
		return structured, nil
	}
	return defaultFormatter{}.Format(raw, source)
}

func init() {
	RegisterFormatter("haproxy", haproxyFormatter{})
}
