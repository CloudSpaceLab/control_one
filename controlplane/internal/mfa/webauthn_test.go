package mfa

import "testing"

func TestChallengeRoundTrip(t *testing.T) {
	ch, raw, err := NewRegistrationChallenge("u", "alice", "rp", "ControlOne")
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 32 {
		t.Fatalf("want 32-byte raw challenge, got %d", len(raw))
	}
	if !VerifyChallengeMatch(ch.Challenge, raw) {
		t.Fatal("matching challenge should verify")
	}
	// Replace a middle character with something guaranteed to decode differently.
	if len(ch.Challenge) < 8 {
		t.Fatal("challenge too short")
	}
	swap := 'A'
	if ch.Challenge[4] == 'A' {
		swap = 'B'
	}
	flipped := ch.Challenge[:4] + string(swap) + ch.Challenge[5:]
	if VerifyChallengeMatch(flipped, raw) {
		t.Fatal("tampered challenge must not verify")
	}
}

func TestAssertionChallenge(t *testing.T) {
	ch, raw, err := NewAssertionChallenge("rp", []string{"credA"})
	if err != nil {
		t.Fatal(err)
	}
	if ch.RPID != "rp" {
		t.Fatalf("unexpected rp_id %s", ch.RPID)
	}
	if len(ch.AllowCredentials) != 1 {
		t.Fatal("allow list should pass through")
	}
	if !VerifyChallengeMatch(ch.Challenge, raw) {
		t.Fatal("assertion challenge should verify")
	}
}

func TestRequiresRPID(t *testing.T) {
	if _, _, err := NewRegistrationChallenge("u", "a", "", "rp"); err == nil {
		t.Fatal("missing rp_id must error")
	}
	if _, _, err := NewAssertionChallenge("", nil); err == nil {
		t.Fatal("missing rp_id must error")
	}
}
