package server

import "testing"

// TestIsSystemPackage locks in the name-pattern heuristic that drives the
// `is_system` flag the UI uses to default-hide OS baseline packages. The
// rule of thumb: errors of omission (an OS package surfacing as application)
// are recoverable via the UI toggle; errors of inclusion (a real app hidden
// by default) are not. So we tilt conservative on additions.
func TestIsSystemPackage(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		source string
		want   bool
	}{
		// Non-system apt / dpkg packages — these are exactly the things an
		// operator went out of their way to install.
		{"nginx", "apt", false},
		{"postgresql-15", "apt", false},
		{"redis-server", "apt", false},

		// OS-baseline apt packages.
		{"linux-image-6.5", "apt", true},
		{"libc6", "apt", true},
		{"systemd", "apt", true},
		{"ca-certificates", "apt", true},

		// RPM.
		{"kernel-5.14", "rpm", true},
		{"nginx", "rpm", false},

		// Winget — Microsoft.* IDs are first-party. Third-party publishers
		// (Notepad++.Notepad++, VLC.VLC) fall through.
		{"Microsoft.VisualStudioCode", "winget", true},
		{"Notepad++.Notepad++", "winget", false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name+"|"+tc.source, func(t *testing.T) {
			t.Parallel()
			if got := isSystemPackage(tc.name, tc.source); got != tc.want {
				t.Fatalf("isSystemPackage(%q, %q) = %v, want %v", tc.name, tc.source, got, tc.want)
			}
		})
	}
}
