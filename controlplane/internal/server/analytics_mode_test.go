package server

import (
	"testing"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
)

func TestEffectiveAnalyticsMode(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.Config
		want string
	}{
		{name: "nil defaults small", want: analyticsModeSmall},
		{
			name: "explicit small wins over Doris config",
			cfg: &config.Config{
				Analytics: config.AnalyticsConfig{Mode: "small"},
				Doris:     config.DorisConfig{Enabled: true, DSN: "root:secret@tcp(doris-fe:9030)/controlone"},
			},
			want: analyticsModeSmall,
		},
		{
			name: "explicit olap",
			cfg: &config.Config{
				Analytics: config.AnalyticsConfig{Mode: "olap"},
			},
			want: analyticsModeOLAP,
		},
		{
			name: "auto with configured Doris",
			cfg: &config.Config{
				Analytics: config.AnalyticsConfig{Mode: "auto"},
				Doris:     config.DorisConfig{Enabled: true, DSN: "root:secret@tcp(doris-fe:9030)/controlone"},
			},
			want: analyticsModeOLAP,
		},
		{
			name: "auto without Doris",
			cfg: &config.Config{
				Analytics: config.AnalyticsConfig{Mode: "auto"},
				Doris:     config.DorisConfig{Enabled: false},
			},
			want: analyticsModeSmall,
		},
		{
			name: "disabled",
			cfg: &config.Config{
				Analytics: config.AnalyticsConfig{Mode: "disabled"},
			},
			want: analyticsModeDisabled,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := effectiveAnalyticsMode(tt.cfg); got != tt.want {
				t.Fatalf("effectiveAnalyticsMode() = %q, want %q", got, tt.want)
			}
		})
	}
}
