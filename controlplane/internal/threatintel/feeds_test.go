package threatintel

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseSpamhaus(t *testing.T) {
	body := []byte(`; Spamhaus DROP - 2026-04-24
;
1.10.16.0/20 ; SBL233995
5.42.113.0/24 ; SBL999999
`)
	inds := parseSpamhaus(body, "spamhaus-drop")
	if len(inds) != 2 {
		t.Fatalf("want 2 inds, got %d", len(inds))
	}
	if inds[0].CIDR != "1.10.16.0/20" {
		t.Fatalf("first cidr wrong: %s", inds[0].CIDR)
	}
	if inds[0].Score != 100 {
		t.Fatalf("expected high score, got %d", inds[0].Score)
	}
}

func TestParseLineListWithComments(t *testing.T) {
	body := []byte(`# header
1.2.3.4
5.6.7.8/24 # noisy scanner
; alt comment
9.9.9.9
`)
	inds := parseLineList(body, "test", "scan", 50)
	if len(inds) != 3 {
		t.Fatalf("want 3 inds, got %d", len(inds))
	}
	for _, ind := range inds {
		if ind.Feed != "test" {
			t.Fatalf("feed wrong: %s", ind.Feed)
		}
	}
}

func TestIndicatorSetLookup(t *testing.T) {
	inds := []Indicator{
		{CIDR: "1.2.3.0/24", Feed: "x", Score: 90},
		{IP: "8.8.8.8", Feed: "y", Score: 50},
	}
	set := buildSet(inds)

	if _, ok := set.LookupIP(net.ParseIP("1.2.3.42")); !ok {
		t.Fatal("CIDR lookup miss")
	}
	if _, ok := set.LookupIP(net.ParseIP("8.8.8.8")); !ok {
		t.Fatal("single IP lookup miss")
	}
	if _, ok := set.LookupIP(net.ParseIP("9.9.9.9")); ok {
		t.Fatal("should not match unrelated ip")
	}
}

func TestIndicatorSetLookupTenantScopedFeeds(t *testing.T) {
	inds := []Indicator{
		{CIDR: "1.2.3.0/24", Feed: "global", Score: 80},
		{CIDR: "1.2.3.0/24", TenantID: "tenant-a", Feed: "tenant", Score: 100},
	}
	set := buildSet(inds)
	matches := set.LookupIPAll(net.ParseIP("1.2.3.42"), "tenant-a")
	if len(matches) != 2 || matches[0].Feed != "tenant" {
		t.Fatalf("expected tenant and global matches sorted by score, got %+v", matches)
	}
	matches = set.LookupIPAll(net.ParseIP("1.2.3.42"), "tenant-b")
	if len(matches) != 1 || matches[0].Feed != "global" {
		t.Fatalf("expected only global match for another tenant, got %+v", matches)
	}
}

func TestSpamhausFetchHTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("1.2.3.0/24 ; SBL1\n"))
	}))
	defer server.Close()
	src := SpamhausDROP{URL: server.URL}
	inds, err := src.Fetch(context.Background(), http.DefaultClient)
	if err != nil {
		t.Fatal(err)
	}
	if len(inds) != 1 || inds[0].CIDR != "1.2.3.0/24" {
		t.Fatalf("unexpected inds: %+v", inds)
	}
}

func TestManagerSubscribeReceivesSnapshot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("1.2.3.0/24 ; SBL1\n"))
	}))
	defer server.Close()
	m := New(Config{Sources: []Source{SpamhausDROP{URL: server.URL}}}, nil)
	m.refreshOnce(context.Background())
	if m.Current() == nil {
		t.Fatal("expected snapshot after refresh")
	}
}
