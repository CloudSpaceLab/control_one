package server

import (
	"regexp"
	"sort"
	"strings"
)

// kg_compress.go — stage 2 of the two-stage KG-A compression pipeline.
//
// Stage 1 (knowledge_graph.go::buildKGSections) collapses homogeneous
// nodes into majority summaries and emits []kgSection. This file is the
// per-request stage: given a user question + a section budget, it picks
// the most relevant sections and concatenates their pre-rendered
// markdown into the final system-prompt blob.
//
// Design (timeline §11 D5 — A1 algorithmic compression):
//   - Tokenize the question into a lowercased alphanumeric word set.
//   - Score each section by tokens shared with the question.
//   - Always include the fleet baseline summary (kgSectionBaseline) and
//     any section whose hostname / node id / public IP appears in the
//     question (UUID + IPv4 regex match).
//   - Sort the remaining sections by score descending, greedy-pack until
//     the token budget is reached (≈ 4 chars per token).

// approxCharsPerToken is the cheap heuristic we use to size the
// compressed blob against an 8K-token budget without an actual
// tokenizer. Anthropic / OpenAI tokenizers average ~4 chars/token for
// English + markdown, so dividing rendered-length by 4 is a safe upper
// bound for budgeting.
const approxCharsPerToken = 4

// kgStopwords are tossed during tokenization so a question like "what
// is the agent version on node X" doesn't match every section because
// of "the" / "is". Tiny set — we only need to suppress the cheapest
// false positives.
var kgStopwords = map[string]struct{}{
	"a": {}, "an": {}, "the": {}, "is": {}, "are": {}, "was": {}, "were": {},
	"do": {}, "does": {}, "did": {}, "of": {}, "to": {}, "in": {}, "on": {},
	"at": {}, "for": {}, "and": {}, "or": {}, "what": {}, "which": {},
	"how": {}, "why": {}, "where": {}, "when": {}, "show": {}, "list": {},
	"give": {}, "tell": {}, "me": {}, "my": {}, "any": {}, "all": {},
	"this": {}, "that": {}, "with": {}, "from": {}, "by": {}, "be": {},
}

var (
	kgWordRe = regexp.MustCompile(`[a-z0-9]+`)
	kgUUIDRe = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)
	kgIPv4Re = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
)

