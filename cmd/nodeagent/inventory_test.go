package main

import "testing"

func TestInferServerPurposesFromPackages(t *testing.T) {
	purposes := inferServerPurposesFromPackages([]PackageInfo{
		{Name: "postgresql-16", Version: "16.3"},
		{Name: "haproxy", Version: "2.9"},
		{Name: "redis-server", Version: "7.0"},
		{Name: "tomcat9", Version: "9.0"},
		{Name: "prometheus-node-exporter", Version: "1.7"},
	})
	got := map[string]ServerPurpose{}
	for _, p := range purposes {
		got[p.Purpose] = p
	}
	for _, want := range []string{"db_node", "load_balancer", "cache_server", "app_node", "monitoring_server"} {
		p, ok := got[want]
		if !ok {
			t.Fatalf("missing inferred purpose %q from %#v", want, purposes)
		}
		if p.Confidence <= 0 || len(p.Evidence) == 0 {
			t.Fatalf("purpose %q missing confidence/evidence: %#v", want, p)
		}
	}
}
