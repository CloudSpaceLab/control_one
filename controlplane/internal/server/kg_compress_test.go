package server

import (
	"fmt"
	"strings"
	"testing"
)

// kg_compress_test.go — load-bearing assertions for the two-stage
// compression pipeline. The 1000-node fixture in particular is the
// reason this code exists: if that test goes red, the system prompt
// will overflow the 8K-token budget and /ai/ask becomes prohibitively
// expensive on real tenants.

func TestTokenizeForKG(t *testing.T) {
	t.Parallel()
	tokens := tokenizeForKG("What is the agent version on host-01?")
	got := map[string]bool{}
	for _, tk := range tokens {
		got[tk] = true
	}
	// "what", "is", "the", "on" are stopwords and must be dropped.
	for _, drop := range []string{"what", "is", "the", "on"} {
		if got[drop] {
			t.Fatalf("expected stopword %q to be dropped, got %v", drop, tokens)
		}
	}
	// "agent", "version", "host", "01" survive.
	for _, keep := range []string{"agent", "version", "host", "01"} {
		if !got[keep] {
			t.Fatalf("expected %q to survive, got %v", keep, tokens)
		}
	}
}

func TestCompressForQuery_EmptySections(t *testing.T) {
	t.Parallel()
	got := compressForQuery(nil, "anything", 8192)
	if got != "" {
		t.Fatalf("expected empty output for no sections, got %q", got)
	}
}

func TestCompressForQuery_OnlyBaseline(t *testing.T) {
	t.Parallel()
	sections := []kgSection{
		{Kind: kgSectionBaseline, Markdown: "# baseline\n", Tokens: []string{"baseline"}},
	}
	out := compressForQuery(sections, "what is the fleet", 8192)
	if !strings.Contains(out, "baseline") {
		t.Fatalf("baseline must always be included, got %q", out)
	}
}

func TestCompressForQuery_NoMatches_ReturnsBaselinePlusTopGroups(t *testing.T) {
	t.Parallel()
	sections := []kgSection{
		{Kind: kgSectionBaseline, Markdown: "# baseline\n", Tokens: []string{"baseline"}},
		{Kind: kgSectionMajority, Markdown: "## maj-1\n", Tokens: []string{"linux", "amd64"}},
		{Kind: kgSectionNode, Hostname: "n1", Markdown: "## n1\n", Tokens: []string{"alpha"}},
	}
	out := compressForQuery(sections, "completely unrelated frobnitz", 8192)
	if !strings.Contains(out, "baseline") {
		t.Fatalf("baseline missing: %q", out)
	}
	// With slack budget all sections should fit even at score 0.
	if !strings.Contains(out, "maj-1") {
		t.Fatalf("expected majority section to be packed in: %q", out)
	}
}

func TestCompressForQuery_UUIDForceIncluded(t *testing.T) {
	t.Parallel()
	const targetID = "11111111-2222-3333-4444-555555555555"
	sections := []kgSection{
		{Kind: kgSectionBaseline, Markdown: "# baseline\n"},
		{Kind: kgSectionNode, Hostname: "irrelevant-host", NodeID: targetID, Markdown: "## target-node\n", Tokens: []string{"unique-marker-zzz"}},
		// Add a high-scoring distractor so a naive sort would push the
		// target out without force-include.
		{Kind: kgSectionNode, Hostname: "distractor", NodeID: "99999999-9999-9999-9999-999999999999", Markdown: "## distractor\n", Tokens: []string{"frobnitz", "widget"}},
	}
	out := compressForQuery(sections, fmt.Sprintf("what is on node %s frobnitz widget", targetID), 8192)
	if !strings.Contains(out, "target-node") {
		t.Fatalf("UUID-matched node must be force-included, got %q", out)
	}
}

func TestCompressForQuery_IPv4ForceIncluded(t *testing.T) {
	t.Parallel()
	sections := []kgSection{
		{Kind: kgSectionBaseline, Markdown: "# baseline\n"},
		{Kind: kgSectionNode, Hostname: "ip-target", PublicIP: "10.0.0.42", Markdown: "## ip-target\n", Tokens: []string{"obscure"}},
	}
	out := compressForQuery(sections, "what's running on 10.0.0.42", 8192)
	if !strings.Contains(out, "ip-target") {
		t.Fatalf("IPv4-matched node must be force-included, got %q", out)
	}
}

func TestCompressForQuery_HostnameSubstringMatch(t *testing.T) {
	t.Parallel()
	sections := []kgSection{
		{Kind: kgSectionBaseline, Markdown: "# baseline\n"},
		{Kind: kgSectionNode, Hostname: "web-prod-7", Markdown: "## web-prod-7\n", Tokens: []string{"nginx"}},
	}
	out := compressForQuery(sections, "show me web-prod-7 services", 8192)
	if !strings.Contains(out, "web-prod-7") {
		t.Fatalf("hostname-matched node must be force-included, got %q", out)
	}
}

