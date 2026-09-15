package main

import "testing"

func TestPlatformDataDirectories(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		goos        string
		programData string
		home        string
		root        bool
		configWant  string
		dataWant    string
	}{
		{
			name:        "windows uses program data",
			goos:        "windows",
			programData: `C:\ProgramData`,
			configWant:  `C:\ProgramData\ControlOne`,
			dataWant:    `C:\ProgramData\ControlOne\nodeagent`,
		},
		{
			name:       "macos system install uses shared application support",
			goos:       "darwin",
			home:       "/var/root",
			root:       true,
			configWant: "/Library/Application Support/ControlOne",
			dataWant:   "/Library/Application Support/ControlOne/nodeagent",
		},
		{
			name:       "macos user install remains user scoped",
			goos:       "darwin",
			home:       "/Users/alex",
			configWant: "/Users/alex/Library/Application Support/ControlOne",
			dataWant:   "/Users/alex/Library/Application Support/ControlOne/nodeagent",
		},
		{
			name:       "linux keeps existing locations",
			goos:       "linux",
			configWant: "/etc/control-one",
			dataWant:   "/var/lib/control-one/nodeagent",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := platformConfigDir(tc.goos, tc.programData, tc.home, tc.root); got != tc.configWant {
				t.Fatalf("config dir = %q, want %q", got, tc.configWant)
			}
			if got := platformDataDir(tc.goos, tc.programData, tc.home, tc.root); got != tc.dataWant {
				t.Fatalf("data dir = %q, want %q", got, tc.dataWant)
			}
		})
	}
}
