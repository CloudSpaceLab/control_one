package packageinventory

import "time"

// Health is an authenticated, time-bounded host observation. Missing is unknown;
// a command exit or heartbeat connection alone is never service/reboot proof.
type Health struct {
	ObservedAt        time.Time         `json:"observed_at"`
	BootID            string            `json:"boot_id"`
	RebootRequired    *bool             `json:"reboot_required"`
	PackageAudit      string            `json:"package_audit"`
	ServiceHealth     string            `json:"service_health"`
	ServicesComplete  bool              `json:"services_complete"`
	ConsumersComplete bool              `json:"consumers_complete"`
	ActiveServices    []string          `json:"active_services,omitempty"`
	ServiceChecks     map[string]string `json:"service_checks,omitempty"`
}
