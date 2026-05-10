// Package sanctions implements the watchlist / sanctions match scanner.
//
// The scanner deliberately refuses to match a subject whose date of birth is
// unknown. An earlier prototype of this code substituted a hardcoded fallback
// (`1962-11-23`) when the caller did not supply a DOB. That silently produced
// wrong-person hits against any real watchlist entry born on that day and
// masked the input gap from compliance review. See bugs §4 #3 in
// docs/incomplete-features-and-bugs.md.
//
// The contract is now: callers must either supply DOB, or accept that
// the scanner will refuse the match and bubble the requirement up to the
// onboarding / KYC layer.
package sanctions

import (
	"context"
	"errors"
	"strings"
	"time"
)

// ErrMissingDOB is returned by Scanner.Match when the request omits the
// subject's date of birth. Callers must treat this as "cannot evaluate" —
// not as a clean match nor a clean miss.
var ErrMissingDOB = errors.New("sanctions: cannot scan without subject date of birth")

// ErrMissingFullName is returned when the request omits the subject's full
// name. A sanctions scan with no name cannot disambiguate hits.
var ErrMissingFullName = errors.New("sanctions: cannot scan without subject full name")

// Request describes a single subject to evaluate against the sanctions list.
type Request struct {
	// FullName is the subject's full legal name. Required.
	FullName string

	// BirthDate is the subject's date of birth. Required.
	//
	// A nil pointer means the caller has no value — the scanner WILL NOT
	// substitute a default; it returns ErrMissingDOB. To express "intentionally
	// scan without DOB" the caller must build a different code path (e.g. an
	// explicit name-only screening API gated by a privileged role).
	BirthDate *time.Time
}

// MatchResult captures the outcome of a single scan.
type MatchResult struct {
	// Hits is the list of watchlist entries that matched the subject. Empty
	// slice means a clean miss; nil never occurs on a successful return.
	Hits []Hit

	// ScannedAt is the timestamp when the scan was performed (UTC).
	ScannedAt time.Time
}

// Hit is a single watchlist match.
type Hit struct {
	ListID  string
	EntryID string
	Score   float64
	Source  string
}

// Backend is the upstream watchlist source (e.g. Moov Watchman). It is
// intentionally tiny so the scanner stays trivial to test.
type Backend interface {
	Lookup(ctx context.Context, req Request) ([]Hit, error)
}

// Scanner runs sanctions matches against an injected Backend.
type Scanner struct {
	backend Backend
	now     func() time.Time
}

// NewScanner constructs a Scanner. backend must be non-nil; pass a fake in
// tests.
func NewScanner(backend Backend) *Scanner {
	return &Scanner{
		backend: backend,
		now:     func() time.Time { return time.Now().UTC() },
	}
}

// Match performs a sanctions screening for the given subject.
//
// Returns ErrMissingDOB if req.BirthDate is nil and ErrMissingFullName if
// req.FullName is empty/whitespace. The scanner deliberately does not provide
// any fallback for missing identifiers — refusing the scan is a feature, not a
// bug. Callers higher in the stack (onboarding, KYC review) are responsible
// for surfacing the requirement to the operator.
func (s *Scanner) Match(ctx context.Context, req Request) (MatchResult, error) {
	if strings.TrimSpace(req.FullName) == "" {
		return MatchResult{}, ErrMissingFullName
	}
	if req.BirthDate == nil {
		return MatchResult{}, ErrMissingDOB
	}

	hits, err := s.backend.Lookup(ctx, req)
	if err != nil {
		return MatchResult{}, err
	}
	if hits == nil {
		hits = []Hit{}
	}
	return MatchResult{
		Hits:      hits,
		ScannedAt: s.now(),
	}, nil
}
