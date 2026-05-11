package server

import "strings"

// isSystemPackage returns true when (name, source) matches a known OS-baseline
// package prefix. The heuristic is intentionally conservative — we prefer
// false negatives (an OS package slipping through as "application") over
// false positives (a user-installed package being hidden by the UI default
// filter). The lists are easy to extend as we find gaps in the wild.
//
// Sources we recognise: apt, dpkg (Debian/Ubuntu), rpm (RHEL family), winget.
// Anything else (homebrew, pacman, etc.) falls through to false — the
// operator UI surfaces it as an "application" package, which matches the
// expectation that the heuristic only suppresses obvious OS noise.
func isSystemPackage(name, source string) bool {
	switch source {
	case "apt", "dpkg":
		return hasAnyPrefix(name, aptSystemPrefixes)
	case "rpm":
		return hasAnyPrefix(name, rpmSystemPrefixes)
	case "winget":
		return hasAnyPrefix(name, wingetSystemPrefixes)
	default:
		return false
	}
}

func hasAnyPrefix(name string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// aptSystemPrefixes covers the Debian/Ubuntu OS baseline. Prefix match so
// `linux-image-6.5.0-21-generic`, `libc6`, `libc-bin`, `gcc-12`, etc. all hit.
var aptSystemPrefixes = []string{
	"linux-",
	"libc",
	"systemd",
	"grub",
	"openssl",
	"ca-certificates",
	"gcc-",
	"g++-",
	"perl-",
	"bash",
	"dash",
	"util-linux",
	"coreutils",
	"base-files",
	"tar",
	"gzip",
	"sed",
	"gawk",
	"grep",
	"mount",
	"adduser",
	"passwd",
	"init-system-helpers",
	"debconf",
	"dpkg",
	"apt",
	"gnupg",
	"gpgv",
	"distro-info",
	"lsb-",
	"update-notifier",
	"netbase",
	"hostname",
	"insserv",
	"sysv-",
	"cron",
	"logrotate",
	"rsyslog",
}

// rpmSystemPrefixes covers the RHEL/Fedora baseline.
var rpmSystemPrefixes = []string{
	"kernel",
	"glibc",
	"systemd",
	"coreutils",
	"bash",
	"dnf-",
	"yum-",
	"rpm-",
	"openssl-libs",
	"ca-certificates",
}

// wingetSystemPrefixes — Microsoft.* IDs are first-party Windows components
// (Edge, .NET runtimes, VC++ redists, etc.). User-installed Notepad++,
// VLC.VLC, etc. fall through to false.
var wingetSystemPrefixes = []string{
	"Microsoft.",
}
