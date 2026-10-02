package main

import "testing"

func TestCountUpgradableLines(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		out  string
		want int
	}{
		{
			name: "apt",
			out: `Listing...
bash/jammy-updates 5.1-6ubuntu1.1 amd64 [upgradable from: 5.1-6ubuntu1]
openssl/jammy-security 3.0.2-0ubuntu1.20 amd64 [upgradable from: 3.0.2-0ubuntu1.18]
`,
			want: 2,
		},
		{
			name: "apt warnings ignored",
			out: `WARNING: apt does not have a stable CLI interface.
Listing...
linux-image-generic/jammy-updates 5.15.0.130.128 amd64 [upgradable from: 5.15.0.127.125]
`,
			want: 1,
		},
		{
			name: "dnf",
			out: `Last metadata expiration check: 0:01:12 ago on Thu 01 Oct 2026.
bash.x86_64 5.2.26-3.el9 updates
openssl-libs.x86_64 3.2.2-6.el9 updates
`,
			want: 2,
		},
		{
			name: "empty",
			out:  "Listing...\n",
			want: 0,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := countUpgradableLines(tt.out); got != tt.want {
				t.Fatalf("countUpgradableLines() = %d, want %d", got, tt.want)
			}
		})
	}
}
