package storage

import "testing"

func TestIsBuiltInRoleName(t *testing.T) {
	tests := map[string]bool{
		"admin":        true,
		" CISO ":       true,
		"investigator": true,
		"operator":     true,
		"viewer":       true,
		"soc-reviewer": false,
		"":             false,
	}

	for role, want := range tests {
		t.Run(role, func(t *testing.T) {
			if got := IsBuiltInRoleName(role); got != want {
				t.Fatalf("IsBuiltInRoleName(%q) = %v, want %v", role, got, want)
			}
		})
	}
}