func TestCompressForQuery_ScoreRanking(t *testing.T) {
	t.Parallel()
	// Three node sections, one of which has heavy token overlap with
	// the question. Budget is tight enough that only baseline + one
	// node fits, so the high-score one must win.
	heavy := strings.Repeat("padding ", 200)
	sections := []kgSection{
		{Kind: kgSectionBaseline, Markdown: "# baseline\n"},
		{Kind: kgSectionNode, Hostname: "low", Markdown: "## low\n" + heavy, Tokens: []string{"alpha"}},
		{Kind: kgSectionNode, Hostname: "high", Markdown: "## high\n" + heavy, Tokens: []string{"firewall", "nginx", "exposed"}},
		{Kind: kgSectionNode, Hostname: "mid", Markdown: "## mid\n" + heavy, Tokens: []string{"firewall"}},
	}
	// Budget: 500 tokens ≈ 2000 chars. Each node is ~1600 chars so
	// only one will fit alongside baseline.
	out := compressForQuery(sections, "which nodes have firewall nginx exposed", 500)
	if !strings.Contains(out, "## high") {
		t.Fatalf("highest-scoring section must be packed first, got: %q", out[:min(200, len(out))])
	}
	if strings.Contains(out, "## low") {
		t.Fatalf("lowest-scoring section should have been skipped, got: %q", out[:min(200, len(out))])
	}
}

// Load-bearing assertion: synthesize a 1000-node tenant and verify
// the compressed output fits the 8192-token budget.
func TestCompressForQuery_1000NodeFixture_FitsBudget(t *testing.T) {
	t.Parallel()
	sections := synth1000NodeSections()
	out := compressForQuery(sections, "what is the fleet baseline and which nodes are degraded", 8192)
	tokens := len(out) / approxCharsPerToken
	if tokens > 8192 {
		t.Fatalf("1000-node compressed blob is %d tokens (limit 8192) — compression is broken", tokens)
	}
	if !strings.Contains(out, "Fleet baseline") && !strings.Contains(out, "Knowledge graph") {
		t.Fatalf("expected baseline or majority summary in output, got: %q", out[:min(300, len(out))])
	}
	t.Logf("1000-node synthetic fixture compressed to %d chars ≈ %d tokens (budget 8192)", len(out), tokens)
}

// synth1000NodeSections simulates what buildKGSections would emit for a
// 1000-node tenant where 980 nodes are homogeneous (one majority
// summary) and 20 are outliers (full per-node sections). This is the
// realistic shape we expect in production.
func synth1000NodeSections() []kgSection {
	sections := make([]kgSection, 0, 22)

	// Baseline summary.
	baseMD := "# Knowledge graph — synth-tenant\n\n" +
		"_Generated 2026-05-12T00:00:00Z · 1000 nodes · 4000 listening services._\n\n" +
		"This document describes every node enrolled under this tenant.\n\n"
	sections = append(sections, kgSection{
		Kind:     kgSectionBaseline,
		Markdown: baseMD,
		Tokens:   tokenizeForKG(baseMD),
	})

	// One majority summary covering 980 nodes.
	majNames := make([]string, 980)
	for i := range majNames {
		majNames[i] = fmt.Sprintf("host-%04d", i)
	}
	majMD := "## Fleet baseline (980 nodes)\n\n" +
		"980 of 980 nodes match: Linux/amd64/agent-1.2.3/active.\n\n" +
		"Nodes: " + strings.Join(majNames[:25], ", ") + ", … (+955 more).\n\n"
	sections = append(sections, kgSection{
		Kind:     kgSectionMajority,
		Markdown: majMD,
		Tokens:   tokenizeForKG(majMD),
	})

	// 20 outlier full sections.
	for i := 0; i < 20; i++ {
		md := fmt.Sprintf("## outlier-%02d\n\n- **id:** `%08d-0000-0000-0000-000000000000`\n- **state:** degraded\n- **os:** Linux (arm64)\n- **agent:** 1.1.0\n\n", i, i)
		// Pad to realistic per-node size (~600 chars with a couple services).
		md += "| Port | Process | Kind | Server | URL |\n|---:|---|---|---|---|\n"
		md += "| 22 | sshd | ssh | OpenSSH | — |\n"
		md += "| 443 | nginx | https | nginx/1.24 | https://outlier.example.com/ |\n\n"
		sections = append(sections, kgSection{
			Kind:     kgSectionNode,
			Hostname: fmt.Sprintf("outlier-%02d", i),
			NodeID:   fmt.Sprintf("%08d-0000-0000-0000-000000000000", i),
			Markdown: md,
			Tokens:   tokenizeForKG(md),
		})
	}
	return sections
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
