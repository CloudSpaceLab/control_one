package main

import (
	"os"
	"os/exec"
	"testing"
)

func TestCompleteNativeInventoryRejectsTruncationAndIgnoresUninstalledDpkgRecords(t *testing.T) {
	rows, err := parseCompleteNativeInventory("demo\t1\tamd64\tinstalled\nremoved\t1\tamd64\tconfig-files\n", "dpkg")
	if err != nil || len(rows) != 1 || rows[0].Name != "demo" {
		t.Fatalf("unexpected complete inventory: %+v %v", rows, err)
	}
	for _, body := range []string{"demo\t1\n", "demo\t\tamd64\tinstalled\n", "truncated"} {
		if _, err := parseCompleteNativeInventory(body, "dpkg"); err == nil {
			t.Fatalf("accepted truncated output: %q", body)
		}
	}
	if rows, err := parseCompleteNativeInventory("", "dpkg"); err != nil || len(rows) != 0 || hashPackages(rows) == "" {
		t.Fatal("complete empty snapshot must have an identity")
	}
}

func TestRPMInventoryPreservesEpoch(t *testing.T) {
	packages := parseRpmQA("demo\t7:1.0-2.el9\tx86_64\n")
	if len(packages) != 1 || packages[0].Version != "7:1.0-2.el9" {
		t.Fatalf("lost RPM EVR: %+v", packages)
	}
	lower := parseRpmQA("demo\t0:1.0-2.el9\tx86_64\n")
	if hashPackages(packages) == hashPackages(lower) {
		t.Fatal("epoch change did not change inventory generation")
	}
}

func TestRPMInventoryNativeHeaderFormat(t *testing.T) {
	if os.Getenv("CONTROLONE_NATIVE_VERSION_ORACLE") != "1" {
		t.Skip("disposable python-rpm fixture required")
	}
	script := `import rpm,sys
h=rpm.hdr();h['name']='demo';h['version']='1.0';h['release']='2.el9';h['arch']='x86_64';h['epoch']=7
print(h.sprintf(sys.argv[1]),end='')`
	out, err := exec.Command("python3", "-c", script, rpmPackageQueryFormat).CombinedOutput()
	if err != nil {
		t.Fatalf("native header format: %v: %s", err, out)
	}
	packages := parseRpmQA(string(out))
	if len(packages) != 1 || packages[0].Version != "7:1.0-2.el9" {
		t.Fatalf("native query lost epoch: %s", out)
	}
}

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
