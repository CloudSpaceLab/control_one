package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/packageinventory"
)

var nativeDebName = regexp.MustCompile(`^[a-z0-9][a-z0-9+.-]*(?::[a-z0-9_-]+)?$`)
var nativeAPTPart = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`)

// collectAPTCandidates never refreshes repositories, changes sources or installs
// a package. Only a verified Release -> Packages -> candidate chain is trusted.
func collectAPTCandidates(items []PackageInfo) []packageinventory.CandidateObservation {
	if len(items) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	args := []string{"policy"}
	for _, p := range items {
		if p.Source != "dpkg" || !nativeDebName.MatchString(p.Name) {
			return nil
		}
		name := p.Name
		if p.Arch != "all" && !strings.Contains(name, ":") {
			name += ":" + p.Arch
		}
		args = append(args, name)
	}
	if len(items) > 2000 {
		return nil
	} // Explicit missing coverage, not fabricated data.
	cmd := exec.CommandContext(ctx, "apt-cache", args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	observations := parseAPTPolicy(string(out), items)
	groups := map[string][]int{}
	for i, o := range observations {
		if o.Repository != "" {
			groups[o.Repository+"\x00"+o.Suite+"\x00"+o.Component+"\x00"+o.RepositoryArch] = append(groups[o.Repository+"\x00"+o.Suite+"\x00"+o.Component+"\x00"+o.RepositoryArch], i)
		}
	}
	for _, indices := range groups {
		proof, artifacts, verifyErr := verifyAPTIndex(ctx, observations[indices[0]], observations, indices)
		for _, i := range indices {
			if verifyErr != nil {
				observations[i].Reason = "repository_signature_index_or_freshness_unverified"
				continue
			}
			artifact := artifacts[observations[i].Name+"\x00"+observations[i].CandidateVersion+"\x00"+observations[i].Arch]
			if artifact == "" {
				observations[i].Reason = "candidate_not_in_verified_index"
				continue
			}
			o := &observations[i]
			o.State = "verified_repository_candidate"
			o.Reason = "signed_release_and_index_verified"
			o.Vendor = proof.Vendor
			o.ReleaseDate = proof.ReleaseDate
			o.ExpiresAt = proof.ExpiresAt
			o.ReleaseSHA256 = proof.ReleaseSHA256
			o.IndexSHA256 = proof.IndexSHA256
			o.ArtifactSHA256 = artifact
		}
	}
	return observations
}

func parseAPTPolicy(output string, items []PackageInfo) []packageinventory.CandidateObservation {
	byName := map[string]int{}
	out := make([]packageinventory.CandidateObservation, 0, len(items))
	for _, p := range items {
		i := len(out)
		out = append(out, packageinventory.CandidateObservation{Name: p.Name, Arch: p.Arch, InstalledVersion: p.Version, ObservedAt: time.Now().UTC(), State: "unknown", Reason: "repository_candidate_unavailable"})
		byName[p.Name] = i
		byName[p.Name+":"+p.Arch] = i
	}
	index := -1
	version := ""
	for _, line := range strings.Split(output, "\n") {
		if line != "" && line[0] != ' ' && strings.HasSuffix(line, ":") {
			var ok bool
			index, ok = byName[strings.TrimSuffix(line, ":")]
			if !ok {
				index = -1
			}
			version = ""
			continue
		}
		if index < 0 {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if fields[0] == "Installed:" && len(fields) == 2 && fields[1] != out[index].InstalledVersion {
			out[index].State = "inventory_changed"
			out[index].Reason = "apt_installed_version_differs_from_snapshot"
		}
		if fields[0] == "Candidate:" && len(fields) == 2 && fields[1] != "(none)" {
			out[index].CandidateVersion = fields[1]
			continue
		}
		if fields[0] == "***" {
			fields = fields[1:]
		}
		if len(fields) == 2 {
			if _, err := strconv.Atoi(fields[1]); err == nil {
				version = fields[0]
			}
			continue
		}
		if len(fields) == 5 && fields[4] == "Packages" && version == out[index].CandidateVersion && out[index].State != "inventory_changed" && out[index].Repository == "" {
			u, err := url.Parse(fields[1])
			if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
				continue
			}
			parts := strings.Split(fields[2], "/")
			if len(parts) != 2 || !nativeAPTPart.MatchString(parts[0]) || !nativeAPTPart.MatchString(parts[1]) || !nativeAPTPart.MatchString(fields[3]) {
				continue
			}
			if out[index].Arch != "all" && out[index].Arch != fields[3] {
				continue
			}
			out[index].Repository = strings.TrimSuffix(fields[1], "/")
			out[index].Suite = parts[0]
			out[index].Component = parts[1]
			out[index].RepositoryArch = fields[3]
		}
	}
	return out
}

type aptReleaseProof struct {
	Vendor, Suite              string
	ReleaseDate, ExpiresAt     time.Time
	ReleaseSHA256, IndexSHA256 string
	IndexSize                  int64
}

func parseVerifiedAPTRelease(body []byte, component, arch string, now time.Time) (aptReleaseProof, error) {
	p := aptReleaseProof{}
	sum := sha256.Sum256(body)
	p.ReleaseSHA256 = hex.EncodeToString(sum[:])
	inHashes := false
	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(line, "Origin:") {
			p.Vendor = strings.TrimSpace(strings.TrimPrefix(line, "Origin:"))
		}
		if strings.HasPrefix(line, "Suite:") {
			p.Suite = strings.TrimSpace(strings.TrimPrefix(line, "Suite:"))
		}
		if strings.HasPrefix(line, "Date:") {
			p.ReleaseDate, _ = time.Parse(time.RFC1123, strings.TrimSpace(strings.TrimPrefix(line, "Date:")))
		}
		if strings.HasPrefix(line, "Valid-Until:") {
			var err error
			p.ExpiresAt, err = time.Parse(time.RFC1123, strings.TrimSpace(strings.TrimPrefix(line, "Valid-Until:")))
			if err != nil {
				return p, err
			}
		}
		if line == "SHA256:" {
			inHashes = true
			continue
		}
		if line != "" && line[0] != ' ' {
			inHashes = false
		}
		if inHashes {
			fields := strings.Fields(line)
			if len(fields) == 3 && fields[2] == component+"/binary-"+arch+"/Packages" {
				p.IndexSHA256 = fields[0]
				p.IndexSize, _ = strconv.ParseInt(fields[1], 10, 64)
			}
		}
	}
	if p.ExpiresAt.IsZero() {
		p.ExpiresAt = p.ReleaseDate.Add(7 * 24 * time.Hour)
	}
	if cap := p.ReleaseDate.Add(7 * 24 * time.Hour); p.ExpiresAt.After(cap) {
		p.ExpiresAt = cap
	}
	if p.Vendor != "Ubuntu" || p.ReleaseDate.IsZero() || p.ReleaseDate.After(now) || !p.ExpiresAt.After(now) || now.Sub(p.ReleaseDate) > 7*24*time.Hour || len(p.IndexSHA256) != 64 || p.IndexSize <= 0 || p.IndexSize > 256*1024*1024 {
		return p, errors.New("unsupported or stale signed Ubuntu repository metadata")
	}
	return p, nil
}

func verifyAPTIndex(ctx context.Context, o packageinventory.CandidateObservation, observations []packageinventory.CandidateObservation, indices []int) (aptReleaseProof, map[string]string, error) {
	var proof aptReleaseProof
	u, err := url.Parse(o.Repository)
	if err != nil {
		return proof, nil, err
	}
	// Unsupported URI-to-list mappings stay unknown; never guess an index.
	if u.Hostname() != u.Host || !regexp.MustCompile(`^[a-zA-Z0-9./_-]*$`).MatchString(u.Host+u.Path) {
		return proof, nil, errors.New("unsupported apt list identity")
	}
	prefix := u.Host + strings.ReplaceAll(strings.TrimSuffix(u.Path, "/"), "/", "_") + "_dists_" + o.Suite
	release := filepath.Join("/var/lib/apt/lists", prefix+"_InRelease")
	if info, err := os.Stat(release); err != nil || info.Size() > 2*1024*1024 {
		return proof, nil, errors.New("release metadata unavailable or oversized")
	}
	cmd := exec.CommandContext(ctx, "gpgv", "--keyring", "/usr/share/keyrings/ubuntu-archive-keyring.gpg", "--output", "-", release)
	body, err := cmd.Output()
	if err != nil {
		return proof, nil, err
	}
	proof, err = parseVerifiedAPTRelease(body, o.Component, o.RepositoryArch, time.Now().UTC())
	if err != nil {
		return proof, nil, err
	}
	if proof.Suite != o.Suite {
		return proof, nil, errors.New("signed repository suite mismatch")
	}
	base := filepath.Join("/var/lib/apt/lists", prefix+"_"+o.Component+"_binary-"+o.RepositoryArch+"_Packages")
	indexFile := ""
	for _, suffix := range []string{".lz4", "", ".xz", ".gz"} {
		if info, err := os.Stat(base + suffix); err == nil && info.Mode().IsRegular() {
			indexFile = base + suffix
			break
		}
	}
	if indexFile == "" {
		return proof, nil, errors.New("repository index unavailable")
	}
	cmd = exec.CommandContext(ctx, "/usr/lib/apt/apt-helper", "cat-file", indexFile)
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return proof, nil, err
	}
	if err = cmd.Start(); err != nil {
		return proof, nil, err
	}
	hash := sha256.New()
	limited := &io.LimitedReader{R: pipe, N: proof.IndexSize + 1}
	reader := io.TeeReader(limited, hash)
	wanted := map[string]bool{}
	for _, i := range indices {
		item := observations[i]
		wanted[item.Name+"\x00"+item.CandidateVersion+"\x00"+item.Arch] = true
	}
	artifacts := map[string]string{}
	stanza := map[string]string{}
	flush := func() {
		key := stanza["Package"] + "\x00" + stanza["Version"] + "\x00" + stanza["Architecture"]
		if wanted[key] && len(stanza["SHA256"]) == 64 {
			artifacts[key] = stanza["SHA256"]
		}
		stanza = map[string]string{}
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			flush()
			continue
		}
		if line[0] == ' ' {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			stanza[parts[0]] = strings.TrimSpace(parts[1])
		}
	}
	flush()
	scanErr := scanner.Err()
	if scanErr != nil || limited.N == 0 {
		_ = cmd.Process.Kill()
	}
	waitErr := cmd.Wait()
	if scanErr != nil {
		return proof, nil, scanErr
	}
	if waitErr != nil {
		return proof, nil, waitErr
	}
	if proof.IndexSize+1-limited.N != proof.IndexSize || hex.EncodeToString(hash.Sum(nil)) != proof.IndexSHA256 {
		return proof, nil, fmt.Errorf("repository index checksum mismatch")
	}
	return proof, artifacts, nil
}
