package threatintel

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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

func TestIndicatorSetLookupSkipsNonPublicBogons(t *testing.T) {
	inds := []Indicator{
		{CIDR: "127.0.0.0/8", Feed: "firehol-level1", Score: 80},
		{CIDR: "172.16.0.0/12", Feed: "firehol-level1", Score: 80},
		{CIDR: "10.0.0.0/8", Feed: "firehol-level1", Score: 80},
		{CIDR: "192.168.0.0/16", Feed: "firehol-level1", Score: 80},
		{CIDR: "1.2.3.0/24", Feed: "spamhaus-drop", Score: 100},
	}
	set := buildSet(inds)

	for _, raw := range []string{"127.0.0.1", "172.18.0.9", "10.0.0.1", "192.168.1.1"} {
		if _, ok := set.LookupIP(net.ParseIP(raw)); ok {
			t.Fatalf("non-public address %s should not match blacklist indicators", raw)
		}
	}
	if match, ok := set.LookupIP(net.ParseIP("1.2.3.42")); !ok || match.Feed != "spamhaus-drop" {
		t.Fatalf("public blacklist lookup = %+v, %v; want spamhaus-drop hit", match, ok)
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

func TestManagerUsesLocalSnapshotWhenRefreshFails(t *testing.T) {
	dir := t.TempDir()
	src := &flakySource{
		name: "abuseipdb",
		inds: []Indicator{
			{IP: "45.135.193.156", Feed: "abuseipdb", Category: "abuse", Score: 100, FirstSeen: time.Now().UTC()},
		},
	}
	m := New(Config{SnapshotDir: dir, Sources: []Source{src}}, nil)
	m.refreshOnce(context.Background())
	if m.Current() == nil {
		t.Fatal("expected current set after first refresh")
	}

	src.err = errors.New("quota exhausted")
	m.refreshOnce(context.Background())
	set := m.Current()
	if set == nil {
		t.Fatal("expected stale snapshot to keep current set alive")
	}
	match, ok := set.LookupIP(net.ParseIP("45.135.193.156"))
	if !ok {
		t.Fatal("expected stale snapshot lookup hit")
	}
	if match.Feed != "abuseipdb" || match.Score != 100 {
		t.Fatalf("unexpected stale match: %+v", match)
	}
}

func TestSnapshotExists(t *testing.T) {
	dir := t.TempDir()
	if SnapshotExists(dir, "static", "abuseipdb") {
		t.Fatal("snapshot should not exist before save")
	}
	if err := saveSnapshot(dir, "static", "abuseipdb", []Indicator{{IP: "1.2.3.4", Feed: "abuseipdb", Score: 90}}); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}
	if !SnapshotExists(dir, "static", "abuseipdb") {
		t.Fatal("snapshot should exist after save")
	}
}

type flakySource struct {
	name string
	inds []Indicator
	err  error
}

func (f *flakySource) Name() string { return f.name }

func (f *flakySource) Fetch(context.Context, *http.Client) ([]Indicator, error) {
	if f.err != nil {
		return nil, f.err
	}
	return append([]Indicator(nil), f.inds...), nil
}