// tokenizeForKG produces the lowercased token set used for both
// section scoring (at build-time) and question matching (at compress
// time). Stable across the two paths so token-overlap is meaningful.
func tokenizeForKG(s string) []string {
	if s == "" {
		return nil
	}
	matches := kgWordRe.FindAllString(strings.ToLower(s), -1)
	out := make([]string, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, w := range matches {
		if len(w) < 2 {
			continue
		}
		if _, drop := kgStopwords[w]; drop {
			continue
		}
		if _, dup := seen[w]; dup {
			continue
		}
		seen[w] = struct{}{}
		out = append(out, w)
	}
	return out
}

// compressForQuery is the public entrypoint. Given the build-time
// section slice and a user question, it returns the markdown blob to
// hand to the LLM as grounded context, sized to ≤ budget tokens.
//
// Force-include rules:
//   - kgSectionBaseline is always first and always present.
//   - Any section whose Hostname / NodeID / PublicIP appears literally
//     in the question is included regardless of score (so "what's
//     running on 10.0.0.5" always pulls the matching node section).
//
// Remaining sections are sorted by token-overlap score and greedy-
// packed until adding the next would exceed the budget.
func compressForQuery(sections []kgSection, question string, budget int) string {
	if budget <= 0 {
		budget = 8192
	}
	if len(sections) == 0 {
		return ""
	}

	qTokens := tokenSet(tokenizeForKG(question))
	qLower := strings.ToLower(question)
	forcedIDs := extractForcedIdentifiers(question)

	var (
		baseline []kgSection
		forced   []kgSection
		rest     []kgSection
	)
	seenIdx := make(map[int]struct{}, len(sections))

	for i, sec := range sections {
		if sec.Kind == kgSectionBaseline {
			baseline = append(baseline, sec)
			seenIdx[i] = struct{}{}
		}
	}

	for i, sec := range sections {
		if _, dup := seenIdx[i]; dup {
			continue
		}
		if sectionMatchesIdentifiers(sec, qLower, forcedIDs) {
			forced = append(forced, sec)
			seenIdx[i] = struct{}{}
		}
	}

	type scored struct {
		section kgSection
		score   int
	}
	scoredRest := make([]scored, 0, len(sections))
	for i, sec := range sections {
		if _, dup := seenIdx[i]; dup {
			continue
		}
		if sec.Kind == kgSectionLookup {
			continue
		}
		scoredRest = append(scoredRest, scored{section: sec, score: overlapScore(sec.Tokens, qTokens)})
	}
	sort.SliceStable(scoredRest, func(i, j int) bool {
		if scoredRest[i].score != scoredRest[j].score {
			return scoredRest[i].score > scoredRest[j].score
		}
		// Tie-break: majority sections before node sections, then by
		// hostname for determinism. Keeps output stable for tests +
		// for caching downstream of this function.
		if scoredRest[i].section.Kind != scoredRest[j].section.Kind {
			return scoredRest[i].section.Kind < scoredRest[j].section.Kind
		}
		return scoredRest[i].section.Hostname < scoredRest[j].section.Hostname
	})
	for _, s := range scoredRest {
		rest = append(rest, s.section)
	}

	// Pack: baseline first, then forced (in original order), then
	// score-ranked rest until the budget is hit.
	budgetChars := budget * approxCharsPerToken
	var b strings.Builder
	for _, sec := range baseline {
		b.WriteString(sec.Markdown)
	}
	for _, sec := range forced {
		// Force-included sections bypass the budget check — they are
		// load-bearing for the user's question. In pathological cases
		// (a question naming 100 nodes) this can slightly exceed the
		// budget, which is preferable to dropping the asked-about
		// node from context.
		b.WriteString(sec.Markdown)
	}
	for _, sec := range rest {
		if b.Len()+len(sec.Markdown) > budgetChars {
			continue
		}
		b.WriteString(sec.Markdown)
	}
	return b.String()
}

// tokenSet turns a token slice into a lookup set. Mirrors the dedup
// already done in tokenizeForKG so this is mostly a type-conversion.
func tokenSet(tokens []string) map[string]struct{} {
	out := make(map[string]struct{}, len(tokens))
	for _, t := range tokens {
		out[t] = struct{}{}
	}
	return out
}

// overlapScore counts how many of a section's tokens appear in the
// question's token set. Cheap O(|section tokens|) lookup.
func overlapScore(sectionTokens []string, questionTokens map[string]struct{}) int {
	if len(questionTokens) == 0 {
		return 0
	}
	n := 0
	for _, t := range sectionTokens {
		if _, ok := questionTokens[t]; ok {
			n++
		}
	}
	return n
}

// extractForcedIdentifiers pulls UUID + IPv4 substrings out of the
// question. These never go through tokenization (they contain '-' and
// '.' which the tokenizer splits) so we regex-match them separately
// and compare against section identifiers directly.
func extractForcedIdentifiers(question string) []string {
	out := make([]string, 0, 4)
	for _, m := range kgUUIDRe.FindAllString(question, -1) {
		out = append(out, strings.ToLower(m))
	}
	out = append(out, kgIPv4Re.FindAllString(question, -1)...)
	return out
}

// sectionMatchesIdentifiers returns true when the section is uniquely
// identified by something in the question — either by an explicit
// UUID/IP regex match against its node id / public ip, or by its
// hostname appearing as a substring in the question text.
func sectionMatchesIdentifiers(sec kgSection, qLower string, forced []string) bool {
	if sec.Kind != kgSectionNode && sec.Kind != kgSectionLookup {
		return false
	}
	for _, f := range forced {
		if sec.NodeID != "" && strings.EqualFold(sec.NodeID, f) {
			return true
		}
		if sec.PublicIP != "" && sec.PublicIP == f {
			return true
		}
	}
	if h := strings.ToLower(strings.TrimSpace(sec.Hostname)); h != "" && len(h) >= 3 {
		if strings.Contains(qLower, h) {
			return true
		}
	}
	return false
}
