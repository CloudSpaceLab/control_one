package patchsupport

import (
	"strings"
	"testing"
)

func TestRepositoryProxyURLRejectsInjectionAndAmbiguousComponents(t *testing.T) {
	for _, raw := range []string{"http://proxy:3128", "https://proxy", "http://[::1]:3128"} {
		if err := ProxyURL(raw); err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
	}
	for _, raw := range []string{"", "file:///tmp/repo", "http://user:secret@proxy", "http://proxy/path", "http://proxy?direct=true", "http://proxy/#fragment", "http://proxy:0", "http://proxy:65536", "http://proxy\";Acquire::AllowInsecureRepositories true;", "http://proxy\ninclude bad", "http://proxy\\evil"} {
		if ProxyURL(raw) == nil {
			t.Fatalf("accepted unsafe proxy %q", raw)
		}
	}
}

func TestSquidAllowlistAlwaysValidatedAndRestrictsPorts(t *testing.T) {
	for _, hosts := range [][]string{nil, {""}, {"."}, {"repo.example\nhttp_access allow all"}, {"*"}} {
		if _, err := SquidConfig(hosts); err == nil {
			t.Fatalf("accepted %q", hosts)
		}
	}
	body, err := SquidConfig([]string{"archive.ubuntu.com", ".ubuntu.com"})
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{"acl local_clients src 127.0.0.0/8 ::1", "http_access deny !Safe_ports", "http_access deny CONNECT !SSL_ports", "http_access allow local_clients allowed_hosts", "http_access deny all"} {
		if !strings.Contains(string(body), part) {
			t.Fatalf("missing %s", part)
		}
	}
}
