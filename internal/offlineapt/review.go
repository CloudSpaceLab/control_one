package offlineapt

import (
	"fmt"
	"strings"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/patchsupport"
)

// reviewResolvedPlan reads the same complete dpkg baseline pinned by staging.
// It describes package scope; maintainer scripts and reboot impact remain unknown.
func reviewResolvedPlan(status []byte, requested, resolved []Selection, arch string, now time.Time) (*patchsupport.OfflineReview, error) {
	installed := map[string]string{}
	for _, stanza := range strings.Split(strings.ReplaceAll(string(status), "\r\n", "\n"), "\n\n") {
		fields := map[string]string{}
		for _, line := range strings.Split(stanza, "\n") {
			if line == "" || line[0] == ' ' || line[0] == '\t' {
				continue
			}
			key, value, ok := strings.Cut(line, ":")
			if ok {
				fields[key] = strings.TrimSpace(value)
			}
		}
		if fields["Status"] != "install ok installed" {
			continue
		}
		name, version, architecture := fields["Package"], fields["Version"], fields["Architecture"]
		if !packageName.MatchString(name) || !exactVersion.MatchString(version) || !part.MatchString(architecture) {
			return nil, fmt.Errorf("cannot review malformed installed package baseline")
		}
		key := name + ":" + architecture
		if _, exists := installed[key]; exists {
			return nil, fmt.Errorf("ambiguous installed package baseline")
		}
		installed[key] = version
	}
	selected := map[string]bool{}
	for _, p := range requested {
		selected[p.Name] = true
	}
	r := &patchsupport.OfflineReview{PreparedAt: now, ExpiresAt: now.Add(time.Hour), Reboot: "unknown", ServiceImpact: "requires_operator_review", Changes: []patchsupport.PlanChange{}}
	for _, p := range resolved {
		name, qualifier, qualified := strings.Cut(p.Name, ":")
		if !qualified {
			qualifier = arch
		}
		before := installed[name+":"+qualifier]
		if all, exists := installed[name+":all"]; exists {
			if before != "" {
				return nil, fmt.Errorf("ambiguous native/all installed version")
			}
			before = all
		}
		role := "dependency"
		if selected[p.Name] {
			role = "requested"
		}
		r.Changes = append(r.Changes, patchsupport.PlanChange{Name: p.Name, InstalledVersion: before, ProposedVersion: p.Version, Role: role})
	}
	return r, nil
}

// ReviewResolvedPlan describes exact APT operations against a complete dpkg baseline.
// It does not predict service/reboot impact or authorize installation.
func ReviewResolvedPlan(status []byte, requested, resolved []Selection, arch string, now time.Time) (*patchsupport.OfflineReview, error) {
	return reviewResolvedPlan(status, requested, resolved, arch, now)
}
