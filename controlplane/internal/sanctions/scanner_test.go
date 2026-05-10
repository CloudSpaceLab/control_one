package sanctions

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeBackend struct {
	lastReq Request
	calls   int
	hits    []Hit
	err     error
}

func (f *fakeBackend) Lookup(_ context.Context, req Request) ([]Hit, error) {
	f.calls++
	f.lastReq = req
	return f.hits, f.err
}

// TestMatch_RefusesNullDOB is the load-bearing assertion for bugs §4 #3.
//
// SanctionsScanner used to silently substitute birthDate=1962-11-23 when DOB
// was nil. We now refuse: ErrMissingDOB must be returned and the backend must
// not be called.
func TestMatch_RefusesNullDOB(t *testing.T) {
	be := &fakeBackend{}
	s := NewScanner(be)

	res, err := s.Match(context.Background(), Request{
		FullName:  "Adunni Okonkwo",
		BirthDate: nil,
	})

	if !errors.Is(err, ErrMissingDOB) {
		t.Fatalf("expected ErrMissingDOB on nil BirthDate, got err=%v result=%+v", err, res)
	}
	if be.calls != 0 {
		t.Errorf("backend must not be called when DOB is missing; got %d calls", be.calls)
	}
	if len(res.Hits) != 0 {
		t.Errorf("expected zero hits on refusal, got %d", len(res.Hits))
	}
}

func TestMatch_RefusesEmptyName(t *testing.T) {
	be := &fakeBackend{}
	s := NewScanner(be)

	dob := time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC)
	_, err := s.Match(context.Background(), Request{
		FullName:  "   ",
		BirthDate: &dob,
	})

	if !errors.Is(err, ErrMissingFullName) {
		t.Fatalf("expected ErrMissingFullName on whitespace name, got %v", err)
	}
	if be.calls != 0 {
		t.Errorf("backend must not be called when name is missing; got %d calls", be.calls)
	}
}

func TestMatch_HappyPath(t *testing.T) {
	want := []Hit{{ListID: "OFAC-SDN", EntryID: "12345", Score: 0.97, Source: "moov-watchman"}}
	be := &fakeBackend{hits: want}
	s := NewScanner(be)
	s.now = func() time.Time { return time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC) }

	dob := time.Date(1985, 6, 14, 0, 0, 0, 0, time.UTC)
	res, err := s.Match(context.Background(), Request{
		FullName:  "Jane Doe",
		BirthDate: &dob,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if be.calls != 1 {
		t.Fatalf("expected 1 backend call, got %d", be.calls)
	}
	if len(res.Hits) != 1 || res.Hits[0].EntryID != "12345" {
		t.Fatalf("unexpected hits: %+v", res.Hits)
	}
	if !res.ScannedAt.Equal(time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("ScannedAt not propagated from clock; got %s", res.ScannedAt)
	}
}

func TestMatch_BackendError_BubblesUp(t *testing.T) {
	sentinel := errors.New("watchman down")
	be := &fakeBackend{err: sentinel}
	s := NewScanner(be)

	dob := time.Date(1985, 6, 14, 0, 0, 0, 0, time.UTC)
	_, err := s.Match(context.Background(), Request{
		FullName:  "Jane Doe",
		BirthDate: &dob,
	})

	if !errors.Is(err, sentinel) {
		t.Fatalf("expected backend error to bubble, got %v", err)
	}
}

func TestMatch_NilHits_NormalizesToEmpty(t *testing.T) {
	be := &fakeBackend{hits: nil}
	s := NewScanner(be)
	dob := time.Date(1985, 6, 14, 0, 0, 0, 0, time.UTC)

	res, err := s.Match(context.Background(), Request{
		FullName:  "Jane Doe",
		BirthDate: &dob,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Hits == nil {
		t.Errorf("Hits should normalize to empty slice, not nil")
	}
}
